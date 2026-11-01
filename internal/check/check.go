package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Check type-checks a resolved file and produces the typed AST —
// phase 4 of the pipeline (docs/design.md section 2). The returned
// file is always non-nil; when the diagnostic list contains errors the
// tree is a best-effort recovery (error sites carry metas), and later
// phases must not consume it.
//
// P1 is monomorphic: declared annotations are the source of truth and
// nothing is generalized. Unification still runs locally — to default
// numeric literals, resolve operator overloading, and instantiate the
// rigid variables of annotated signatures and builtin constructors at
// each use site — but a meta left unsolved at the end is E0407 (a
// request for an annotation), never a polymorphic type.
//
// src supplies the signatures of symbols the file does not declare
// (imported members, Go functions and methods); it may be nil. The
// checker itself knows the builtin scope (the Option/Result
// constructors, assert, assert_eq, todo).
func Check(res *resolve.Resolved, src SignatureSource) (*File, diag.List) {
	bv := &builtinTVar{}
	c := &checker{
		res:       res,
		src:       src,
		vals:      map[*resolve.Symbol]types.Type{},
		tyCons:    map[conKey]*typeInfo{},
		tyBySym:   map[*resolve.Symbol]*typeInfo{},
		ctors:     map[*resolve.Symbol]*variantInfo{},
		fieldSyms: map[*resolve.Symbol]*fieldInfo{},
		metas:     map[*types.Meta]*metaRec{},
		builtins:  builtinSigs(bv),
	}
	c.registerBuiltinTypes(bv)
	c.registerTypeDecls(res.File.Decls)
	c.resolveTypeBodies(res.File.Decls)
	c.registerValueDecls(res.File.Decls)
	c.checkDecls(res.File.Decls)
	c.finalize()

	f := &File{Sp: res.File.Sp}
	if res.File.Package != nil {
		f.Package = res.File.Package.Name
		f.PkgSp = res.File.Package.Sp
	}
	f.Imports = c.imports
	f.Decls = c.decls
	return f, c.diags
}

// untypedMeta returns a meta the checker deliberately never records —
// the type of a non-value position (a module name used as a select
// receiver). It is kept off c.metas so finalize does not ask for an
// annotation it can never receive.
func (c *checker) untypedMeta() types.Type {
	return &types.Meta{ID: -1}
}

// checker is the per-file state of the type checker.
type checker struct {
	res *resolve.Resolved
	src SignatureSource

	diags diag.List

	// vals maps every value-level symbol — top-level functions,
	// constructors, let-bound names, parameters, pattern variables —
	// to its type. Identity is the symbol pointer; symbols not
	// declared in this file resolve through src and the builtins.
	vals map[*resolve.Symbol]types.Type

	// The type-declaration tables.
	tyCons    map[conKey]*typeInfo          // declared + builtin types by identity
	tyBySym   map[*resolve.Symbol]*typeInfo // declared types by symbol
	ctors     map[*resolve.Symbol]*variantInfo
	fieldSyms map[*resolve.Symbol]*fieldInfo // record/variant fields by symbol
	records   []*typeInfo                    // record types, for literal resolution

	// metas records every unification variable with its constraints.
	metas    map[*types.Meta]*metaRec
	nextMeta int
	nextVar  int

	// builtins are the signatures known without a source, keyed by
	// name (Option/Result constructors and compiler-provided
	// functions).
	builtins map[string]Signature

	// bounds is the escape-boundary stack for `try` — innermost last.
	bounds  []boundary
	goDepth int

	imports []Import
	decls   []Decl
}

// boundary is one `try` escape target: the innermost enclosing
// function (a fn declaration, a lambda, or a test block). ret is the
// type `try` must agree on an error channel with — a declared return
// type for fn, a meta for lambdas and test blocks.
type boundary struct {
	ret     types.Type
	sp      diag.Span
	goDepth int
	kind    string // "fn", "lambda" or "test" — for messages
}

