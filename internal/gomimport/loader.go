// Package gomimport loads Gom packages for `import "path"`: the
// import path names the Go package the directory's generated code
// occupies, the directory holds exactly one <pkg>.gomi, and that
// interface alone supplies the member table — signatures
// requalified so the package's own type names carry the import path
// (docs/decisions/2026-11-05-package-compilation-unit.md section 2).
//
// One Loader is one session: member symbols it hands out are shared
// across every file that imports the package, which is what keeps
// symbol — and therefore type — identity meaningful downstream.
// The loader answers every consumer of imported-Gom metadata:
//
//   - resolve.Loader: Load returns the exported member table.
//   - check.SignatureSource: SignatureOf returns a member's
//     signature; a member that is a type answers its constructor.
//   - check.TypeDeclSource: TypeDecls returns the (requalified) type
//     declarations so `.field`, record literals and constructor
//     payload names type against the real declaration.
//   - resolve.CtorFieldSource (CtorFieldsOf): the payload field
//     tables `{ name = pat }` and `~label:` arguments of
//     constructor patterns resolve against.
//   - lower.TypeOracle: Variants and Fields expand wildcard match
//     arms over a foreign ADT and order record literals.
//   - FuncArity hook (ArityOf): declared arities for value
//     references and partial application.
//
// Directory lookup is module-local: the import path must equal the
// main module's path or sit under it (the generated package's Go
// import path); packages outside the main module — including
// workspace siblings — are not importable and report an ordinary
// load error, which resolution surfaces as E0203.
package gomimport

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/podhmo/gom/internal/build"
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/lower"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Config configures a Loader.
type Config struct {
	// Dir is the directory module resolution starts from — the
	// importing file's own directory. "" means the process working
	// directory.
	Dir string

	// Go is the `import go` oracle the loader delegates to — both
	// for the importing file's own `import go` clauses and for ones
	// inside loaded interfaces. It must be the same instance that
	// supplies those symbols' signatures through build.Options.Sigs,
	// or members the interfaces mint resolve nowhere. Nil makes
	// `import go` fail with E0203.
	Go resolve.Loader

	// Sigs supplies signatures for symbols this loader does not
	// mint — the Go loader's SignatureSource and the stdlib's
	// module members — consulted both for the importing file (via
	// SignatureOf's fallback) and inside loaded interfaces (their
	// own compiles get this loader as their signature source).
	Sigs check.SignatureSource

	// Modules supplies the builtin-scope module tables interface
	// compilation needs (internal/stdlib's Apply contribution).
	Modules map[string]map[string]*resolve.Symbol
}

// Loader is the Gom import oracle; see the package comment.
type Loader struct {
	cfg Config

	// modOnce resolves the main module's path and root directory
	// from the nearest go.mod once; failures latch into modErr.
	modOnce         sync.Once
	modPath, modDir string
	modErr          error

	// pkgs caches loaded member tables; errs the failures behind
	// them; loading marks in-progress paths so a cyclic `import`
	// reports instead of recursing.
	pkgs    map[string]*resolve.Package
	errs    map[string]error
	loading map[string]bool

	// The cross-phase tables every loaded package contributes to —
	// keyed by the same requalified symbols the member tables hand
	// out.
	sigs       map[*resolve.Symbol]check.Signature
	arities    map[*resolve.Symbol]int
	tdecls     []*check.TypeDecl
	byCon      map[conKey]*check.TypeDecl
	ctorFields map[*resolve.Symbol]map[string]*resolve.Symbol
	typeCtors  map[*resolve.Symbol]map[string]*resolve.Symbol

	// The implementation-import cycle check (E0606): .gom files of
	// loaded packages may import back toward the entry package —
	// a cycle the .gomi guard cannot see and `go build` would only
	// reject after codegen. implEdges caches each package's .gom
	// import paths; the DFS seeded at the entry package runs once.
	entry        string
	implScanned  bool
	implEdges    map[string]map[string]bool
	implDone     map[string]bool
	implCycleErr *resolve.ImportCycleError
	implCycleSet map[string]bool // the package paths *on* the found cycle
}

// conKey indexes loaded type declarations by nominal identity —
// the same shape check's type tables use.
type conKey struct {
	qual, name string
}

