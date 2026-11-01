package resolve

import (
	"github.com/podhmo/gom/internal/syntax"
)

// resolveDecls resolves declaration bodies and signatures — the
// second pass, run once every top-level name is bound.
func (r *resolver) resolveDecls(f *syntax.File, file *scope) {
	for _, d := range f.Decls {
		switch decl := d.(type) {
		case *syntax.FnDecl:
			r.resolveFn(decl, file)
		case *syntax.LetDecl:
			r.resolveLetDecl(decl, file)
		case *syntax.TypeDecl:
			r.resolveTypeDecl(decl, file)
		case *syntax.ExternDecl:
			r.resolveExtern(decl, file)
		case *syntax.TestDecl:
			r.withTest(func() {
				// a test block is a fresh signature-less scope for
				// type variables — no outer declaration to see
				r.withTV(&tvScope{implicit: true}, func() {
					r.expr(decl.Body, file)
				})
			})
		case *syntax.ImportDecl:
			// bound and loaded in the collection pass
		default:
			panic("unreachable")
		}
	}
}

// useTerm records a term-level reference: it resolves id through the
// scope chain and emits the undefined-name diagnostic when the name
// does not resolve (r.use handles the test-only rule).
func (r *resolver) useTerm(id *syntax.Ident, sc *scope) {
	if id == nil {
		return
	}
	sym := sc.lookupTerm(id.Name)
	if sym == nil {
		r.undefinedTerm(id)
		return
	}
	r.use(id, sym)
}

// use records an ident→symbol reference, enforcing the test-only
// visibility rule of spec section 11.3.
func (r *resolver) use(id *syntax.Ident, sym *Symbol) {
	r.uses[id] = sym
	if sym.TestOnly && !r.inTest {
		r.testOnlyRef(id, sym)
	}
}

// withTest runs f in a test-only context.
func (r *resolver) withTest(f func()) {
	old := r.inTest
	r.inTest = true
	f()
	r.inTest = old
}

// withTV runs f with tv as the enclosing type-variable scope — the
// scope annotations inside a body consult.
func (r *resolver) withTV(tv *tvScope, f func()) {
	old := r.tv
	r.tv = tv
	f()
	r.tv = old
}

// annScope returns the type-variable scope for an annotation nested
// inside a body: it sees the enclosing signature's variables and
// binds any new implicit variables locally to the annotation.
func (r *resolver) annScope() *tvScope {
	return &tvScope{parent: r.tv, implicit: true}
}

// resolveFn resolves a function declaration or signature: its
// parameter types and return type share one implicit type-variable
// scope, its parameters bind in a fresh function scope, and its body
// resolves under both. A @test declaration's body resolves in a
// test-only context.
//
// Parameter binders are recorded even for a bodiless signature (a
// .gomi `fn` declaration): the scope is disposable, like an extern
// entry's, but every binder ident still gets its Defs entry.
func (r *resolver) resolveFn(decl *syntax.FnDecl, file *scope) {
	tv := &tvScope{implicit: true}
	for i := range decl.Params {
		p := &decl.Params[i]
		if p.Type != nil {
			r.typeExpr(p.Type, file, tv)
		}
	}
	if decl.Ret != nil {
		r.typeExpr(decl.Ret, file, tv)
	}
	fn := pushScope(file)
	r.bindParams(decl.Params, fn)
	if decl.Body == nil {
		return
	}
	body := func() { r.expr(decl.Body, fn) }
	// the body sees the signature's type variables
	run := func() { r.withTV(tv, body) }
	if decl.TestOnly {
		r.withTest(run)
	} else {
		run()
	}
}

// resolveLetDecl resolves a top-level `let pat : T = e`: annotation
// and value in the file scope (the pattern was already bound during
// collection so file-scope names are mutually visible).
func (r *resolver) resolveLetDecl(decl *syntax.LetDecl, file *scope) {
	tv := &tvScope{implicit: true}
	if decl.Type != nil {
		r.typeExpr(decl.Type, file, tv)
	}
	if decl.Value == nil {
		return
	}
	body := func() { r.expr(decl.Value, file) }
	// the value sees the annotation's type variables
	run := func() { r.withTV(tv, body) }
	if decl.TestOnly {
		r.withTest(run)
	} else {
		run()
	}
}

