// Package stdlib wires the Gom-written standard library into a
// compile: the member tables of the builtin-scope modules
// (List.map, Option.get, Result.map_err, ...), their check
// signatures and declared arities — all derived once by running the
// embedded .gomi interface files through the front half of the
// pipeline (docs/design.md: the stdlib is built by the compiler
// itself). The `Chan`, `Task`, and `Ptr` modules — and the
// `Go`-namespace type-member table `Go.Map` — ship alongside them
// as hand-built tables backed by rt functions (builtin.go): their
// signatures are not expressible in a .gomi.
//
// The interface files are the single source of truth: they are what
// `gom check stdlib/list` enforces the .gom implementation against
// (spec section 7), and they are what Apply turns into resolver
// module tables — the same signatures, seen from both sides.
package stdlib

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/iface"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	gomstdlib "github.com/podhmo/gom/stdlib"
)

// packages binds each builtin-scope module to its Gom package: the
// interface source is <pkg>/<pkg>.gomi inside gomstdlib.Gomis and
// generated code reaches the members through impPath — the same
// paths codegen hardcodes for the builtin types' representations.
var packages = []struct {
	module  string // builtin-scope module name
	pkg     string // gom package and directory name
	impPath string // Go import path of the generated package
}{
	{"List", "list", "github.com/podhmo/gom/stdlib/list"},
	{"Option", "option", "github.com/podhmo/gom/stdlib/option"},
	{"Result", "result", "github.com/podhmo/gom/stdlib/result"},
}

// env is the loaded standard library: resolver member tables keyed
// by builtin module name, the check signatures of every member
// symbol, their declared arities for lowering, and the GoSig oracles
// of the rt-backed members (Chan/Task/Ptr/Go.Map).
type env struct {
	modules map[string]map[string]*resolve.Symbol
	sigs    check.MapSource
	arities map[*resolve.Symbol]int
	gosigs  map[*resolve.Symbol]lower.GoSig
}

// load compiles every embedded .gomi once and surfaces it into the
// resolver/checker oracles. The sources ship inside the binary and
// are part of the compiler's own test suite, so a failure here is a
// broken toolchain, not a user error — callers panic on it.
var load = sync.OnceValues(func() (*env, error) {
	e := &env{
		modules: map[string]map[string]*resolve.Symbol{},
		sigs:    check.MapSource{},
		arities: map[*resolve.Symbol]int{},
		gosigs:  map[*resolve.Symbol]lower.GoSig{},
	}
	e.builtinModules()
	for _, p := range packages {
		gomiPath := p.pkg + "/" + p.pkg + ".gomi"
		src, err := gomstdlib.Gomis.ReadFile(gomiPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", gomiPath, err)
		}
		// The interface alone goes through parse → resolve →
		// check; it has no imports and needs no oracles.
		r, err := build.Compile(build.Input{
			Path: "stdlib/" + gomiPath, Src: src, Interface: true,
		}, nil)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", gomiPath, err)
		}
		if r.Diagnostics.HasError() {
			return nil, fmt.Errorf("%s does not check: %d diagnostic(s)",
				gomiPath, len(r.Diagnostics))
		}
		table := map[string]*resolve.Symbol{}
		for _, m := range iface.Surface(r.Typed).Members {
			kind := resolve.KindFunc
			if m.Kind == iface.MemberLet {
				kind = resolve.KindVar
			}
			if m.Kind == iface.MemberType {
				// Module type members (`List.Foo`) would need a
				// type-level symbol table entry; no stdlib
				// interface declares types today — the builtin
				// types themselves are compiler-known.
				continue
			}
			sym := &resolve.Symbol{
				Name: m.Name, Kind: kind, Package: p.impPath,
			}
			table[m.Name] = sym
			e.sigs[sym] = check.Signature{
				Type:      m.Type,
				DeclArity: m.Arity,
			}
			e.arities[sym] = m.Arity
		}
		e.modules[p.module] = table
	}
	return e, nil
})

// SelfPath answers build.Options.SelfImportPath for inputPath: a
// source living under a `stdlib/<pkg>/` directory compiles into the
// corresponding toolchain package, so references into that import
// path are self-references emitted unqualified — generated code can
// never produce a self-import. The stdlib directory names are
// reserved: a user project that happens to place a package under
// its own `stdlib/list/` takes the same convention. Other inputs
// get "" — codegen cannot know a user file's import path.
func SelfPath(inputPath string) string {
	segs := strings.Split(filepath.ToSlash(inputPath), "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] != "stdlib" {
			continue
		}
		for _, p := range packages {
			if segs[i+1] == p.pkg {
				return p.impPath
			}
		}
	}
	return ""
}

// Apply merges the standard library into opts and returns it:
//
//   - Modules gains the List/Option/Result member tables, so
//     `List.map` resolves without an import clause.
//   - Sigs is chained ahead of the existing source: each member
//     symbol carries its .gomi signature.
//   - Lower.FuncArity is chained likewise, giving partial
//     application and adapters the declared parameter count.
//
// The embedded interfaces are part of the toolchain: when one fails
// to compile the compiler itself is broken, so Apply panics rather
// than degrading into E0201s pointing at user code.
func Apply(o *build.Options) *build.Options {
	e, err := load()
	if err != nil {
		panic("internal/stdlib: " + err.Error())
	}
	if o == nil {
		o = &build.Options{}
	}
	if o.Modules == nil {
		o.Modules = map[string]map[string]*resolve.Symbol{}
	}
	for m, t := range e.modules {
		o.Modules[m] = t
	}
	o.Sigs = check.Chain(e.sigs, o.Sigs)
	le := lower.Env{}
	if o.Lower != nil {
		le = *o.Lower
	}
	base := le.FuncArity
	le.FuncArity = func(sym *resolve.Symbol) int {
		if a, ok := e.arities[sym]; ok {
			return a
		}
		if base != nil {
			return base(sym)
		}
		return -1
	}
	baseGoSig := le.GoSig
	le.GoSig = func(sym *resolve.Symbol) (lower.GoSig, bool) {
		if s, ok := e.gosigs[sym]; ok {
			return s, true
		}
		if baseGoSig != nil {
			return baseGoSig(sym)
		}
		return lower.GoSig{}, false
	}
	o.Lower = &le
	return o
}
