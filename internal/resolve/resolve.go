package resolve

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

// Package is the exported surface of an imported package as the
// resolver sees it: a flat member table. Members are *Symbol values
// created by whoever loaded the package, so they are shared across
// every file that imports it — symbol identity stays meaningful at
// package granularity (docs/decisions/2026-11-02-ast-representation.md).
type Package struct {
	// Path is the import path as written in the import declaration.
	Path string
	// Members maps exported member name to its symbol. Member symbols
	// of a Go package carry IsGo; Def is nil for members without a
	// Gom-source definition.
	Members map[string]*Symbol
}

// Loader supplies the member table of an imported package — the seam
// where dependency information enters name resolution (docs/design.md
// section 2.5). internal/goimport implements it for `import go` via
// go/packages (P1-9, P3-1) and the .gomi reader implements it for Gom
// packages (P4-1). goPkg distinguishes `import go "path"` from
// `import "path"`.
//
// A nil Loader means the compiler was invoked without a package
// loader; every import then reports E0203 at the import site.
type Loader interface {
	// Load returns the exported members of the package at path. An
	// error means the package could not be resolved (missing .gomi,
	// unknown Go package) and produces E0203.
	Load(path string, goPkg bool) (*Package, error)
}

// MapLoader is a Loader backed by a fixed map of packages — used in
// tests of this and later phases until internal/goimport lands.
type MapLoader map[string]*Package

// Load returns the package registered for path, or an error when the
// path (or the path under the other goPkg convention) is absent.
func (m MapLoader) Load(path string, goPkg bool) (*Package, error) {
	key := path
	if goPkg {
		key = "go:" + path
	}
	if p, ok := m[key]; ok {
		return p, nil
	}
	return nil, fmt.Errorf("package %q not found", path)
}

// Config configures one File resolution. A nil *Config is valid and
// means: no package loader (every import reports E0203) and no
// standard-library module members.
type Config struct {
	// Loader resolves imported packages. Nil disables imports.
	Loader Loader

	// Modules provides member tables for builtin-scope modules —
	// the standard-library modules named after builtin types
	// (List, Option, Result, Chan, Task, Ref, Array), keyed by module
	// name. It is the extension point for stdlib injection before
	// stdlib .gomi packages exist (P4-8); the compiler-owned `Go`
	// module always has its hardcoded table and is not looked up here.
	Modules map[string]map[string]*Symbol
}

// resolver carries the state of one file's name resolution.
type resolver struct {
	cfg Config

	defs  map[*syntax.Ident]*Symbol
	uses  map[*syntax.Ident]*Symbol
	diags diag.List

	// ctorsOf maps a type symbol to its constructor name table, for
	// `T.Ctor` / `pkg.Ctor` resolution.
	ctorsOf map[*Symbol]map[string]*Symbol
	// ctorFields maps a file-local constructor symbol to its named
	// payload slots (Field.ParamName() -> field symbol), used to
	// resolve `{ name = pat }` fields and `~label:` arguments of
	// constructor patterns.
	ctorFields map[*Symbol]map[string]*Symbol
	// members maps a module symbol (imported or builtin) to its
	// member table. A nil entry marks a module whose package failed
	// to load (already diagnosed at the import site — member lookups
	// are suppressed, not re-diagnosed).
	members map[*Symbol]map[string]*Symbol
	// externs collects `extern go` entries per package so they can
	// overlay the loaded member table (spec section 8.4). Keyed like
	// pkgCache — `extern go` declarations always name a Go package,
	// so they must not overlay a same-pathed Gom import.
	externs map[pkgKey]map[string]*Symbol
	// pkgCache deduplicates Loader calls per (path, goPkg).
	pkgCache map[pkgKey]*Package

	// inTest is true while resolving a `test` block or the body of an
	// @test declaration — the contexts allowed to reference test-only
	// symbols (spec section 11.3).
	inTest bool
	// tv is the enclosing declaration's type-variable scope while a
	// body is being resolved — annotations inside the body see the
	// signature's variables through it.
	tv *tvScope
}