// New returns a Loader per cfg; a nil cfg means the zero-value
// configuration (module lookup from the working directory, no Go
// oracle, no external signatures).
func New(cfg *Config) *Loader {
	c := Config{}
	if cfg != nil {
		c = *cfg
	}
	return &Loader{
		cfg:        c,
		pkgs:       map[string]*resolve.Package{},
		errs:       map[string]error{},
		loading:    map[string]bool{},
		sigs:       map[*resolve.Symbol]check.Signature{},
		arities:    map[*resolve.Symbol]int{},
		byCon:      map[conKey]*check.TypeDecl{},
		ctorFields: map[*resolve.Symbol]map[string]*resolve.Symbol{},
		typeCtors:  map[*resolve.Symbol]map[string]*resolve.Symbol{},
		implEdges:  map[string]map[string]bool{},
		implDone:   map[string]bool{},
	}
}

// Load returns the exported member table of the package at path —
// the resolve.Loader entry. goPkg routes `import go` clauses to the
// configured Go oracle; a plain `import` loads the package's .gomi.
func (l *Loader) Load(path string, goPkg bool) (*resolve.Package, error) {
	if goPkg {
		if l.cfg.Go == nil {
			return nil, fmt.Errorf("no Go package loader configured")
		}
		return l.cfg.Go.Load(path, true)
	}
	if p, ok := l.pkgs[path]; ok {
		return p, nil
	}
	if err := l.errs[path]; err != nil {
		return nil, err
	}
	// The implementation-import scan needs no load state — it reads
	// .gom import clauses straight from disk — so it runs before the
	// first Gom load. Its verdict applies only to loads *on* the
	// found cycle: a clean import of a package that is not part of
	// the loop is not a cycle edge and must not report E0606.
	l.ensureImplScan()
	if l.implCycleErr != nil && l.implCycleSet[path] {
		return nil, l.implCycleErr
	}
	if l.loading[path] {
		return nil, fmt.Errorf("cyclic Gom import of %q", path)
	}
	l.loading[path] = true
	defer delete(l.loading, path)

	p, err := l.load(path)
	if err != nil {
		l.errs[path] = err
		return nil, err
	}
	l.pkgs[path] = p
	return p, nil
}

// load resolves path to a directory, compiles its .gomi, and
// surfaces the members.
func (l *Loader) load(path string) (*resolve.Package, error) {
	dir, err := l.dirOf(path)
	if err != nil {
		return nil, err
	}
	gomi, err := oneGomi(dir)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(gomi)
	if err != nil {
		return nil, err
	}
	// The interface compiles against this same loader: its `import
	// go` clauses delegate to Config.Go, nested Gom imports load
	// through here, and both kinds of symbols find their signatures
	// in the shared tables (SignatureOf falls back to Config.Sigs).
	r, err := build.Compile(build.Input{Path: gomi, Src: src, Interface: true},
		&build.Options{Loader: l, Sigs: l, Modules: l.cfg.Modules})
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", gomi, err)
	}
	if r.Diagnostics.HasError() {
		return nil, fmt.Errorf("%s does not check: %d diagnostic(s)", gomi, len(r.Diagnostics))
	}
	return l.extract(path, r.Typed)
}

// dirOf maps an import path to its package directory: the main
// module's root (found by walking up from Config.Dir to go.mod)
// plus the module-relative remainder of the path.
func (l *Loader) dirOf(path string) (string, error) {
	modPath, modDir, err := l.module()
	if err != nil {
		return "", err
	}
	switch {
	case path == modPath:
		return modDir, nil
	case strings.HasPrefix(path, modPath+"/"):
		return filepath.Join(modDir, filepath.FromSlash(strings.TrimPrefix(path, modPath+"/"))), nil
	default:
		return "", fmt.Errorf("package %q is outside the main module %q — only module-local Gom packages are importable", path, modPath)
	}
}

// module reports the main module's path and root directory, read
// from the go.mod nearest Config.Dir. The module line is parsed
// directly — `go list -m` would answer the same for the main module
// without dragging workspace state in, and the .gomi contract is
// module-local anyway.
func (l *Loader) module() (string, string, error) {
	l.modOnce.Do(func() {
		dir := l.cfg.Dir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			l.modErr = err
			return
		}
		for {
			gm := filepath.Join(dir, "go.mod")
			b, err := os.ReadFile(gm)
			if err == nil {
				for _, line := range strings.Split(string(b), "\n") {
					fields := strings.Fields(line)
					if len(fields) == 2 && fields[0] == "module" {
						l.modPath, l.modDir = fields[1], dir
						return
					}
				}
				l.modErr = fmt.Errorf("%s has no module directive", gm)
				return
			}
			if !os.IsNotExist(err) {
				l.modErr = err
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				l.modErr = fmt.Errorf("no go.mod found above %s", l.cfg.Dir)
				return
			}
			dir = parent
		}
	})
	return l.modPath, l.modDir, l.modErr
}

