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
// P2-1 adds Hindley–Milner let-polymorphism on top of the P1 shape:
// declared annotations are still authoritative, but a top-level `let`
// whose right-hand side is a syntactic value (the adopted value
// restriction — decisions/2026-11-01-value-restriction.md)
// generalizes its unsolved metas into rigid type parameters, and a
// use of a generalized binding instantiates them with fresh metas.
// The generalized binding lowers as a generic Go declaration
// (decisions/2026-11-02-let-polymorphism-emission.md): a lambda lets
// become a `func` with type parameters, other values become niladic
// generic thunks. Metas that may not generalize — shared with the
// environment, constrained, or recovery artifacts — still report
// E0407.
//
// src supplies the signatures of symbols the file does not declare
// (imported members, Go functions and methods); it may be nil. The
// checker itself knows the builtin scope (the Option/Result
// constructors, assert, assert_eq, todo).
func Check(res *resolve.Resolved, src SignatureSource) (*File, diag.List) {
	bv := &builtinTVar{}
	c := &checker{
		res:        res,
		src:        src,
		vals:       map[*resolve.Symbol]types.Type{},
		referenced: map[*resolve.Symbol]bool{},
		sigScope:   map[*resolve.Symbol]*tscope{},
		thunks:     map[*resolve.Symbol]bool{},
		tyCons:     map[conKey]*typeInfo{},
		tyBySym:    map[*resolve.Symbol]*typeInfo{},
		ctors:      map[*resolve.Symbol]*variantInfo{},
		fieldSyms:  map[*resolve.Symbol]*fieldInfo{},
		metas:      map[*types.Meta]*metaRec{},
		builtins:   builtinSigs(bv),
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

// pos supplies a fallback span for diagnostics whose natural site is
// positionless — a meta created away from the source (an implicit
// test-boundary result, a recovered unknown) still gets reported
// somewhere readable rather than at offset 0. The file's own span is
// the least surprising such site.
func (c *checker) pos(sp diag.Span) diag.Span {
	if sp.IsZero() && c.res != nil && c.res.File != nil {
		return c.res.File.Sp
	}
	return sp
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

	// referenced records the value-level symbols a use site consumed
	// — every valOf call for a known symbol sets it. checkLetDecl
	// consults it to decide whether the pre-registered meta of a
	// top-level `let` pattern already carries constraints from an
	// earlier (forward) reference, in which case the binding cannot
	// generalize: the earlier use recorded a monomorphic shape.
	referenced map[*resolve.Symbol]bool

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

	// curTScope is the rigid-variable scope of the signature whose
	// body is being checked — a nested annotation (a local `let`'s
	// type, a lambda parameter, an ascription) resolves a type
	// variable by name through it, so `let y : List a` inside
	// `fn f (xs: List a)` refers to f's `a`. Nil at top level.
	curTScope *tscope
	// sigScope records each declared signature's tscope so the body
	// checker can set curTScope to it.
	sigScope map[*resolve.Symbol]*tscope

	// thunks records the symbols checkLetDecl flipped to a
	// generalized value-let — their `() -> T` signature is a thunk,
	// and valOf types a reference at the value T rather than the
	// niladic function a declared `fn f () -> T` would keep.
	thunks map[*resolve.Symbol]bool

	// errMethod is the synthetic symbol for `GoError.Error` —
	// created on first use by goErrMethodSym.
	errMethod *resolve.Symbol

	// pendingSels are member selections deferred on an unresolved
	// receiver type — `0.f` waits for the literal's numeric default,
	// `x.f` for the meta the use site pins. finalize revisits them
	// once the receiver is known (or asks for its annotation).
	pendingSels []*MemberExpr

	imports []Import
	decls   []Decl
}

// goErrMethodSym returns the symbol standing for GoError's
// `Error() string` method — the one member the builtin `error`
// interface carries. It is minted once per file and registered in
// vals like an extern signature, so signature lookup, CallGo
// classification and member codegen treat it as an ordinary Go
// method. `e.Error` is a receiver-free `() -> String`.
func (c *checker) goErrMethodSym() *resolve.Symbol {
	if c.errMethod == nil {
		sym := &resolve.Symbol{
			Name:    "Error",
			Kind:    resolve.KindMethod,
			IsGo:    true,
			Package: "builtin",
		}
		c.errMethod = sym
		c.vals[sym] = types.Fn(types.String)
	}
	return c.errMethod
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

// valOf returns the type a symbol denotes at a use site — its
// declared or external signature instantiated with fresh metas —
// together with the instantiation arguments (nil for monomorphic or
// locally bound types). Unknown symbols get a fresh meta (the
// resolver's E0201 covers the cause).
//
// Instantiation applies only to symbols whose signature quantifies
// its type variables — declared fns, constructors, builtins, methods,
// anything the SignatureSource supplies, and generalized top-level
// lets (P2-1: `let id = fn x -> x` binds `a` as a rigid variable, so
// each use gets fresh metas). A local value binding (KindVar: a
// lambda parameter, a pattern variable, a block-level let) is
// different: the `a` in its declared type is the enclosing
// declaration's rigid variable, shared by every use of the binding.
// Returning it as-is lets a constraint the body places on `x : a` —
// like `x + 1` — reach the declared `a` itself instead of evaporating
// into a fresh meta.
//
// valOf also marks the symbol referenced: a top-level let-bound name
// consumed before its own declaration is checked keeps the
// monomorphic meta the pre-registration gave it — the earlier use
// fixed a shape the binding may not generalize away from.
func (c *checker) valOf(sym *resolve.Symbol, sp diag.Span) (types.Type, []types.Type) {
	if sym == nil {
		// The resolver's E0201 already reports the name; the
		// recovery meta must not add an E0407 of its own.
		return c.quietMeta(sp, "an unresolved name"), nil
	}
	c.referenced[sym] = true
	if c.hiddenDiag(sym, sp) {
		return c.quietMeta(sp, fmt.Sprintf("the type of `%s`", sym.Name)), nil
	}
	if sig, ok := c.sigOf(sym); ok {
		if _, local := c.vals[sym]; local && sym.Kind == resolve.KindVar {
			return sig.Type, nil
		}
		typ, targs := c.instantiate(sig.Type, sp)
		if c.thunks[sym] {
			// A generalized value-let's signature is the niladic
			// thunk `() -> T` (checkLetDecl): the reference denotes
			// the *value*, so it types at the instantiated T — the
			// call is lowering's emission detail (`x[T]()`).
			if inst, ok := types.Chase(typ).(*types.Func); ok {
				return inst.Ret, targs
			}
		}
		return typ, targs
	}
	return c.newMeta(sp, fmt.Sprintf("the type of `%s`", sym.Name)), nil
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
		sig, c.sigScope[sym] = c.fnSig(d)
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
	// The signature's rigid-variable scope stays visible while the
	// body is checked so a nested annotation (`let y : List a`, a
	// lambda parameter, an ascription) resolves `a` to this
	// declaration's own variable.
	prevScope := c.curTScope
	c.curTScope = c.sigScope[sym]
	c.bounds = append(c.bounds, boundary{ret: sig.Ret, sp: d.Sp, goDepth: c.goDepth, kind: "fn"})
	var body Expr
	if d.Body != nil {
		body = c.checkExpr(d.Body, sig.Ret)
	}
	c.bounds = c.bounds[:len(c.bounds)-1]
	c.curTScope = prevScope
	c.decls = append(c.decls, &FnDecl{
		Sp:     d.Sp,
		Sym:    sym,
		Sig:    sig,
		Params: params,
		Body:   body,
		// A declared signature's rigid variables are the function's
		// own type parameters — `fn len2 (t: Tree a) -> Int` emits
		// `func len2[A](t Tree[A]) int64`.
		TypeParams: rigidVars(sig),
	})
}

// checkLetDecl checks a top-level `let`: the value is checked first
// (so it binds the binding's type), then the pattern is checked
// against it. Bound symbols were pre-registered with metas, so
// references in other declarations already resolve.
//
// Generalization (P2-1, decisions/2026-11-01-value-restriction.md):
// a `let x = v` whose right-hand side is a syntactic value
// generalizes the binding's unsolved metas — the ones not shared
// with the environment — into rigid variables the reference sites
// instantiate per use. Two emission shapes carry that through the
// pipeline (decisions/2026-11-02-let-polymorphism-emission.md):
//
//   - `let f = <lambda>` becomes a function declaration: the
//     symbol's kind flips to KindFunc and the typed decl is a
//     FnDecl with the generalized variables as TypeParams.
//   - `let x = <other syntactic value>` with generalized variables
//     becomes a "thunk": a niladic function declaration
//     `func x[T...]() T` whose references emit `x[T]()`.
//
// A binding stays monomorphic — an ordinary LetDecl — when its
// pattern binds more than a bare variable, when the symbol was
// already consumed by a forward reference (the pre-registered meta
// carries the earlier use's constraints), or when generalization
// found nothing to quantify. A `let` annotation that names type
// variables it cannot bind (the pattern or RHS blocked the flip, or
// the binding is a local let inside a body) is E0412.
func (c *checker) checkLetDecl(d *syntax.LetDecl) {
	var want types.Type
	var annScope *tscope
	if d.Type != nil {
		annScope = c.newTScope()
		want = c.typeExpr(d.Type, annScope)
	} else {
		want = c.newMeta(d.Sp, "the type of the let binding")
	}
	// The environment snapshot generalization subtracts: every
	// value-level symbol bound before this decl, minus the let's own
	// pattern symbols — a meta reachable from another binding is
	// shared and may not be quantified here.
	env := map[*resolve.Symbol]bool{}
	own := map[*resolve.Symbol]bool{}
	for _, s := range c.patSyms(d.Pat) {
		if s != nil {
			own[s] = true
		}
	}
	for s := range c.vals {
		if !own[s] {
			env[s] = true
		}
	}

	// The annotation's variables stay visible inside the right-hand
	// side — `let f : List a -> List a = fn (xs: List a) -> xs`
	// resolves the lambda's `a` to the annotation's own variable.
	prevScope := c.curTScope
	if annScope != nil {
		c.curTScope = annScope
	}
	var val Expr
	if d.Value != nil {
		val = c.checkExpr(d.Value, want)
	} else {
		// A .gomi-style signature `let name : T` has no body.
		val = &Lit{Base: baseOf(d.Sp, want), Kind: syntax.LitUnit}
	}
	c.curTScope = prevScope
	pat := c.checkPat(d.Pat, want)

	// A let-bound name that is a syntactic value — and was not
	// consumed earlier — may generalize.
	if d.Value != nil {
		if vp, ok := pat.(*VarPat); ok && vp.Sym != nil {
			if lam, ok := val.(*Lambda); ok {
				// `let f = fn ...` — a function binding. The
				// signature keeps the lambda's own shape (its
				// declared arity): nested function-typed bodies
				// stay in Ret position the way fnSig keeps them.
				sig := &types.Func{Ret: lam.Body.Type()}
				sig.Params = make([]types.FuncParam, len(lam.Params))
				for i, p := range lam.Params {
					sig.Params[i] = types.FuncParam{Label: p.Label, Type: p.Pat.Type()}
				}
				// A binding already consumed by a forward (or
				// recursive) reference may not generalize: the
				// earlier use recorded a monomorphic shape through
				// the pre-registered meta, and binding it to a
				// rigid variable now would leave the use site
				// holding the meta.
				if !c.referenced[vp.Sym] {
					// Eligible metas bind to fresh rigid
					// variables; rigidVars then reports every
					// quantified variable — generalized metas
					// plus an annotation's own declared
					// variables (`let f : a -> a = ...`) — in
					// the signature's first-occurrence order,
					// the same order instantiation uses at
					// call sites.
					c.generalize(env, sig)
				}
				vars := rigidVars(sig)
				// A referenced binding still flips when its
				// signature stays monomorphic — nothing is left
				// to quantify, so the earlier uses' shape is the
				// declaration's. It must flip: `var f = <func
				// literal>` puts the body inside Go's
				// initialization-dependency analysis, where every
				// (mutually) recursive reference is an
				// initialization cycle; a func declaration has no
				// initializer and escapes it.
				if !c.referenced[vp.Sym] || len(vars) == 0 {
					vp.Sym.Kind = resolve.KindFunc
					c.vals[vp.Sym] = sig
					if c.referenced[vp.Sym] {
						// Uses checked while the symbol was a
						// variable recorded CallUnknown; the
						// callee is statically known now.
						c.reclassifyCalls(vp.Sym, len(sig.Params), val)
					}
					c.decls = append(c.decls, &FnDecl{
						Sp: d.Sp, Sym: vp.Sym, Sig: sig,
						Params: lam.Params, Body: lam.Body,
						TypeParams: vars,
					})
					return
				}
			} else if isValue(val) && !c.referenced[vp.Sym] {
				c.generalize(env, want)
				vars := rigidVars(want)
				// The flip is only sound when the value really
				// produced want's type — a failed unify leaves the
				// annotation's variables unbound (E0412 below), and
				// quantifying them would emit a body the residual
				// check rejects.
				if len(vars) > 0 && types.Equal(want, val.Type()) {
					// `let x = <value>` — a thunk: a niladic
					// generic function whose body is the value.
					sig := &types.Func{Ret: want}
					vp.Sym.Kind = resolve.KindFunc
					c.vals[vp.Sym] = sig
					c.thunks[vp.Sym] = true
					c.decls = append(c.decls, &FnDecl{
						Sp: d.Sp, Sym: vp.Sym, Sig: sig,
						Body: val, TypeParams: vars, Thunk: true,
					})
					return
				}
			}
		}
		// A `let` annotation's variables bind only when the binding
		// generalizes; on a monomorphic let — or on a `let` that
		// could not flip (a destructure pattern, a non-value RHS,
		// an earlier reference) — they are unbound.
		if d.Type != nil {
			before := len(c.diags)
			c.unboundVars(want, d.Type.Span(), annScope)
			if len(c.diags) != before {
				// E0412 already doomed the binding — metas left
				// unsolved inside the value's type are leftovers
				// of the same failure, not separate user errors:
				// `let f : List a = [[]]` needs no third E0407.
				metasIn(val.Type(), func(m *types.Meta) { c.markQuiet(m) })
			}
		}
	}
	c.decls = append(c.decls, &LetDecl{
		Sp:    d.Sp,
		Pat:   pat,
		Value: val,
	})
}

// unboundVars reports E0412 for every rigid variable of t the
// annotation's own scope minted — a type variable with no quantifier
// to bind it. Variables resolved through an enclosing signature's
// scope (`let y : List a` inside `fn f (xs: List a)`) are bound by
// that declaration and skipped; metas are not the annotation's own.
func (c *checker) unboundVars(t types.Type, sp diag.Span, sc *tscope) {
	for _, v := range rigidVars(t) {
		if !sc.owned[v] {
			continue
		}
		c.diags.Add(diag.Error("E0412",
			fmt.Sprintf("unbound type variable `%s`", v)).
			At(sp).Set("var", v.String()))
	}
}

// generalize binds the unification variables of t that belong to this
// binding alone — occurring in t, not reachable from the environment's
// types, and free of constraint flags — to fresh rigid variables, and
// returns them in first-occurrence order. env is the set of
// value-level symbols bound before the binding's own pattern. A meta
// carrying a constraint (numeric, ordered, equality, integral,
// float, or the quiet recovery flag) is left for defaulting or E0407:
// `1` generalizes to nothing because its literal meta is Int-bound by
// default, and a quiet meta belongs to a diagnostic already reported.
func (c *checker) generalize(env map[*resolve.Symbol]bool, t types.Type) []*types.Var {
	shared := map[*types.Meta]bool{}
	for s := range env {
		metasIn(c.vals[s], func(m *types.Meta) { shared[m] = true })
	}
	used := map[string]bool{}
	for _, v := range rigidVars(t) {
		used[v.Name] = true
	}
	var vars []*types.Var
	metasIn(t, func(m *types.Meta) {
		if shared[m] {
			return
		}
		if r := c.metas[m]; r != nil &&
			(r.num || r.ord || r.eq || r.flo || r.intg || r.quiet) {
			return
		}
		v := &types.Var{ID: c.nextVar, Name: genName(used)}
		c.nextVar++
		m.Bind(v)
		vars = append(vars, v)
	})
	return vars
}

// genName picks the next unused variable name `a`, `b`, ..., `z`,
// `a1`, `b1`, ... — used records the names rigidVars already found.
func genName(used map[string]bool) string {
	for suffix := 0; ; suffix++ {
		for r := 'a'; r <= 'z'; r++ {
			name := string(r)
			if suffix > 0 {
				name = fmt.Sprintf("%s%d", name, suffix)
			}
			if !used[name] {
				used[name] = true
				return name
			}
		}
	}
}

// metasIn walks t chasing bound metas and calls fn on each unbound
// meta in first-occurrence order, deduplicated by identity.
func metasIn(t types.Type, fn func(*types.Meta)) {
	seen := map[*types.Meta]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		switch t := types.Chase(t).(type) {
		case *types.Meta:
			if !seen[t] {
				seen[t] = true
				fn(t)
			}
		case *types.Var:
		case *types.Con:
			for _, a := range t.Args {
				walk(a)
			}
		case *types.Func:
			for _, p := range t.Params {
				walk(p.Type)
			}
			walk(t.Ret)
		case *types.Tuple:
			for _, e := range t.Elems {
				walk(e)
			}
		case *types.Go:
			for _, a := range t.Args {
				walk(a)
			}
		default:
			panic("unreachable")
		}
	}
	walk(t)
}

// isValue reports whether x is a syntactic value per the value
// restriction (decisions/2026-11-01-value-restriction.md): a literal,
// a lambda, a constructor applied to values or referenced bare as a
// constant, or a tuple/list/record composed of values. Applications
// of ordinary functions, and every other expression, are not values —
// `let r = Ref.new []` stays monomorphic and reports E0407 on its
// unsolved element meta.
func isValue(x Expr) bool {
	switch x := x.(type) {
	case *Lit:
		return true
	case *Lambda:
		return true
	case *Var:
		// A bare constructor constant (None) is a value; a function
		// reference is not a *saturated* value form — but the spec's
		// "constructor applications" read includes the nullary one.
		return x.Sym != nil && x.Sym.Kind == resolve.KindCtor &&
			!isFuncT(x.Type())
	case *MemberExpr:
		return x.Sym != nil && x.Sym.Kind == resolve.KindCtor &&
			!isFuncT(x.Type())
	case *TupleLit:
		for _, e := range x.Elems {
			if !isValue(e) {
				return false
			}
		}
		return true
	case *ListLit:
		// A `..rest` element is a *Spread — not a value — so a
		// spread list is excluded by the element check itself.
		for _, e := range x.Elems {
			if !isValue(e) {
				return false
			}
		}
		return true
	case *RecordLit:
		if x.With != nil {
			return false
		}
		for _, f := range x.Fields {
			if !isValue(f.Value) {
				return false
			}
		}
		return true
	case *Apply:
		// A saturated constructor application whose arguments are
		// all values (`Some (1, 2)`, `Ok v`) is a value.
		if x.Call.Callee == nil || x.Call.Callee.Kind != resolve.KindCtor ||
			x.Call.Applied != x.Call.Arity {
			return false
		}
		for _, a := range x.Args {
			if !isValue(a.Value) {
				return false
			}
		}
		return true
	case *Hole, *Unary, *Binary, *StrInterp, *If, *Match, *Block,
		*Let, *Ignore, *Try, *GoExpr, *For, *SelectExpr, *Spread,
		*Ascribe:
		return false
	default:
		panic("unreachable")
	}
}

// isFuncT reports whether t is a function type — used to tell a
// nullary constructor constant (a value) from a curried constructor
// reference (not a *saturated* value).
func isFuncT(t types.Type) bool {
	_, ok := types.Chase(t).(*types.Func)
	return ok
}

// targsOf returns the instantiation arguments a callee-position
// expression recorded — the Var/MemberExpr TypeArgs that CallInfo
// mirrors for direct calls.
func targsOf(e Expr) []types.Type {
	switch e := e.(type) {
	case *Var:
		return e.TypeArgs
	case *MemberExpr:
		return e.TypeArgs
	case *Lit, *Apply, *Hole, *Unary, *Binary, *StrInterp, *Lambda,
		*If, *Match, *Block, *Let, *Ignore, *Try, *GoExpr, *For,
		*SelectExpr, *ListLit, *Spread, *TupleLit, *RecordLit,
		*Ascribe:
		return nil
	default:
		panic("unreachable")
	}
}

// ctorADTArgs returns the instantiated type arguments a constructor
// reference carries — the Con.Args of its result type. That is the
// order generated code instantiates the variant's ADT in:
// `result.Err` is declared `func Err[A, E](v E) Result[A, E]`, so the
// emitted `result.Err[A, E]` wants the result's [a, e], not the
// signature's occurrence order (`e -> Result a e`, which collects
// e first).
func ctorADTArgs(t types.Type) []types.Type {
	_, ret := flattenFunc(t)
	if c, ok := types.Chase(ret).(*types.Con); ok {
		return c.Args
	}
	return nil
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
		var errT types.Type
		if isCon(types.Chase(body.Type()), "Result") {
			lifted = body.Type()
		} else {
			errT = c.newMeta(body.Span(), "the test's error type")
			lifted = types.Result(body.Type(), errT)
		}
		if !c.unify(ret, lifted, body.Span()) {
			c.markQuiet(ret)
			// The lifted error meta never saw a constraint — the
			// boundary mismatch is the whole story.
			c.markQuiet(errT)
		}
	} else {
		if !c.unify(ret, body.Type(), body.Span()) {
			c.markQuiet(ret)
		}
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
			sig, sc := c.fnSig(d)
			c.vals[sym] = sig
			c.sigScope[sym] = sc
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
func (c *checker) fnSig(d *syntax.FnDecl) (*types.Func, *tscope) {
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
	return &types.Func{Params: params, Ret: ret}, sc
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