// resolveTypeDecl resolves the field and alias types of a type
// declaration inside its declared type-parameter scope — undeclared
// type variables are errors here, not implicitly bound.
func (r *resolver) resolveTypeDecl(decl *syntax.TypeDecl, file *scope) {
	tv := &tvScope{}
	for _, p := range decl.Params {
		if p == nil {
			continue
		}
		sym := &Symbol{Name: p.Name, Kind: KindTypeVar, Def: p}
		r.defs[p] = sym
		if prev := tv.vars[p.Name]; prev != nil {
			r.dupDef(p, prev)
		}
		tv.defineVar(p.Name, sym)
	}
	switch body := decl.Body.(type) {
	case *syntax.SumBody:
		for i := range body.Variants {
			r.fieldTypes(body.Variants[i].Params, file, tv)
		}
	case *syntax.RecordBody:
		r.fieldTypes(body.Fields, file, tv)
	case *syntax.AliasBody:
		if body.Type != nil {
			r.typeExpr(body.Type, file, tv)
		}
	case *syntax.AbstractBody:
	case nil:
	default:
		panic("unreachable")
	}
}

// fieldTypes resolves the type expressions of a field list in the
// declaration's type-variable scope.
func (r *resolver) fieldTypes(fields []syntax.Field, file *scope, tv *tvScope) {
	for i := range fields {
		if fields[i].Type != nil {
			r.typeExpr(fields[i].Type, file, tv)
		}
	}
}

// resolveExtern resolves the signatures inside an `extern go "pkg"`
// block. Type names in an extern entry name Go-package members first
// — `extern go "io" { fn Copy (~dst: Writer) ... }` means io.Writer —
// and fall back to the ordinary scope (spec section 8.4). Parameter
// binders get symbols for Defs completeness; nothing is in scope to
// use them since extern entries have no bodies.
func (r *resolver) resolveExtern(decl *syntax.ExternDecl, file *scope) {
	var members map[string]*Symbol
	if pkg := r.loadExternPackage(decl); pkg != nil {
		members = pkg.Members
	}
	for _, e := range decl.Entries {
		fn, ok := e.(*syntax.ExternFn)
		if !ok {
			continue
		}
		tv := &tvScope{implicit: true}
		for i := range fn.Params {
			p := &fn.Params[i]
			if p.Type != nil {
				r.typeExprExtern(p.Type, file, tv, members)
			}
		}
		if fn.Ret != nil {
			r.typeExprExtern(fn.Ret, file, tv, members)
		}
		r.bindParams(fn.Params, pushScope(file))
	}
}

// loadExternPackage loads the member table an extern block overrides,
// without diagnostics: a missing loader simply leaves extern type
// names to the ordinary scope. Unlike imports the path literal is not
// a binding occurrence, so E0203 belongs to `import`, not `extern`.
func (r *resolver) loadExternPackage(decl *syntax.ExternDecl) *Package {
	if r.cfg.Loader == nil {
		return nil
	}
	key := pkgKey{path: decl.Path, go_: true}
	if pkg, ok := r.pkgCache[key]; ok {
		return pkg
	}
	pkg, err := r.cfg.Loader.Load(decl.Path, true)
	if err != nil {
		r.pkgCache[key] = nil
		return nil
	}
	r.pkgCache[key] = pkg
	return pkg
}

// bindParams binds the pattern binders of a parameter list into sc
// and records the parameter labels. Parameters are sibling binders on
// one path, so they share the duplicate table — `fn f x x` is one
// duplicate definition of x. A `~name` label of a VarPat parameter
// defines the same variable symbol as the pattern — the label ident
// is the parameter's external name for that variable. A label on a
// non-variable (or differently named) pattern gets its own KindVar
// symbol: it declares the parameter's external name, which is
// otherwise bound to no variable.
func (r *resolver) bindParams(params []syntax.Param, sc *scope) {
	seen := make(map[string]*syntax.Ident)
	for i := range params {
		p := &params[i]
		if p.Pat != nil {
			r.bindPatSeen(p.Pat, sc, seen, nil)
		}
		if p.Label != nil {
			if vp, ok := p.Pat.(*syntax.VarPat); ok && vp.Name != nil && vp.Name.Name == p.Label.Name {
				r.defs[p.Label] = r.defs[vp.Name]
				continue
			}
			r.defs[p.Label] = &Symbol{Name: p.Label.Name, Kind: KindVar, Def: p}
		}
	}
}