// pkgKey is the cache key for loaded packages.
type pkgKey struct {
	path string
	go_  bool
}

// File resolves the names of one .gom file: it binds every identifier
// occurrence to a *Symbol in the Defs/Uses side tables and returns the
// diagnostics produced while doing so. Resolution continues after
// errors — the returned tables are as complete as the input allows —
// so a diagnostic never aborts the walk.
//
// Resolution is single-file: the package clause is not validated
// against other files, and top-level names are mutually visible in
// the whole file regardless of source order.
//
// After resolution an identifier has exactly one of three states:
//   - in Defs (it introduces a symbol),
//   - in Uses (it refers to a symbol),
//   - absent from both. Absence means either the position is not a
//     symbol occurrence (the package name, call-site `~labels` and
//     `~name:` parameter names in fn-types, record-literal field
//     names, member selectors on non-module receivers — all resolved
//     later by check), the position needs type information (record-
//     pattern and foreign-constructor field names), or resolution
//     deferred/failed at an enclosing point already diagnosed (a
//     member of a package that failed to load).
func File(f *syntax.File, cfg *Config) (*Resolved, diag.List) {
	c := Config{}
	if cfg != nil {
		c = *cfg
	}
	r := &resolver{
		cfg:        c,
		defs:       make(map[*syntax.Ident]*Symbol),
		uses:       make(map[*syntax.Ident]*Symbol),
		ctorsOf:    make(map[*Symbol]map[string]*Symbol),
		ctorFields: make(map[*Symbol]map[string]*Symbol),
		members:    make(map[*Symbol]map[string]*Symbol),
		externs:    make(map[pkgKey]map[string]*Symbol),
		pkgCache:   make(map[pkgKey]*Package),
	}
	builtins := builtinScope()
	r.wireBuiltinCtors(builtins)
	file := pushScope(builtins)

	r.collect(f, file)
	r.resolveDecls(f, file)

	return &Resolved{File: f, Defs: r.defs, Uses: r.uses}, r.diags
}

// wireBuiltinCtors links the builtin Option/Result constructor
// symbols to their type symbols so `Option.Some` and pattern
// resolution work like file-declared constructors.
func (r *resolver) wireBuiltinCtors(builtins *scope) {
	ctors := map[string][]string{
		"Option": {"Some", "None"},
		"Result": {"Ok", "Err"},
	}
	for tname, cnames := range ctors {
		tsym := builtins.types[tname]
		table := make(map[string]*Symbol, len(cnames))
		for _, cn := range cnames {
			table[cn] = builtins.terms[cn]
		}
		r.ctorsOf[tsym] = table
	}
}