// declSym returns the symbol bound by a defining identifier, or a
// fabricated one when the Defs table has no entry — the checker stays
// total over resolver gaps rather than failing on a contract issue a
// diagnostic cannot describe.
func (c *checker) declSym(id *syntax.Ident, kind resolve.Kind) *resolve.Symbol {
	if id == nil {
		return nil
	}
	if sym := c.res.DefOf(id); sym != nil {
		return sym
	}
	return &resolve.Symbol{Name: id.Name, Kind: kind, Def: id}
}

// defSymAny returns Defs of the first non-nil ident that has one, else
// a fabricated field symbol named after whichever ident is present.
func (c *checker) defSymAny(a, b *syntax.Ident, kind resolve.Kind) *resolve.Symbol {
	for _, id := range []*syntax.Ident{a, b} {
		if id == nil {
			continue
		}
		if sym := c.res.DefOf(id); sym != nil {
			return sym
		}
	}
	return c.declSym(a, kind)
}

// useSym returns the symbol an identifier in reference position refers
// to, or nil when unbound (the resolver reports the undefined name).
func (c *checker) useSym(id *syntax.Ident) *resolve.Symbol {
	if id == nil {
		return nil
	}
	return c.res.UseOf(id)
}

// sigOf returns the signature of a symbol the file does not declare:
// the declaration environment first (declared fns, lets, constructors),
// then the SignatureSource, then the builtin table keyed by name.
// The returned signature's type may contain rigid variables; callers
// instantiate per use.
func (c *checker) sigOf(sym *resolve.Symbol) (Signature, bool) {
	if t, ok := c.vals[sym]; ok {
		// vals keeps the declared (unflattened) shape for fns, so the
		// declared arity is recoverable from the type itself.
		return Signature{Type: t}, true
	}
	if c.src != nil {
		if sig, ok := c.src.SignatureOf(sym); ok {
			return sig, true
		}
	}
	// The builtin table is keyed by name alone; only builtin-scope
	// symbols (Package == "") may match it — a foreign `Some` or
	// `assert` is an ordinary imported name.
	if sym.Package == "" {
		if sig, ok := c.builtins[sym.Name]; ok {
			return sig, true
		}
	}
	return Signature{}, false
}

// valOf returns the type a symbol denotes at a use site: its declared
// or external signature instantiated with fresh metas. Unknown symbols
// get a fresh meta (the resolver's E0201 covers the cause).
//
// Instantiation applies only to symbols whose signature quantifies
// its type variables — declared fns, constructors, builtins, methods,
// and anything the SignatureSource supplies (an `a` in
// `fn id (x: a) -> a` means "for every a", so each call gets fresh
// metas). A local value binding (KindVar: a let, a parameter, a
// pattern variable) is different: the `a` in its declared type is the
// enclosing declaration's rigid variable, shared by every use of the
// binding. Returning it as-is lets a constraint the body places on
// `x : a` — like `x + 1` — reach the declared `a` itself instead of
// evaporating into a fresh meta.
func (c *checker) valOf(sym *resolve.Symbol, sp diag.Span) types.Type {
	if sym == nil {
		return c.newMeta(sp, "an unresolved name")
	}
	if c.hiddenDiag(sym, sp) {
		return c.newMeta(sp, fmt.Sprintf("the type of `%s`", sym.Name))
	}
	if sig, ok := c.sigOf(sym); ok {
		if _, local := c.vals[sym]; local && sym.Kind == resolve.KindVar {
			return sig.Type
		}
		t, _ := c.instantiate(sig.Type, sp)
		return t
	}
	return c.newMeta(sp, fmt.Sprintf("the type of `%s`", sym.Name))
}

// hiddenDiag reports E0501 for a loaded Go symbol whose signature
// cannot be represented in Gom (spec section 8.5): the resolver
// binds the member so references resolve, and every use site gets
// the reason the loader recorded on the symbol. Returns true when a
// diagnostic was emitted.
func (c *checker) hiddenDiag(sym *resolve.Symbol, sp diag.Span) bool {
	if sym == nil || sym.Hidden == "" {
		return false
	}
	c.diags.Add(diag.Error("E0501",
		fmt.Sprintf("Go symbol `%s.%s` is not exposed: %s", sym.Package, sym.Name, sym.Hidden)).
		At(sp).Set("package", sym.Package).Set("symbol", sym.Name).Set("reason", sym.Hidden))
	return true
}