// oneGomi returns the single .gomi file directly inside dir — the
// unit's public interface. Zero or several both fail: a missing one
// is the ordinary "not a Gom package" case, and several means the
// directory does not conform to the one-interface-per-package rule.
func oneGomi(dir string) (string, error) {
	ms, err := filepath.Glob(filepath.Join(dir, "*.gomi"))
	if err != nil {
		return "", err
	}
	switch len(ms) {
	case 0:
		return "", fmt.Errorf("no .gomi interface file in %s", dir)
	case 1:
		return ms[0], nil
	default:
		return "", fmt.Errorf("several .gomi files in %s — a package has exactly one interface", dir)
	}
}

// extract turns a compiled interface's typed declarations into the
// importable member table plus this loader's cross-phase tables.
// Symbols are mutated in place — requalified to path — which is
// sound because the .gomi compile is private to this load: the same
// symbol objects then serve the member table, the signature table,
// the type-declaration tables and the pattern-time constructor
// tables, so identity stays consistent across every consumer.
func (l *Loader) extract(path string, tf *check.File) (*resolve.Package, error) {
	if tf == nil {
		return nil, fmt.Errorf("interface produced no typed declarations")
	}
	own := map[string]bool{}
	for _, d := range tf.Decls {
		if td, ok := d.(*check.TypeDecl); ok && td.Sym != nil {
			own[td.Sym.Name] = true
		}
	}
	p := &resolve.Package{Path: path, Members: map[string]*resolve.Symbol{}}
	for _, d := range tf.Decls {
		switch d := d.(type) {
		case *check.FnDecl:
			sym := d.Sym
			if sym == nil || sym.TestOnly {
				continue
			}
			sym.Package = path
			p.Members[sym.Name] = sym
			sig := check.Signature{Type: requalify(d.Sig, own, path)}
			if !d.Thunk && d.Sig != nil {
				sig.DeclArity = len(d.Sig.Params)
				l.arities[sym] = len(d.Sig.Params)
			}
			l.sigs[sym] = sig
		case *check.LetDecl:
			for _, sym := range letSyms(d.Pat) {
				if sym == nil || sym.TestOnly {
					continue
				}
				sym.Package = path
				p.Members[sym.Name] = sym
				l.sigs[sym] = check.Signature{
					Type: requalify(patType(d.Pat, sym), own, path),
				}
			}
		case *check.TypeDecl:
			if d.Sym == nil {
				continue
			}
			d.Sym.Package = path
			l.requalifyDecl(d, own, path)
			l.tdecls = append(l.tdecls, d)
			l.byCon[conKey{qual: path, name: d.Sym.Name}] = d
			p.Members[d.Sym.Name] = d.Sym
			// The type's own signature is its constructor — what
			// `pkg.T` in annotation position resolves to (arity
			// included for generic declarations). An alias answers
			// the aliased type itself instead.
			if al, ok := d.Body.(*check.AliasType); ok {
				l.sigs[d.Sym] = check.Signature{Type: requalify(al.Type, own, path)}
			} else {
				args := make([]types.Type, len(d.TypeParams))
				for i, v := range d.TypeParams {
					args[i] = v
				}
				l.sigs[d.Sym] = check.Signature{
					Type: &types.Con{Qual: path, Name: d.Sym.Name, Args: args},
				}
			}
			if s, ok := d.Body.(*check.SumType); ok {
				parent := &types.Con{Qual: path, Name: d.Sym.Name, Args: conArgs(d.TypeParams)}
				// The per-type ctor table `pkg.Type.Ctor`
				// resolves against (resolve.CtorsOf).
				ctab := make(map[string]*resolve.Symbol, len(s.Variants))
				for i := range s.Variants {
					v := &s.Variants[i]
					if v.Sym == nil {
						continue
					}
					v.Sym.Package = path
					p.Members[v.Sym.Name] = v.Sym
					ctab[v.Sym.Name] = v.Sym
					params := make([]types.FuncParam, len(v.Fields))
					for j, f := range v.Fields {
						params[j] = types.FuncParam{Label: f.Label, Type: f.Type}
					}
					var sig types.Type = parent
					if len(params) > 0 {
						sig = &types.Func{Params: params, Ret: parent}
					}
					l.sigs[v.Sym] = check.Signature{Type: sig}
					l.ctorFields[v.Sym] = fieldTable(v.Fields)
				}
				l.typeCtors[d.Sym] = ctab
			}
			if rc, ok := d.Body.(*check.RecordType); ok {
				l.ctorFields[d.Sym] = fieldTable(rc.Fields)
			}
		case *check.TestDecl:
			// Interfaces cannot declare tests.
		default:
			panic("unreachable")
		}
	}
	// A member whose generated Go name another member claims
	// misbinds every imported reference — `p.foo` compiles to
	// `p.Foo`, the other member's name. The declaring package's
	// conformance check reports the collision as E0607; marking the
	// loser here lets an importer report E0607 at the reference
	// too, instead of silently emitting the wrong name. The mark
	// replays codegen's claiming over the member table: types take
	// `ExportName` first, then term members in (base, Gom name)
	// order — a constructor member emits type-qualified
	// (`T_Variant`) and never claims a bare name.
	var typeMembers, termMembers []*resolve.Symbol
	for _, m := range p.Members {
		switch m.Kind {
		case resolve.KindType:
			typeMembers = append(typeMembers, m)
		case resolve.KindVar, resolve.KindFunc:
			termMembers = append(termMembers, m)
		}
	}
	sort.Slice(typeMembers, func(i, j int) bool {
		return typeMembers[i].Name < typeMembers[j].Name
	})
	sort.Slice(termMembers, func(i, j int) bool {
		bi, bj := core.ExportName(termMembers[i].Name), core.ExportName(termMembers[j].Name)
		if bi != bj {
			return bi < bj
		}
		return termMembers[i].Name < termMembers[j].Name
	})
	claimed := map[string]*resolve.Symbol{}
	for _, m := range typeMembers {
		base := core.ExportName(m.Name)
		if claimed[base] == nil {
			claimed[base] = m
		} else {
			m.EmitClash = base
		}
	}
	for _, m := range termMembers {
		base := core.ExportName(m.Name)
		if claimed[base] == nil {
			claimed[base] = m
		} else {
			m.EmitClash = base
		}
	}
	return p, nil
}

