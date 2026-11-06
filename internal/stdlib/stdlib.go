// Package stdlib wires the Gom-written standard library into a
// compile: the member tables of the builtin-scope modules
// (List.map, Option.get, Result.map_err, ...), their check
// signatures and declared arities — all derived once by running the
// embedded .gomi interface files through the front half of the
// pipeline (docs/design.md: the stdlib is built by the compiler
// itself). The `Chan`, `Task`, and `Ptr` modules — and the
// `Go`-namespace type-member tables `Go.Map` and `Go.Slice` —
// ship alongside them as hand-built tables backed by rt functions
// (builtin.go): their signatures are not expressible in a .gomi.
//
// The interface files are the single source of truth: they are what
// `gom check stdlib/list` enforces the .gom implementation against
// (spec section 7), and they are what Apply turns into resolver
// module tables — the same signatures, seen from both sides.
package stdlib

import (
	"fmt"
	"maps"
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
// of the rt-backed members (Chan/Task/Ptr/Go.Map/Go.Slice). Type
// members add the same tables a gomimport Loader builds for an
// imported package — the foreign declarations check registers, the
// `pkg.Type.Ctor` and labeled-payload tables resolve consults
// through the Loader seam, and the by-constructor index the
// lowering oracle answers from.
type env struct {
	modules    map[string]map[string]*resolve.Symbol
	sigs       check.MapSource
	arities    map[*resolve.Symbol]int
	gosigs     map[*resolve.Symbol]lower.GoSig
	tdecls     []*check.TypeDecl
	byCon      map[conKey]*check.TypeDecl
	ctorFields map[*resolve.Symbol]map[string]*resolve.Symbol
	typeCtors  map[*resolve.Symbol]map[string]*resolve.Symbol
}

// load compiles every embedded .gomi once and surfaces it into the
// resolver/checker oracles. The sources ship inside the binary and
// are part of the compiler's own test suite, so a failure here is a
// broken toolchain, not a user error — callers panic on it.
var load = sync.OnceValues(func() (*env, error) {
	e := newEnv()
	e.builtinModules()
	for _, p := range packages {
		gomiPath := p.pkg + "/" + p.pkg + ".gomi"
		src, err := gomstdlib.Gomis.ReadFile(gomiPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", gomiPath, err)
		}
		if err := e.loadPackage(p.module, gomiPath, p.impPath, src); err != nil {
			return nil, err
		}
	}
	return e, nil
})

// newEnv is an empty stdlib environment — the shape load() fills.
// Tests drive it per package through loadPackage.
func newEnv() *env {
	return &env{
		modules:    map[string]map[string]*resolve.Symbol{},
		sigs:       check.MapSource{},
		arities:    map[*resolve.Symbol]int{},
		gosigs:     map[*resolve.Symbol]lower.GoSig{},
		byCon:      map[conKey]*check.TypeDecl{},
		ctorFields: map[*resolve.Symbol]map[string]*resolve.Symbol{},
		typeCtors:  map[*resolve.Symbol]map[string]*resolve.Symbol{},
	}
}

// loadPackage compiles one embedded .gomi source and surfaces its
// members — functions, lets, and type declarations — into the
// module's member table and the env's cross-phase tables, the same
// extraction gomimport's Loader runs on a loaded package's
// interface.
func (e *env) loadPackage(module, gomiPath, impPath string, src []byte) error {
	// The interface alone goes through parse → resolve → check; it
	// has no imports and needs no oracles.
	r, err := build.Compile(build.Input{
		Path: "stdlib/" + gomiPath, Src: src, Interface: true,
	}, nil)
	if err != nil {
		return fmt.Errorf("compile %s: %w", gomiPath, err)
	}
	if r.Diagnostics.HasError() {
		return fmt.Errorf("%s does not check: %d diagnostic(s)",
			gomiPath, len(r.Diagnostics))
	}
	// Type members register from the checked declaration, not the
	// surface member — the ctor tables, signatures and foreign-type
	// registration all key on the declaration's own symbols.
	tdeclByName := map[string]*check.TypeDecl{}
	own := map[string]bool{}
	for _, d := range r.Typed.Decls {
		if td, ok := d.(*check.TypeDecl); ok && td.Sym != nil {
			tdeclByName[td.Sym.Name] = td
			own[td.Sym.Name] = true
		}
	}
	table := map[string]*resolve.Symbol{}
	for _, m := range iface.Surface(r.Typed).Members {
		if m.Kind == iface.MemberType {
			td := tdeclByName[m.Name]
			if td == nil {
				return fmt.Errorf("%s: type member %s has no declaration",
					gomiPath, m.Name)
			}
			e.loadType(table, impPath, td, own)
			continue
		}
		kind := resolve.KindFunc
		if m.Kind == iface.MemberLet {
			kind = resolve.KindVar
		}
		sym := &resolve.Symbol{
			Name: m.Name, Kind: kind, Package: impPath,
		}
		table[m.Name] = sym
		// Signature references to the interface's own types
		// carry the import path — the same requalification
		// gomimport's extract runs on member signatures.
		e.sigs[sym] = check.Signature{
			Type:      requalify(m.Type, own, impPath),
			DeclArity: m.Arity,
		}
		// Only functions carry a declared arity — like
		// gomimport's extract, which registers FnDecl/
		// MethodDecl arities only. A let member's answer must
		// stay unknown (lowering falls back to the flat arity
		// of its type); recording m.Arity's zero would claim
		// arity 0 for a function-valued let.
		if m.Kind == iface.MemberFn {
			e.arities[sym] = m.Arity
		}
	}
	e.modules[module] = table
	return nil
}

// Apply merges the standard library into opts and returns it:
//
//   - Modules gains the List/Option/Result member tables, so
//     `List.map` resolves without an import clause. Type members
//     register like an imported package's: the type symbol and its
//     variant constructors join the table.
//   - Sigs is chained ahead of the existing source: each member
//     symbol carries its .gomi signature, and the source also
//     answers TypeDecls so check registers module type members as
//     foreign declarations.
//   - Loader is wrapped with the ctor/field tables of module type
//     members — `Mod.Type.Ctor` and labeled payload resolve through
//     the same Config.Ctors / Config.CtorFields seam an imported
//     package's do — before delegating to the real loader.
//   - Lower.Types is chained likewise: the modules' type members
//     answer variants/fields for lowering ahead of the imported-
//     package oracle.
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
	return e.apply(o)
}

// apply merges one loaded env into opts — Apply's work, split out so
// tests can wire a synthetic environment through the identical
// seams (a type member's resolution then proves identical to the
// imported-package path end to end).
func (e *env) apply(o *build.Options) *build.Options {
	if o == nil {
		o = &build.Options{}
	}
	if o.Modules == nil {
		o.Modules = map[string]map[string]*resolve.Symbol{}
	}
	maps.Copy(o.Modules, e.modules)
	o.Sigs = check.Chain(memberSigs{sigs: e.sigs, decls: e.tdecls}, o.Sigs)
	o.Loader = &ctorLoader{
		inner:  o.Loader,
		ctors:  e.typeCtors,
		fields: e.ctorFields,
	}
	le := lower.Env{}
	if o.Lower != nil {
		le = *o.Lower
	}
	le.Types = &typeOracle{byCon: e.byCon, inner: le.Types}
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