// base is the Base shared by every typed node.
func baseOf(sp diag.Span, t types.Type) Base {
	return Base{Sp: sp, Typ: t}
}

// checkDecls checks each remaining declaration in order, continuing
// past errors — a failed body marks itself with metas, and the loop
// recovers at the next declaration boundary.
func (c *checker) checkDecls(decls []syntax.Decl) {
	for _, d := range decls {
		switch d := d.(type) {
		case *syntax.TypeDecl:
			// Emitted by resolveTypeBodies.
		case *syntax.ImportDecl:
			if imp, ok := c.importOf(d); ok {
				c.imports = append(c.imports, imp)
			}
		case *syntax.FnDecl:
			c.checkFnDecl(d)
		case *syntax.LetDecl:
			c.checkLetDecl(d)
		case *syntax.ExternDecl:
			c.checkExternDecl(d)
		case *syntax.TestDecl:
			c.checkTestDecl(d)
		default:
			panic("unreachable")
		}
	}
}

// importOf translates a resolved import into the typed Import node.
// The bound module symbol is kept; the path is copied verbatim.
func (c *checker) importOf(d *syntax.ImportDecl) (Import, bool) {
	return Import{
		Sp:   d.Sp,
		Path: d.Path,
		IsGo: d.Go,
		Sym:  c.res.DefOf(d.Name),
	}, true
}

// checkFnDecl checks a function body against its declared signature.
// Parameters carry patterns (a bare name, `()`, or a destructuring
// pattern); each binds against the declared parameter type.
func (c *checker) checkFnDecl(d *syntax.FnDecl) {
	sym := c.declSym(d.Name, resolve.KindFunc)
	sig, ok := types.Chase(c.vals[sym]).(*types.Func)
	if !ok {
		// registerValueDecls always stores a *types.Func for fns.
		sig = c.fnSig(d)
		c.vals[sym] = sig
	}
	params := make([]Param, len(d.Params))
	for i, p := range d.Params {
		var pt types.Type
		if i < len(sig.Params) {
			pt = sig.Params[i].Type
		} else {
			// More patterns than the signature provides — only
			// reachable on malformed input; keep a meta.
			pt = c.newMeta(p.Sp, "the parameter type")
		}
		label := ""
		if p.Label != nil {
			label = p.Label.Name
		}
		var pat Pat
		if p.Pat != nil {
			pat = c.checkPat(p.Pat, pt)
		}
		params[i] = Param{Label: label, Pat: pat}
	}
	c.bounds = append(c.bounds, boundary{ret: sig.Ret, sp: d.Sp, goDepth: c.goDepth, kind: "fn"})
	var body Expr
	if d.Body != nil {
		body = c.checkExpr(d.Body, sig.Ret)
	}
	c.bounds = c.bounds[:len(c.bounds)-1]
	c.decls = append(c.decls, &FnDecl{
		Sp:     d.Sp,
		Sym:    sym,
		Sig:    sig,
		Params: params,
		Body:   body,
	})
}

// checkLetDecl checks a top-level `let`: the value is checked first
// (so it binds the binding's type), then the pattern is checked
// against it. Bound symbols were pre-registered with metas, so
// references in other declarations already resolve.
func (c *checker) checkLetDecl(d *syntax.LetDecl) {
	var want types.Type
	if d.Type != nil {
		want = c.typeExpr(d.Type, c.newTScope())
	} else {
		want = c.newMeta(d.Sp, "the type of the let binding")
	}
	var val Expr
	if d.Value != nil {
		val = c.checkExpr(d.Value, want)
	} else {
		// A .gomi-style signature `let name : T` has no body.
		val = &Lit{Base: baseOf(d.Sp, want), Kind: syntax.LitUnit}
	}
	pat := c.checkPat(d.Pat, want)
	c.decls = append(c.decls, &LetDecl{
		Sp:    d.Sp,
		Pat:   pat,
		Value: val,
	})
}