// requalifyDecl rewrites a loaded type declaration's field and
// alias types so every reference to one of the package's own type
// names carries the import path — `Shape` becomes
// `example.com/m/shapes.Shape` — while builtin and other-package
// names stay untouched.
func (l *Loader) requalifyDecl(td *check.TypeDecl, own map[string]bool, path string) {
	switch body := td.Body.(type) {
	case *check.SumType:
		for i := range body.Variants {
			for j := range body.Variants[i].Fields {
				body.Variants[i].Fields[j].Type =
					requalify(body.Variants[i].Fields[j].Type, own, path)
			}
		}
	case *check.RecordType:
		for j := range body.Fields {
			body.Fields[j].Type = requalify(body.Fields[j].Type, own, path)
		}
	case *check.AliasType:
		body.Type = requalify(body.Type, own, path)
	case *check.AbstractType, nil:
	default:
		panic("unreachable")
	}
}

// --- the checker seam ---

// SignatureOf returns the signature of a member symbol — the
// check.SignatureSource entry. Symbols this loader minted answer
// from the loaded tables; anything else defers to Config.Sigs so a
// chain built on this loader still answers Go members and stdlib
// modules.
func (l *Loader) SignatureOf(sym *resolve.Symbol) (check.Signature, bool) {
	if sig, ok := l.sigs[sym]; ok {
		return sig, true
	}
	if l.cfg.Sigs != nil {
		return l.cfg.Sigs.SignatureOf(sym)
	}
	return check.Signature{}, false
}

// TypeDecls returns every loaded package's (requalified) type
// declarations — the check.TypeDeclSource entry.
func (l *Loader) TypeDecls() []*check.TypeDecl {
	return l.tdecls
}