// collect performs the declaration pass: it binds every top-level
// name into the file scope (mutual visibility, source order
// irrelevant inside bodies), records constructor/field side tables,
// and loads imported packages. Sub-passes order the work so that
// later steps see the names they need:
//
//  1. extern entries — so import member tables can overlay them
//  2. type names — so constructor binding and type references see them
//  3. constructors and fields — so let-patterns resolve ctor references
//  4. fn/let/import binders — so every top-level name exists
//  5. imports — load member tables once all externs are known
func (r *resolver) collect(f *syntax.File, file *scope) {
	// Pass 1: extern member symbols (Def'd here; their types resolve
	// with the bodies).
	for _, d := range f.Decls {
		ex, ok := d.(*syntax.ExternDecl)
		if !ok {
			continue
		}
		// `extern go` always names a Go package (spec section 8.4)
		key := pkgKey{path: ex.Path, go_: true}
		table := r.externs[key]
		if table == nil {
			table = make(map[string]*Symbol)
			r.externs[key] = table
		}
		for _, e := range ex.Entries {
			fn, ok := e.(*syntax.ExternFn)
			if !ok {
				continue
			}
			if fn.Name == nil {
				continue
			}
			sym := &Symbol{Name: fn.Name.Name, Kind: KindFunc, IsGo: true,
				Package: ex.Path, Def: fn}
			r.defs[fn.Name] = sym
			// two entries of one name in one extern table — same
			// scope, same name — are an ordinary duplicate
			if prev, dup := table[fn.Name.Name]; dup {
				r.dupDef(fn.Name, prev)
			}
			table[fn.Name.Name] = sym
		}
	}

	// Pass 2: type names.
	for _, d := range f.Decls {
		td, ok := d.(*syntax.TypeDecl)
		if !ok || td.Name == nil {
			continue
		}
		sym := &Symbol{Name: td.Name.Name, Kind: KindType, Def: td}
		r.defs[td.Name] = sym
		if prev := file.defineType(sym.Name, sym); prev != nil {
			r.dupDef(td.Name, prev)
		}
	}

	// Pass 3: constructors and their field tables.
	for _, d := range f.Decls {
		td, ok := d.(*syntax.TypeDecl)
		if !ok {
			continue
		}
		tsym := r.defs[td.Name]
		switch body := td.Body.(type) {
		case nil:
		case *syntax.SumBody:
			table := make(map[string]*Symbol, len(body.Variants))
			for i := range body.Variants {
				v := &body.Variants[i]
				if v.Name == nil {
					continue
				}
				cs := &Symbol{Name: v.Name.Name, Kind: KindCtor, Def: v}
				r.defs[v.Name] = cs
				table[v.Name.Name] = cs
				if prev := file.defineTerm(v.Name.Name, cs); prev != nil {
					r.dupDef(v.Name, prev)
				}
				r.ctorFields[cs] = r.bindFields(v.Params)
			}
			if tsym != nil {
				r.ctorsOf[tsym] = table
			}
		case *syntax.RecordBody:
			if tsym != nil {
				r.ctorFields[tsym] = r.bindFields(body.Fields)
			}
		case *syntax.AliasBody, *syntax.AbstractBody:
			// no constructors or fields to collect
		default:
			panic("unreachable")
		}
	}

	// Pass 4a: import aliases — bound before let-patterns are walked
	// so `pkg.Ctor` heads resolve regardless of source order (all
	// top-level names are mutually visible).
	for _, d := range f.Decls {
		if decl, ok := d.(*syntax.ImportDecl); ok {
			r.bindImport(decl, file)
		}
	}

	// Pass 4b: fn and let binders.
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *syntax.FnDecl:
			if decl.Name == nil {
				continue
			}
			sym := &Symbol{Name: decl.Name.Name, Kind: KindFunc, Def: decl,
				TestOnly: decl.TestOnly}
			r.defs[decl.Name] = sym
			if prev := file.defineTerm(sym.Name, sym); prev != nil {
				r.dupDef(decl.Name, prev)
			}
		case *syntax.LetDecl:
			r.bindPat(decl.Pat, file)
			if decl.TestOnly {
				for _, id := range binderIdents(decl.Pat) {
					if sym := r.defs[id]; sym != nil {
						sym.TestOnly = true
					}
				}
			}
		case *syntax.TypeDecl, *syntax.ImportDecl, *syntax.ExternDecl, *syntax.TestDecl:
			// bound in other passes (or not bound at all)
		default:
			panic("unreachable")
		}
	}
}