// checkExternDecl records an extern block's entries. Each `fn`
// signature introduces a Go-bound function name with its declared
// type; there are no bodies to check.
func (c *checker) checkExternDecl(d *syntax.ExternDecl) {
	for _, e := range d.Entries {
		switch e := e.(type) {
		case *syntax.ExternFn:
			sc := c.newTScope()
			params := make([]types.FuncParam, len(e.Params))
			for i, p := range e.Params {
				fp := types.FuncParam{}
				if p.Label != nil {
					fp.Label = p.Label.Name
				}
				if p.Type != nil {
					fp.Type = c.typeExpr(p.Type, sc)
				} else {
					fp.Type = c.newMeta(p.Sp, fmt.Sprintf("the parameter of extern `%s`", e.Name.Name))
				}
				params[i] = fp
			}
			var ret types.Type
			if e.Ret != nil {
				ret = c.typeExpr(e.Ret, sc)
			} else {
				ret = types.Unit
			}
			sym := c.declSym(e.Name, resolve.KindFunc)
			sym.IsGo = true
			// Unflattened: the extern entry's parameter list is the
			// Go callee's declared arity.
			c.vals[sym] = &types.Func{Params: params, Ret: ret}
		default:
			panic("unreachable")
		}
	}
}

// checkTestDecl checks a `test` block: the body's type must be Unit
// or `Result Unit e` (spec section 11.1). The test block is a `try`
// escape boundary — `try` inside a test escapes to the test.
func (c *checker) checkTestDecl(d *syntax.TestDecl) {
	ret := c.newMeta(d.Sp, "the test body's result type")
	c.bounds = append(c.bounds, boundary{ret: ret, sp: d.Sp, goDepth: c.goDepth, kind: "test"})
	body := c.checkExpr(d.Body, nil)
	c.bounds = c.bounds[:len(c.bounds)-1]
	// The boundary ret records what a `try` inside demanded: Result
	// a e. Unify it with the body's own type (lifted into Result
	// when a try fixed it) so its metas do not stay dangling.
	if bt := types.Chase(ret); isCon(bt, "Result") {
		var lifted types.Type
		if isCon(types.Chase(body.Type()), "Result") {
			lifted = body.Type()
		} else {
			lifted = types.Result(body.Type(), c.newMeta(body.Span(), "the test's error type"))
		}
		c.unify(ret, lifted, body.Span())
	} else {
		c.unify(ret, body.Type(), body.Span())
	}
	switch t := types.Chase(body.Type()).(type) {
	case *types.Con:
		if t.Qual == "" && t.Name == "Unit" {
			break
		}
		if t.Qual == "" && t.Name == "Result" && len(t.Args) == 2 &&
			isCon(types.Chase(t.Args[0]), "Unit") {
			// A test's error channel is free: nothing else pins it,
			// so a still-open error meta defaults to GoError rather
			// than surfacing as an unsolved variable.
			if m, ok := types.Chase(t.Args[1]).(*types.Meta); ok {
				c.bind(m, types.GoError, body.Span())
			}
			break
		}
		c.testBodyError(body.Span(), body.Type())
	case *types.Meta:
		// An unconstrained unknown may still be Unit; a constrained
		// one can never be Unit-or-Result — that is E0801's shape.
		if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
			c.testBodyError(body.Span(), body.Type())
		} else {
			c.bind(t, types.Unit, body.Span())
		}
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		c.testBodyError(body.Span(), body.Type())
	default:
		panic("unreachable")
	}
	c.decls = append(c.decls, &TestDecl{
		Sp:     d.Sp,
		Name:   d.Name,
		NameSp: d.NameSp,
		Body:   body,
	})
}

// testBodyError reports E0801 for a test block of a disallowed type.
func (c *checker) testBodyError(sp diag.Span, t types.Type) {
	c.diags.Add(diag.Error("E0801",
		fmt.Sprintf("a test body must have type Unit or Result Unit e, got %s", t)).
		At(sp).Set("type", t.String()))
}