// MethodOf defers the optional check.MethodSource protocol to
// Config.Sigs — this loader mints no Go method symbols.
func (l *Loader) MethodOf(recv types.Type, name string) (*resolve.Symbol, check.Signature, bool) {
	if ms, ok := l.cfg.Sigs.(check.MethodSource); ok {
		return ms.MethodOf(recv, name)
	}
	return nil, check.Signature{}, false
}

// InterfaceAssignable defers check.InterfaceSource to Config.Sigs.
func (l *Loader) InterfaceAssignable(from, to types.Type) bool {
	if is, ok := l.cfg.Sigs.(check.InterfaceSource); ok {
		return is.InterfaceAssignable(from, to)
	}
	return false
}

// FieldOf defers check.FieldSource to Config.Sigs.
func (l *Loader) FieldOf(recv types.Type, name string) (*resolve.Symbol, check.Signature, types.Type, bool) {
	if fs, ok := l.cfg.Sigs.(check.FieldSource); ok {
		return fs.FieldOf(recv, name)
	}
	return nil, check.Signature{}, nil, false
}

// GoComparable defers check.ComparableSource to Config.Sigs.
func (l *Loader) GoComparable(t types.Type) bool {
	if cs, ok := l.cfg.Sigs.(check.ComparableSource); ok {
		return cs.GoComparable(t)
	}
	return false
}

// GoConvertible defers check.ConvertSource to Config.Sigs.
func (l *Loader) GoConvertible(from, to types.Type) (bool, bool) {
	if cs, ok := l.cfg.Sigs.(check.ConvertSource); ok {
		return cs.GoConvertible(from, to)
	}
	return false, false
}

// --- the resolver seam ---

// CtorFieldsOf returns the named payload slots of a constructor (or
// record type) the loaded packages declare — the
// resolve.Config.CtorFields hook, keyed by field param name like
// the resolver's own tables.
func (l *Loader) CtorFieldsOf(ctor *resolve.Symbol) map[string]*resolve.Symbol {
	return l.ctorFields[ctor]
}

// CtorsOf returns the constructor table of a type the loaded
// packages declare — the resolve.Config.Ctors hook that unlocks
// `pkg.Type.Ctor`. Records, aliases and abstract types have no
// constructors and answer nil, like a symbol this loader never
// minted.
func (l *Loader) CtorsOf(typeSym *resolve.Symbol) map[string]*resolve.Symbol {
	return l.typeCtors[typeSym]
}

// --- the lowering seam ---

// ArityOf returns a loaded function member's declared arity — the
// lower.Env.FuncArity hook shape — or -1 when sym is not a loaded
// function.
func (l *Loader) ArityOf(sym *resolve.Symbol) int {
	if a, ok := l.arities[sym]; ok {
		return a
	}
	return -1
}

// Variants returns the constructor set of a loaded sum type —
// lower.TypeOracle — with field types instantiated at the queried
// constructor's arguments.
func (l *Loader) Variants(c *types.Con) []lower.Variant {
	td := l.byCon[conKey{qual: c.Qual, name: c.Name}]
	if td == nil {
		return nil
	}
	s, ok := td.Body.(*check.SumType)
	if !ok {
		return nil
	}
	sub := instantiate(td.TypeParams, c.Args)
	out := make([]lower.Variant, len(s.Variants))
	for i, v := range s.Variants {
		fields := make([]check.TField, len(v.Fields))
		for j, f := range v.Fields {
			fields[j] = f
			fields[j].Type = sub.Apply(f.Type)
		}
		out[i] = lower.Variant{Sym: v.Sym, Fields: fields}
	}
	return out
}

// Fields returns a loaded record type's fields in declaration
// order — lower.TypeOracle — instantiated at the queried
// constructor's arguments.
func (l *Loader) Fields(c *types.Con) []check.TField {
	td := l.byCon[conKey{qual: c.Qual, name: c.Name}]
	if td == nil {
		return nil
	}
	rc, ok := td.Body.(*check.RecordType)
	if !ok {
		return nil
	}
	sub := instantiate(td.TypeParams, c.Args)
	out := make([]check.TField, len(rc.Fields))
	for i, f := range rc.Fields {
		out[i] = f
		out[i].Type = sub.Apply(f.Type)
	}
	return out
}

// conArgs turns declared type parameters into the constructor's
// own argument list — `type Shape a` applied to itself.
func conArgs(params []*types.Var) []types.Type {
	args := make([]types.Type, len(params))
	for i, v := range params {
		args[i] = v
	}
	return args
}

