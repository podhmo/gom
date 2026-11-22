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
	"github.com/podhmo/gom/internal/types"
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
	e.conversionModules()
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
	// Members register from the checked declarations — the same
	// objects and shapes gomimport's extract registers for an
	// imported package: `d.Sym` carries Def (the decl node hover
	// and diagnostics cite), a thunk FnDecl keeps its `() -> T`
	// signature rather than the let-style T the surface reports,
	// and only real functions record a declared arity.
	own := map[string]bool{}
	for _, d := range r.Typed.Decls {
		if td, ok := d.(*check.TypeDecl); ok && td.Sym != nil {
			own[td.Sym.Name] = true
		}
	}
	table := map[string]*resolve.Symbol{}
	for _, d := range r.Typed.Decls {
		switch d := d.(type) {
		case *check.FnDecl:
			sym := d.Sym
			if sym == nil || sym.TestOnly {
				continue
			}
			sym.Package = impPath
			table[sym.Name] = sym
			// Signature references to the interface's own types
			// carry the import path — the same requalification
			// gomimport's extract runs on member signatures.
			sig := check.Signature{Type: requalify(d.Sig, own, impPath)}
			// Only functions carry a declared arity — like
			// gomimport's extract, which registers FnDecl/
			// MethodDecl arities only. A let member's answer must
			// stay unknown (lowering falls back to the flat arity
			// of its type); recording zero would claim arity 0
			// for a function-valued let.
			if !d.Thunk && d.Sig != nil {
				sig.DeclArity = len(d.Sig.Params)
				e.arities[sym] = len(d.Sig.Params)
			}
			e.sigs[sym] = sig
		case *check.LetDecl:
			for _, m := range letPatMembers(d.Pat) {
				if m.sym == nil || m.sym.TestOnly {
					continue
				}
				m.sym.Package = impPath
				table[m.sym.Name] = m.sym
				e.sigs[m.sym] = check.Signature{
					Type: requalify(m.typ, own, impPath),
				}
			}
		case *check.TypeDecl:
			if d.Sym == nil {
				continue
			}
			e.loadType(table, impPath, d, own)
		case *check.TestDecl:
			// A .gomi cannot declare tests; nothing registers.
		default:
			panic("unreachable")
		}
	}
	// `//gom:noescape` directives annotate the higher-order
	// members whose callback parameter is applied inside the
	// call and never retained — the same read gomimport runs
	// on a loaded package's .gomi (internal/iface's shared
	// scan).
	iface.ApplyNoescape(table,
		func(sym *resolve.Symbol) (int, bool) { n, ok := e.arities[sym]; return n, ok },
		func(sym *resolve.Symbol) (check.Signature, bool) { sg, ok := e.sigs[sym]; return sg, ok },
		iface.ScanNoescape(src))
	e.modules[module] = table
	return nil
}

// letPatMember pairs a symbol bound by a `let` pattern with the type
// it carries — mirroring gomimport's letSyms+patType for the member
// table (types.go already mirrors that file's other helpers).
type letPatMember struct {
	sym *resolve.Symbol
	typ types.Type
}

// letPatMembers expands a typed top-level `let` pattern into one
// member per bound name — `let (a, b) = e` exposes two constants.
// Wildcards and non-binding positions contribute nothing.
func letPatMembers(p check.Pat) []letPatMember {
	var out []letPatMember
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch p := p.(type) {
		case nil:
		case *check.WildPat, *check.LitPat:
		case *check.VarPat:
			out = append(out, letPatMember{p.Sym, p.Type()})
		case *check.CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for i := range p.Fields {
				walk(p.Fields[i].Pat)
			}
		case *check.TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for i := range p.Fields {
				walk(p.Fields[i].Pat)
			}
		case *check.ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			walk(p.Rest)
		case *check.OrPat:
			if len(p.Alts) > 0 {
				walk(p.Alts[0]) // alternatives bind the same names
			}
		case *check.AsPat:
			walk(p.Pat)
			out = append(out, letPatMember{p.Sym, p.Type()})
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
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
	baseFuncType := le.FuncType
	le.FuncType = func(sym *resolve.Symbol) (*types.Func, bool) {
		if s, ok := e.sigs[sym]; ok {
			if f, ok := s.Type.(*types.Func); ok {
				return f, true
			}
		}
		if baseFuncType != nil {
			return baseFuncType(sym)
		}
		return nil, false
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