// isCon reports whether t is the nullary builtin constructor name.
func isCon(t types.Type, name string) bool {
	con, ok := t.(*types.Con)
	return ok && con.Qual == "" && con.Name == name
}
func (c *checker) registerValueDecls(decls []syntax.Decl) {
	for _, d := range decls {
		switch d := d.(type) {
		case *syntax.FnDecl:
			sym := c.declSym(d.Name, resolve.KindFunc)
			sig := c.fnSig(d)
			c.vals[sym] = sig
		case *syntax.LetDecl:
			for _, sym := range c.patSyms(d.Pat) {
				if sym != nil {
					c.vals[sym] = c.newMeta(d.Sp, fmt.Sprintf("the type of `%s`", sym.Name))
				}
			}
		case *syntax.TypeDecl, *syntax.ImportDecl, *syntax.ExternDecl,
			*syntax.TestDecl:
			// No value bindings to pre-register.
		default:
			panic("unreachable")
		}
	}
}

// fnSig resolves a function declaration's signature `p1 ... -> R`
// into its *declared* shape: Params is exactly the written parameter
// list and Ret the written return type — which may itself be a
// *types.Func when the function returns a function. Callers flatten
// the type for curried application and use len(Params) as the
// declared arity (CallInfo.Arity). Unannotated parameters get metas
// (the grammar requires annotations; tolerance keeps hand-built
// inputs checkable). A `()` parameter is a Unit parameter.
func (c *checker) fnSig(d *syntax.FnDecl) *types.Func {
	sc := c.newTScope()
	params := make([]types.FuncParam, len(d.Params))
	for i, p := range d.Params {
		fp := types.FuncParam{}
		if p.Label != nil {
			fp.Label = p.Label.Name
		}
		if p.Type != nil {
			fp.Type = c.typeExpr(p.Type, sc)
		} else if lp, ok := p.Pat.(*syntax.LitPat); ok && lp.Lit.Kind == syntax.LitUnit {
			// The `()` parameter — the function takes Unit.
			fp.Type = types.Unit
		} else {
			fp.Type = c.newMeta(p.Sp, fmt.Sprintf("the parameter of `%s`", d.Name.Name))
		}
		params[i] = fp
	}
	var ret types.Type
	if d.Ret != nil {
		ret = c.typeExpr(d.Ret, sc)
	} else {
		ret = c.newMeta(d.Sp, fmt.Sprintf("the return type of `%s`", d.Name.Name))
	}
	// Not types.Fn: Fn would flatten a function-typed Ret into the
	// parameter list, losing the declared arity — `fn mk () -> (Int
	// -> Int)` declares one parameter, and `mk ()` is CallDirect
	// rather than CallPartial.
	return &types.Func{Params: params, Ret: ret}
}

// patSyms collects the symbols a surface pattern binds (VarPat and
// AsPat names) — used to pre-register top-level let bindings.
func (c *checker) patSyms(p syntax.Pat) []*resolve.Symbol {
	var out []*resolve.Symbol
	var walk func(p syntax.Pat)
	walk = func(p syntax.Pat) {
		switch p := p.(type) {
		case *syntax.WildPat:
		case *syntax.VarPat:
			out = append(out, c.declSym(p.Name, resolve.KindVar))
		case *syntax.LitPat:
		case *syntax.CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for _, f := range p.Fields {
				if f.Punned() {
					out = append(out, c.declSym(f.Name, resolve.KindVar))
				} else {
					walk(f.Pat)
				}
			}
		case *syntax.TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *syntax.RecordPat:
			for _, f := range p.Fields {
				if f.Punned() {
					out = append(out, c.declSym(f.Name, resolve.KindVar))
				} else {
					walk(f.Pat)
				}
			}
		case *syntax.ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			if p.Rest != nil {
				walk(p.Rest)
			}
		case *syntax.OrPat:
			// Every alternative binds the same names; the first
			// gives the symbols for pre-registration.
			if len(p.Alts) > 0 {
				walk(p.Alts[0])
			}
		case *syntax.AsPat:
			walk(p.Pat)
			out = append(out, c.declSym(p.Name, resolve.KindVar))
		default:
			panic("unreachable")
		}
	}
	if p != nil {
		walk(p)
	}
	return out
}