// instantiate maps a declaration's type parameters to a queried
// constructor's arguments.
func instantiate(params []*types.Var, args []types.Type) types.Subst {
	sub := types.Subst{}
	for i, v := range params {
		if i < len(args) {
			sub[v.ID] = args[i]
		}
	}
	return sub
}

// fieldTable builds the param-name -> field-symbol map
// resolve.Config.CtorFields expects — labeled and named slots
// only; positional payload is addressed by index.
func fieldTable(fields []check.TField) map[string]*resolve.Symbol {
	var table map[string]*resolve.Symbol
	for _, f := range fields {
		name := f.Label
		if name == "" {
			name = f.Name
		}
		if name == "" || f.Sym == nil {
			continue
		}
		if table == nil {
			table = map[string]*resolve.Symbol{}
		}
		table[name] = f.Sym
	}
	return table
}

// letSyms collects the variable symbols a top-level let's pattern
// binds — the member names a `let` contributes.
func letSyms(p check.Pat) []*resolve.Symbol {
	var out []*resolve.Symbol
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch n := p.(type) {
		case nil:
		case *check.VarPat:
			out = append(out, n.Sym)
		case *check.LitPat, *check.WildPat:
			// Literal and wildcard patterns bind no symbols.
		case *check.CtorPat:
			for _, a := range n.Args {
				walk(a.Pat)
			}
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.TuplePat:
			for _, e := range n.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.ListPat:
			for _, e := range n.Elems {
				walk(e)
			}
			walk(n.Rest)
		case *check.OrPat:
			if len(n.Alts) > 0 {
				walk(n.Alts[0])
			}
		case *check.AsPat:
			walk(n.Pat)
			out = append(out, n.Sym)
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
}

// patType reports the type one bound symbol of a let pattern
// carries — the let's declared or inferred member type.
func patType(p check.Pat, sym *resolve.Symbol) types.Type {
	var found types.Type
	var walk func(p check.Pat)
	walk = func(p check.Pat) {
		switch n := p.(type) {
		case nil:
		case *check.VarPat:
			if n.Sym == sym {
				found = n.Type()
			}
		case *check.LitPat, *check.WildPat:
			// Literal and wildcard patterns bind no symbols.
		case *check.CtorPat:
			for _, a := range n.Args {
				walk(a.Pat)
			}
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.TuplePat:
			for _, e := range n.Elems {
				walk(e)
			}
		case *check.RecordPat:
			for i := range n.Fields {
				walk(n.Fields[i].Pat)
			}
		case *check.ListPat:
			for _, e := range n.Elems {
				walk(e)
			}
			walk(n.Rest)
		case *check.OrPat:
			if len(n.Alts) > 0 {
				walk(n.Alts[0])
			}
		case *check.AsPat:
			walk(n.Pat)
			if n.Sym == sym {
				found = n.Type()
			}
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return found
}

// requalify rewrites t so constructor references to the package's
// own type names carry the import path. Every other constructor —
// builtin-scope and other-package names — keeps its qualifier;
// Var/Meta are shared leaves.
func requalify(t types.Type, own map[string]bool, path string) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Var:
		return t
	case *types.Meta:
		return t
	case *types.Con:
		c := &types.Con{Qual: t.Qual, Name: t.Name}
		if c.Qual == "" && own[c.Name] {
			c.Qual = path
		}
		c.Args = requalifyAll(t.Args, own, path)
		return c
	case *types.Func:
		params := make([]types.FuncParam, len(t.Params))
		for i, fp := range t.Params {
			params[i] = types.FuncParam{Label: fp.Label,
				Type: requalify(fp.Type, own, path)}
		}
		return &types.Func{Params: params,
			Ret: requalify(t.Ret, own, path)}
	case *types.Tuple:
		return &types.Tuple{Elems: requalifyAll(t.Elems, own, path)}
	case *types.Go:
		return &types.Go{Ref: t.Ref, Args: requalifyAll(t.Args, own, path)}
	default:
		panic("unreachable")
	}
}

// requalifyAll maps requalify over a type slice.
func requalifyAll(ts []types.Type, own map[string]bool, path string) []types.Type {
	if len(ts) == 0 {
		return ts
	}
	out := make([]types.Type, len(ts))
	for i, t := range ts {
		out[i] = requalify(t, own, path)
	}
	return out
}

// --- the implementation-import cycle check (E0606) ---

// ensureImplScan runs the implementation-import cycle check once, on
// the first Gom load. The .gomi loading guard catches interface
// cycles, but a loaded package's .gom files may import back toward
// the package being compiled — a cycle the generated Go packages
// would only hit at `go build` ("import cycle not allowed"). The
// check seeds the graph at the entry directory's own .gom files and
// expands each reached package's .gom import clauses on demand; the
// first cycle found becomes the loader's verdict for every Gom load.
func (l *Loader) ensureImplScan() {
	if l.implScanned {
		return
	}
	l.implScanned = true
	entry, err := l.entryPath()
	if err != nil || entry == "" {
		// No module context — no Gom package can load anyway (dirOf
		// answers the same failure), so there is nothing to check.
		return
	}
	l.entry = entry
	visiting := map[string]bool{}
	var stack []string
	if chain := l.visitImpl(entry, visiting, &stack); chain != nil {
		l.implCycleErr = &resolve.ImportCycleError{Chain: chain}
		// The members of the loop are the distinct chain nodes —
		// only their imports close a cycle; loads outside the set
		// proceed normally even though a cycle exists elsewhere.
		l.implCycleSet = map[string]bool{}
		for _, u := range chain {
			l.implCycleSet[u] = true
		}
	}
}

// visitImpl is the DFS over .gom import clauses: reaching the entry
// package closes a mutual cycle, reaching an on-stack package closes
// a cycle internal to the dependency graph. Children are visited in
// sorted order so the reported chain is deterministic. A node that
// cannot reach the entry or its own ancestors is marked done — the
// graph is static (each package's .gom files are read once), so the
// verdict is final.
func (l *Loader) visitImpl(u string, visiting map[string]bool, stack *[]string) []string {
	if u == l.entry && len(*stack) > 0 {
		return append(append([]string{}, *stack...), u)
	}
	if visiting[u] {
		// The cycle is the stack suffix that starts at u.
		for i, s := range *stack {
			if s == u {
				return append(append([]string{}, (*stack)[i:]...), u)
			}
		}
		return append(append([]string{}, u), u)
	}
	if l.implDone[u] {
		return nil
	}
	visiting[u] = true
	*stack = append(*stack, u)
	edges := l.implImports(u)
	keys := make([]string, 0, len(edges))
	for q := range edges {
		keys = append(keys, q)
	}
	sort.Strings(keys)
	for _, q := range keys {
		if chain := l.visitImpl(q, visiting, stack); chain != nil {
			return chain
		}
	}
	*stack = (*stack)[:len(*stack)-1]
	delete(visiting, u)
	l.implDone[u] = true
	return nil
}

// implImports returns the Gom import paths declared by the .gom files
// of package u — the edges the generated Go package will carry. The
// entry package answers from Config.Dir; other packages resolve
// through dirOf, so a package outside the main module (or one with no
// .gom files) contributes no edges. Files that fail to parse still
// yield whatever import clauses the parser recovered.
func (l *Loader) implImports(u string) map[string]bool {
	if m, ok := l.implEdges[u]; ok {
		return m
	}
	m := map[string]bool{}
	l.implEdges[u] = m
	var dir string
	if u == l.entry {
		dir = l.cfg.Dir
	} else {
		d, err := l.dirOf(u)
		if err != nil {
			return m
		}
		dir = d
	}
	ms, err := filepath.Glob(filepath.Join(dir, "*.gom"))
	if err != nil {
		return m
	}
	for _, f := range ms {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		ast, _ := syntax.Parse(src, f)
		if ast == nil {
			continue
		}
		for _, d := range ast.Decls {
			if imp, ok := d.(*syntax.ImportDecl); ok && !imp.Go {
				m[imp.Path] = true
			}
		}
	}
	return m
}

// entryPath computes the entry package's import path: Config.Dir
// rendered module-relative — the package generated output for the
// current compile lands in. An empty result means Config.Dir sits
// outside the main module, where no Gom package can resolve.
func (l *Loader) entryPath() (string, error) {
	modPath, modDir, err := l.module()
	if err != nil {
		return "", err
	}
	dir, err := filepath.Abs(l.cfg.Dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(modDir, dir)
	if err != nil {
		return "", err
	}
	switch {
	case rel == ".":
		return modPath, nil
	case rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		return "", nil
	default:
		return modPath + "/" + filepath.ToSlash(rel), nil
	}
}