// binderIdents collects the variable-binding idents of a pattern in
// order — the Defs keys created for it. Or-pattern alternatives
// contribute all their binders (they share symbols by name).
func binderIdents(p syntax.Pat) []*syntax.Ident {
	var out []*syntax.Ident
	var walk func(p syntax.Pat)
	walk = func(p syntax.Pat) {
		switch n := p.(type) {
		case nil:
		case *syntax.WildPat, *syntax.LitPat:
		case *syntax.VarPat:
			out = append(out, n.Name)
		case *syntax.CtorPat:
			for i := range n.Args {
				walk(n.Args[i].Pat)
			}
			for i := range n.Fields {
				f := &n.Fields[i]
				if f.Punned() {
					out = append(out, f.Name)
				} else {
					walk(f.Pat)
				}
			}
		case *syntax.TuplePat:
			for _, e := range n.Elems {
				walk(e)
			}
		case *syntax.RecordPat:
			for i := range n.Fields {
				f := &n.Fields[i]
				if f.Punned() {
					out = append(out, f.Name)
				} else {
					walk(f.Pat)
				}
			}
		case *syntax.ListPat:
			for _, e := range n.Elems {
				walk(e)
			}
			if n.Rest != nil {
				walk(n.Rest)
			}
		case *syntax.OrPat:
			for _, e := range n.Alts {
				walk(e)
			}
		case *syntax.AsPat:
			walk(n.Pat)
			out = append(out, n.Name)
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
}

// bindFields allocates KindField symbols for the named payload slots
// or record fields in fields, fills Defs for their name/label idents,
// and returns the param-name -> symbol table. Duplicate field names
// are reported at the second occurrence. Positional payload slots
// (ParamName() == "") get no symbol — they are identified by position
// (check.TField.Index).
func (r *resolver) bindFields(fields []syntax.Field) map[string]*Symbol {
	var table map[string]*Symbol
	seen := make(map[string]diag.Span)
	for i := range fields {
		fld := &fields[i]
		name := fld.ParamName()
		if name == "" {
			continue
		}
		sym := &Symbol{Name: name, Kind: KindField, Def: fld}
		if fld.Name != nil {
			r.defs[fld.Name] = sym
		}
		if fld.Label != nil {
			r.defs[fld.Label] = sym
		}
		if first, dup := seen[name]; dup {
			r.dupDefName(name, fld, first)
		} else {
			seen[name] = fld.Sp
			if table == nil {
				table = make(map[string]*Symbol)
			}
			table[name] = sym
		}
	}
	return table
}

// bindImport binds an import alias into the file scope and loads the
// package's member table through the configured Loader. The module
// symbol is bound even when loading fails, so the import's own
// references stay consistent; member selections on a failed package
// are left unresolved (Uses has no entry) with the error already
// reported at the import site.
func (r *resolver) bindImport(decl *syntax.ImportDecl, file *scope) {
	if decl.Name == nil {
		return
	}
	sym := &Symbol{Name: decl.Name.Name, Kind: KindModule,
		Package: decl.Path, IsGo: decl.Go, Def: decl}
	r.defs[decl.Name] = sym
	if prev := file.defineTerm(sym.Name, sym); prev != nil {
		r.dupDef(decl.Name, prev)
	}

	members, failed := r.loadPackage(decl)
	if failed {
		r.members[sym] = nil // mark: package unavailable
		return
	}
	if overlay, ok := r.externs[pkgKey{path: decl.Path, go_: decl.Go}]; ok {
		merged := make(map[string]*Symbol, len(members)+len(overlay))
		for k, v := range members {
			merged[k] = v
		}
		for k, v := range overlay {
			merged[k] = v
		}
		members = merged
	}
	r.members[sym] = members
}

// loadPackage fetches a package's member table through the Loader,
// caching per (path, goPkg). The second result reports failure, in
// which case an E0203 has already been emitted at the import's path
// literal.
func (r *resolver) loadPackage(decl *syntax.ImportDecl) (members map[string]*Symbol, failed bool) {
	key := pkgKey{path: decl.Path, go_: decl.Go}
	if pkg, ok := r.pkgCache[key]; ok {
		if pkg == nil {
			return nil, true
		}
		return pkg.Members, false
	}
	if r.cfg.Loader == nil {
		r.diags.Add(diag.Error("E0203", fmt.Sprintf("cannot resolve import %q: no package loader configured", decl.Path)).
			At(decl.PathSp).
			Set("path", decl.Path).
			Set("go", decl.Go))
		r.pkgCache[key] = nil
		return nil, true
	}
	pkg, err := r.cfg.Loader.Load(decl.Path, decl.Go)
	if err != nil {
		r.diags.Add(diag.Error("E0203", fmt.Sprintf("cannot resolve import %q", decl.Path)).
			At(decl.PathSp).
			Set("path", decl.Path).
			Set("go", decl.Go).
			Set("reason", err.Error()))
		r.pkgCache[key] = nil
		return nil, true
	}
	r.pkgCache[key] = pkg
	return pkg.Members, false
}
