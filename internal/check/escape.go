package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// The Scope escape check of spec section 6.1 (SC-2,
// decisions/2026-11-21-scoped-cleanup). Two rules keep a `Scope`
// value inside the `Scope.run` body it belongs to:
//
//   - type positions: `Scope` may appear only at a parameter's leaf
//     or inside function-argument positions — never in a result,
//     record field, variant payload, type argument, or extern
//     signature (E0419, checked at signature/annotation elaboration).
//   - use sites: a `Scope`-typed variable — and any lambda capturing
//     one — may reach only contexts that cannot retain it: an
//     argument slot of a non-escaping parameter, a `let` binding, or
//     a consumed position. Passing it to a Go call, a `go {}` body,
//     `Task.spawn`, a constructor or collection literal, or an
//     escaping parameter is E0419.
//
// Escaping of *parameters* is inferred per package by a monotone
// fixpoint: a parameter escapes when the body places it (or an
// alias) in a retaining position. The flags are exported into
// `.gomi` as `//gom:noescape` directive lines so importers keep the
// same judgment; an absent directive means "assume escaping". Go
// callees always escape. A Gom callee's `Scope`-typed parameter is
// non-escaping by construction — its own compile already enforced
// the rule. See docs/decisions/2026-11-22-scope-escape-check.md.

// isScopeType reports whether t is exactly the builtin `Scope`.
func isScopeType(t types.Type) bool {
	con, ok := types.Chase(t).(*types.Con)
	return ok && con.Qual == "" && con.Name == "Scope" && len(con.Args) == 0
}

// containsScope reports whether `Scope` occurs anywhere inside t.
func (c *checker) containsScope(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		return false
	case *types.Func:
		for _, p := range t.Params {
			if c.containsScope(p.Type) {
				return true
			}
		}
		return c.containsScope(t.Ret)
	case *types.Tuple:
		for _, e := range t.Elems {
			if c.containsScope(e) {
				return true
			}
		}
		return false
	case *types.Con:
		if t.Qual == "" && t.Name == "Scope" {
			return true
		}
		for _, a := range t.Args {
			if c.containsScope(a) {
				return true
			}
		}
		return false
	case *types.Go:
		for _, a := range t.Args {
			if c.containsScope(a) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// scopeParamOK — `Scope` allowed at the leaf and at argument
// positions of nested function types: `Scope`, `Scope -> a`,
// `(Scope -> a) -> b`. Result positions are scopeResultOK.
func (c *checker) scopeParamOK(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		return true
	case *types.Func:
		for _, p := range t.Params {
			if !c.scopeParamOK(p.Type) {
				return false
			}
		}
		return c.scopeResultOK(t.Ret)
	case *types.Con, *types.Go, *types.Tuple:
		return isScopeType(t) || !c.containsScope(t)
	default:
		panic("unreachable")
	}
}

// scopeResultOK — `Scope` may not be produced: the leaf is banned,
// nested function types keep the param/result rules.
func (c *checker) scopeResultOK(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta, *types.Var:
		return true
	case *types.Func:
		for _, p := range t.Params {
			if !c.scopeParamOK(p.Type) {
				return false
			}
		}
		return c.scopeResultOK(t.Ret)
	case *types.Con, *types.Go, *types.Tuple:
		return !c.containsScope(t)
	default:
		panic("unreachable")
	}
}

func (c *checker) scopeTypeErr(sp diag.Span, what string) {
	c.diags.Add(diag.Error("E0419",
		fmt.Sprintf("a %s cannot mention `Scope` here — it is allowed only as a parameter's leaf type or inside function-argument positions", what)).
		At(sp))
}

// checkScopeTypeParam validates a parameter or annotation type: the
// leaf may be `Scope` and nested function types may carry `Scope` in
// argument positions.
func (c *checker) checkScopeTypeParam(te syntax.TypeExpr, t types.Type, what string) {
	if te == nil || t == nil {
		return
	}
	if !c.scopeParamOK(t) {
		c.scopeTypeErr(te.Span(), what)
	}
}

// checkScopeTypeResult validates a result type: `Scope` may not be
// produced anywhere in it.
func (c *checker) checkScopeTypeResult(te syntax.TypeExpr, t types.Type, what string) {
	if te == nil || t == nil {
		return
	}
	if !c.scopeResultOK(t) {
		c.scopeTypeErr(te.Span(), what)
	}
}

// checkScopeTypeFree validates a type where `Scope` may not appear
// at all: record fields, variant payloads, extern signatures.
func (c *checker) checkScopeTypeFree(te syntax.TypeExpr, t types.Type, what string) {
	if te == nil || t == nil {
		return
	}
	if c.containsScope(t) {
		c.scopeTypeErr(te.Span(), what)
	}
}

// --- use-site escape analysis ---

// escapeBody is the per-body working state of the escape walk:
// aliases marks let-bound vars whose value mentions a tainted
// variable; danger records symbols seen in escaping positions (the
// param-flag input); forward marks lambda arguments whose produced
// value the callee returns as the call's own result — the scope
// runner calls are the transparent case, so an escaping call
// position reaches the body lambda's tail through them.
type escapeBody struct {
	aliases map[*resolve.Symbol]bool
	danger  map[*resolve.Symbol]bool
	forward map[Expr]bool
}

func (b *escapeBody) tainted(s *resolve.Symbol) bool { return b.aliases[s] }

// checkScopeEscape runs the use-site pass: first a monotone
// fixpoint infers each top-level parameter's escape flag, then a
// second pass reports E0419 at tainted variables in escaping
// positions. Flags land on sym.Noescape for .gomi emission.
func (c *checker) checkScopeEscape() {
	// Pass 1 — fixpoint over param escape flags.
	for changed := true; changed; {
		changed = false
		for _, d := range c.decls {
			body := c.declBody(d)
			if body == nil {
				// .gomi declarations have no body — their
				// flags come from `//gom:noescape`
				// directives, and absent those they must
				// keep the nil (assume-escaping) default.
				continue
			}
			if fn, ok := d.(*FnDecl); ok && fn.Sym != nil &&
				len(fn.Sym.Escapes) == 0 && len(fn.Params) > 0 {
				fn.Sym.Escapes = make([]bool, len(fn.Params))
			}
			b := c.newEscapeBody(d)
			c.escWalk(body, true, b, false)
			if fn, ok := d.(*FnDecl); ok && fn.Sym != nil {
				for sym := range b.danger {
					if i, ok := c.declParamIndex(d, sym); ok && i < len(fn.Sym.Escapes) {
						if !fn.Sym.Escapes[i] {
							fn.Sym.Escapes[i] = true
							changed = true
						}
					}
				}
			}
		}
	}
	// Pass 2 — report tainted uses in escaping positions.
	for _, d := range c.decls {
		c.escWalk(c.declBody(d), true, c.newEscapeBody(d), true)
	}
}

// declParamIndex maps a top-level declaration's parameter symbol to
// its flattened index inside sym.Escapes.
func (c *checker) declParamIndex(d Decl, sym *resolve.Symbol) (int, bool) {
	switch d := d.(type) {
	case *FnDecl:
		for i, p := range d.Params {
			for _, s := range patBoundSyms(p.Pat) {
				if s == sym {
					return i, true
				}
			}
		}
	case *LetDecl, *TestDecl, *TypeDecl:
	}
	return 0, false
}

func (c *checker) declBody(d Decl) Expr {
	switch d := d.(type) {
	case *FnDecl:
		return d.Body
	case *LetDecl:
		return d.Value
	case *TestDecl:
		return d.Body
	case *TypeDecl:
		return nil
	default:
		panic("unreachable")
	}
}

func (c *checker) newEscapeBody(d Decl) *escapeBody {
	return &escapeBody{
		aliases: map[*resolve.Symbol]bool{},
		danger:  map[*resolve.Symbol]bool{},
		forward: map[Expr]bool{},
	}
}

// isFuncType reports whether the expression's type is a function —
// an under-applied unknown callee produces one that retains the
// arguments already supplied.
func isFuncType(t types.Type) bool {
	_, ok := types.Chase(t).(*types.Func)
	return ok
}

// isScopeRunner reports whether the call is one of the two scope
// runners — `Scope.run` / `Scope.run_result` — which return their
// body argument's produced value as the call's own result.
func isScopeRunner(call CallInfo) bool {
	s := call.Callee
	return s != nil && s.IsGo && (s.Name == "ScopeRun" || s.Name == "ScopeRunResult")
}

// patBoundSyms collects the symbols a typed pattern binds.
func patBoundSyms(p Pat) []*resolve.Symbol {
	var out []*resolve.Symbol
	var walk func(p Pat)
	walk = func(p Pat) {
		if p == nil {
			return
		}
		switch p := p.(type) {
		case *VarPat:
			if p.Sym != nil {
				out = append(out, p.Sym)
			}
		case *AsPat:
			if p.Sym != nil {
				out = append(out, p.Sym)
			}
			walk(p.Pat)
		case *CtorPat:
			for _, a := range p.Args {
				walk(a.Pat)
			}
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *TuplePat:
			for _, e := range p.Elems {
				walk(e)
			}
		case *RecordPat:
			for _, f := range p.Fields {
				walk(f.Pat)
			}
		case *ListPat:
			for _, e := range p.Elems {
				walk(e)
			}
			walk(p.Rest)
		case *OrPat:
			for _, a := range p.Alts {
				walk(a)
			}
		case *WildPat, *LitPat:
		default:
			panic("unreachable")
		}
	}
	walk(p)
	return out
}

// callParamEscapes reports whether the i-th argument slot of a call
// may retain its argument. Go callees, unknown callees, and variant
// constructors always escape; a Gom callee's Scope-typed parameter
// is non-escaping by construction; other positions consult the
// inferred/annotated flag.
func (c *checker) callParamEscapes(call CallInfo, i int) bool {
	p := i
	if call.ArgToParam != nil && i < len(call.ArgToParam) && call.ArgToParam[i] >= 0 {
		p = call.ArgToParam[i]
	}
	if call.Kind == CallUnknown {
		return true
	}
	sym := call.Callee
	if sym == nil || sym.Kind == resolve.KindCtor {
		return true
	}
	// The callee's declared parameter type tells the rule for free:
	// a `Scope`-typed parameter of a Gom callee is non-escaping —
	// the callee's own compile enforced it. Consult the signature
	// source quietly (the use site already ran sigOf's diagnostics).
	var sig *types.Func
	if t, ok := c.vals[sym]; ok {
		sig, _ = types.Chase(t).(*types.Func)
	} else if c.src != nil {
		if sg, ok := c.src.SignatureOf(sym); ok {
			sig, _ = types.Chase(sg.Type).(*types.Func)
		}
	}
	if sig != nil {
		if p >= len(sig.Params) {
			return true
		}
		if isScopeType(sig.Params[p].Type) {
			return false
		}
	}
	if p < len(sym.Escapes) {
		return sym.Escapes[p]
	}
	return true
}

// escWalk walks e: `danger` is true when a tainted variable used in
// this subtree escapes. Transparent wrappers (blocks, if/match
// branches, try) propagate it; call argument slots re-derive it from
// parameter flags; bindings and consumed positions reset it.
func (c *checker) escWalk(e Expr, danger bool, b *escapeBody, report bool) {
	if e == nil {
		return
	}
	switch e := e.(type) {
	case *Lit, *Hole:
	case *Var:
		if !danger {
			return
		}
		if b.tainted(e.Sym) || isScopeType(e.Type()) {
			if report {
				c.diags.Add(diag.Error("E0419",
					fmt.Sprintf("`%s` is a `Scope` value (or closes over one) and escapes its scope here", e.Sym.Name)).
					At(e.Sp))
			}
		}
		b.danger[e.Sym] = true
	case *Apply:
		if m, ok := e.Fn.(*MemberExpr); ok && m.Sym != nil && m.Sym.Kind == resolve.KindMethod {
			// A method call's receiver is its implicit first
			// argument — Go methods retain it (CallGo), Gom
			// methods follow the flat-parameter flags.
			c.escWalk(m.Recv, danger || e.Call.Kind == CallGo, b, report)
		} else {
			c.escWalk(e.Fn, false, b, report)
		}
		ctor := e.Call.Callee != nil && e.Call.Callee.Kind == resolve.KindCtor
		// A partial or hole application produces a closure that
		// retains every supplied argument; a CallUnknown whose
		// result is still a function is the same shape — a curried
		// step that binds arguments for a later call. Supplied
		// arguments escape when the call's own result does.
		producesClosure := e.Call.Kind == CallPartial || e.Call.HoleIndex >= 0 ||
			(e.Call.Kind == CallUnknown && isFuncType(e.Type()))
		for i, a := range e.Args {
			// The call's own position does not leak into its
			// arguments: an argument escapes only when the callee
			// retains that parameter (or the construction stores
			// it) — or when the call produces a closure that
			// captures it.
			d := ctor || c.callParamEscapes(e.Call, i) || (producesClosure && danger)
			if lam, ok := a.Value.(*Lambda); ok && danger && isScopeRunner(e.Call) {
				b.forward[lam] = true
			}
			c.escWalk(a.Value, d, b, report)
		}
		// A constructor application stores its arguments: a
		// result type containing Scope means a slot held it.
		if ctor && c.containsScope(e.Type()) {
			if report {
				c.scopeEscapeErr(e.Sp)
			}
		}
	case *MemberExpr:
		c.escWalk(e.Recv, danger, b, report)
	case *Lambda:
		// An escaping closure retains every variable it
		// captured — whatever the type, so parameter inference
		// marks all of them; only Scope-tainted ones report.
		// The body's tail keeps the escaping context (plus the
		// scope-runner forwarding mark): a closure the body
		// produces escapes with it.
		if danger {
			for sym, v := range capturedVars(e.Body, lamBound(e)) {
				b.danger[sym] = true
				if report && (isScopeType(v.Type()) || b.aliases[sym]) {
					c.diags.Add(diag.Error("E0419",
						fmt.Sprintf("`%s` is a `Scope` value (or closes over one) captured by a closure that escapes its scope", sym.Name)).
						At(v.Sp))
				}
			}
		}
		c.escWalk(e.Body, danger || b.forward[e], b, report)
		if c.containsScope(e.Type()) && !c.scopeParamOK(e.Type()) {
			// A lambda whose type mentions Scope outside
			// argument positions — e.g. `fn () -> s` — produces
			// the value.
			if report {
				c.scopeEscapeErr(e.Sp)
			}
		}
	case *If:
		c.escWalk(e.Cond, false, b, report)
		c.escWalk(e.Then, danger, b, report)
		c.escWalk(e.Else, danger, b, report)
	case *Match:
		c.escWalk(e.Subj, false, b, report)
		for _, a := range e.Arms {
			c.escWalk(a.Guard, danger, b, report)
			c.escWalk(a.Body, danger, b, report)
		}
	case *Block:
		for i, s := range e.Stmts {
			tail := i == len(e.Stmts)-1
			c.escWalk(s, danger && tail, b, report)
			if l, ok := s.(*Let); ok {
				b.bindLet(l)
			}
		}
	case *Let:
		c.escWalk(e.Value, false, b, report)
		b.bindLet(e)
	case *Ignore:
		c.escWalk(e.X, false, b, report)
	case *Try:
		c.escWalk(e.X, danger, b, report)
	case *FieldAssign:
		c.escWalk(e.Recv, danger, b, report)
		c.escWalk(e.Value, true, b, report)
	case *GoExpr:
		// A `go` block retains its captures for the goroutine's
		// lifetime — an outliving goroutine is the escaping
		// closure case with no produced value.
		for sym, v := range capturedVars(e.Body, map[*resolve.Symbol]bool{}) {
			b.danger[sym] = true
			if report && (isScopeType(v.Type()) || b.aliases[sym]) {
				c.diags.Add(diag.Error("E0419",
					fmt.Sprintf("`%s` is a `Scope` value (or closes over one) captured by a `go` block that escapes its scope", sym.Name)).
					At(v.Sp))
			}
		}
		c.escWalk(e.Body, false, b, report)
	case *For:
		c.escWalk(e.Iter, false, b, report)
		c.escWalk(e.Body, danger, b, report)
	case *SelectExpr:
		for _, a := range e.Arms {
			switch a := a.(type) {
			case *RecvArm:
				c.escWalk(a.Chan, false, b, report)
				c.escWalk(a.Body, danger, b, report)
			case *SendArm:
				c.escWalk(a.Chan, false, b, report)
				c.escWalk(a.Value, true, b, report)
				c.escWalk(a.Body, danger, b, report)
			case *DefaultArm:
				c.escWalk(a.Body, danger, b, report)
			default:
				panic("unreachable")
			}
		}
	case *ListLit:
		for _, el := range e.Elems {
			c.escWalk(el, true, b, report)
		}
		if c.containsScope(e.Type()) && report {
			c.scopeEscapeErr(e.Sp)
		}
	case *TupleLit:
		for _, el := range e.Elems {
			c.escWalk(el, true, b, report)
		}
		if c.containsScope(e.Type()) && report {
			c.scopeEscapeErr(e.Sp)
		}
	case *Spread:
		c.escWalk(e.X, danger, b, report)
	case *RecordLit:
		c.escWalk(e.With, danger, b, report)
		for _, f := range e.Fields {
			c.escWalk(f.Value, true, b, report)
		}
		if c.containsScope(e.Type()) && report {
			c.scopeEscapeErr(e.Sp)
		}
	case *Ascribe:
		c.escWalk(e.X, danger, b, report)
	case *Unary:
		c.escWalk(e.X, false, b, report)
	case *Binary:
		switch e.Op {
		case syntax.BinPipeFwd, syntax.BinPipeBack, syntax.BinPipeTry:
			// `x |> f` / `f <| x` / `x |?> f` — the pipe applies
			// fn to arg; check keeps the operands as a Binary.
			// A partial-application callee retains the piped
			// argument too, so it walks under the pipe's own
			// position.
			arg, fn := e.X, e.Y
			if e.Op == syntax.BinPipeBack {
				arg, fn = e.Y, e.X
			}
			_, fnIsApply := fn.(*Apply)
			c.escWalk(fn, fnIsApply && danger, b, report)
			c.escWalk(arg, c.pipeParamEscapes(fn) || (fnIsApply && danger), b, report)
		case syntax.BinComposeFwd, syntax.BinComposeBack:
			// `f >> g` / `f << g` produce a closure retaining
			// both operands — they escape with the composition.
			c.escWalk(e.X, danger, b, report)
			c.escWalk(e.Y, danger, b, report)
		default:
			c.escWalk(e.X, false, b, report)
			c.escWalk(e.Y, false, b, report)
		}
	case *StrInterp:
		for _, p := range e.Parts {
			switch p := p.(type) {
			case *StrExpr:
				c.escWalk(p.X, false, b, report)
			case *StrText:
			default:
				panic("unreachable")
			}
		}
	default:
		panic("unreachable")
	}
}

// bindLet registers a `let`'s bound symbols as tainted when the
// bound value can carry a live Scope — a lambda closing over one,
// or an alias of a tainted variable.
func (b *escapeBody) bindLet(l *Let) {
	if exprTainted(l.Value, b) {
		for _, s := range patBoundSyms(l.Pat) {
			b.aliases[s] = true
		}
	}
}

// lamBound collects the symbols a lambda's own parameter patterns
// bind — the starting bound set for capturedVars.
func lamBound(lam *Lambda) map[*resolve.Symbol]bool {
	bound := map[*resolve.Symbol]bool{}
	for _, p := range lam.Params {
		for _, s := range patBoundSyms(p.Pat) {
			bound[s] = true
		}
	}
	return bound
}

// capturedVars returns every variable an expression uses but binds
// outside itself — its captures — mapped to a use site. bound is
// the caller-seeded bound set (a lambda's params, empty for a `go`
// block); the walk adds every pattern the body can introduce
// (nested lambdas, lets, for/match bindings), so a var the
// expression declares itself does not count as a capture.
func capturedVars(body Expr, bound map[*resolve.Symbol]bool) map[*resolve.Symbol]*Var {
	var out map[*resolve.Symbol]*Var
	WalkExpr(body, func(y Expr) {
		switch y := y.(type) {
		case *Lambda:
			for _, p := range y.Params {
				for _, s := range patBoundSyms(p.Pat) {
					bound[s] = true
				}
			}
		case *Let:
			for _, s := range patBoundSyms(y.Pat) {
				bound[s] = true
			}
		case *For:
			for _, s := range patBoundSyms(y.Pat) {
				bound[s] = true
			}
		case *Match:
			for _, a := range y.Arms {
				for _, s := range patBoundSyms(a.Pat) {
					bound[s] = true
				}
			}
		case *Var:
			if bound[y.Sym] {
				return
			}
			if out == nil {
				out = map[*resolve.Symbol]*Var{}
			}
			if _, seen := out[y.Sym]; !seen {
				out[y.Sym] = y
			}
		case *Apply, *Ascribe, *Binary, *Block, *FieldAssign, *GoExpr,
			*Hole, *If, *Ignore, *ListLit, *Lit, *MemberExpr, *RecordLit,
			*SelectExpr, *Spread, *StrInterp, *Try, *TupleLit, *Unary:
		default:
			panic("unreachable")
		}
	})
	return out
}

// pipeParamEscapes reports whether the operand a pipe feeds into a
// callee may be retained — `x |> f`'s f gets x as an argument. The
// pipe node records no CallInfo, so the callee's first parameter's
// rule applies directly; a partial-application callee's next open
// slot is ambiguous under labeled arguments, and a callee that is
// not a named symbol gets the conservative answer.
func (c *checker) pipeParamEscapes(fn Expr) bool {
	switch f := fn.(type) {
	case *Var:
		return c.callParamEscapes(CallInfo{Callee: f.Sym}, 0)
	case *MemberExpr:
		return c.callParamEscapes(CallInfo{Callee: f.Sym}, 0)
	case *Lambda:
		// An immediately-applied literal cannot retain — its
		// parameter bindings die with the call.
		return false
	case *Apply, *Ascribe, *Binary, *Block, *FieldAssign, *For,
		*GoExpr, *Hole, *If, *Ignore, *Let, *ListLit, *Lit, *Match,
		*RecordLit, *SelectExpr, *Spread, *StrInterp, *Try, *TupleLit,
		*Unary:
		// An *Apply callee is a partial application appending the
		// piped value to an already-retaining closure; anything
		// else cannot name a callee's parameter — the conservative
		// answer.
		return true
	default:
		panic("unreachable")
	}
}

// exprTainted reports whether the VALUE e produces can carry a live
// Scope into a binding: the expression's type is Scope, the value is
// or contains a lambda that closes over a tainted variable, or an
// under-application's produced closure retains a tainted argument.
// The walk follows only positions that propagate the produced value —
// a lambda consumed as a saturated call's argument is not part of the
// call's result (the callee's escape rule decides its fate at the
// call site), so `let y = helper p` binding the Int `y` is clean.
func exprTainted(e Expr, b *escapeBody) bool {
	return exprTaintedL(e, b, nil)
}

// exprTaintedL is exprTainted with locals — a map of let-bound
// symbols visible inside e to their bound expressions, so a bound
// variable's taint resolves through its own binding when the
// expression under inspection is a scope-runner body's tail (the
// runner forwards exactly that value).
func exprTaintedL(e Expr, b *escapeBody, locals map[*resolve.Symbol]Expr) bool {
	if e == nil {
		// An absent else arm produces Unit — never tainted.
		return false
	}
	if isScopeType(e.Type()) {
		return true
	}
	switch x := e.(type) {
	case *Var:
		if v, ok := locals[x.Sym]; ok {
			return exprTaintedL(v, b, locals)
		}
		return b.aliases[x.Sym]
	case *Lambda:
		for sym, v := range capturedVars(x.Body, lamBound(x)) {
			if isScopeType(v.Type()) || b.aliases[sym] {
				return true
			}
		}
		return false
	case *Block:
		if len(x.Stmts) == 0 {
			return false
		}
		inner := make(map[*resolve.Symbol]Expr, len(locals)+len(x.Stmts))
		for s, v := range locals {
			inner[s] = v
		}
		for _, st := range x.Stmts[:len(x.Stmts)-1] {
			if l, ok := st.(*Let); ok {
				for _, s := range patBoundSyms(l.Pat) {
					inner[s] = l.Value
				}
			}
		}
		return exprTaintedL(x.Stmts[len(x.Stmts)-1], b, inner)
	case *If:
		return exprTaintedL(x.Then, b, locals) || exprTaintedL(x.Else, b, locals)
	case *Match:
		for _, a := range x.Arms {
			if exprTaintedL(a.Body, b, locals) {
				return true
			}
		}
		return false
	case *Try:
		return exprTaintedL(x.X, b, locals)
	case *Ascribe:
		return exprTaintedL(x.X, b, locals)
	case *SelectExpr:
		for _, a := range x.Arms {
			switch a := a.(type) {
			case *RecvArm:
				if exprTaintedL(a.Body, b, locals) {
					return true
				}
			case *SendArm:
				if exprTaintedL(a.Body, b, locals) {
					return true
				}
			case *DefaultArm:
				if exprTaintedL(a.Body, b, locals) {
					return true
				}
			default:
				panic("unreachable")
			}
		}
		return false
	case *RecordLit:
		if x.With != nil && exprTaintedL(x.With, b, locals) {
			return true
		}
		for _, f := range x.Fields {
			if exprTaintedL(f.Value, b, locals) {
				return true
			}
		}
		return false
	case *TupleLit:
		for _, el := range x.Elems {
			if exprTaintedL(el, b, locals) {
				return true
			}
		}
		return false
	case *ListLit:
		for _, el := range x.Elems {
			if exprTaintedL(el, b, locals) {
				return true
			}
		}
		return false
	case *Spread:
		return exprTaintedL(x.X, b, locals)
	case *MemberExpr:
		// The field's own value propagates — tainted only when the
		// record it was read from holds a tainted closure.
		return exprTaintedL(x.Recv, b, locals)
	case *Apply:
		if isScopeRunner(x.Call) {
			// The runner forwards its body argument's produced
			// value: a literal body's tail, or — for a named body
			// — the same conservative read as any tainted callee.
			for _, a := range x.Args {
				switch f := a.Value.(type) {
				case *Lambda:
					if exprTaintedL(f.Body, b, nil) {
						return true
					}
				case *Var:
					if b.aliases[f.Sym] {
						return true
					}
				}
			}
			return false
		}
		// Only a produced closure retains the callee's captures
		// and the arguments already supplied; a saturated call's
		// arguments are consumed under the callee's own escape
		// rule.
		if x.Call.Kind == CallPartial || x.Call.HoleIndex >= 0 ||
			(x.Call.Kind == CallUnknown && isFuncType(x.Type())) {
			if exprTaintedL(x.Fn, b, locals) {
				return true
			}
			for _, a := range x.Args {
				if exprTaintedL(a.Value, b, locals) {
					return true
				}
			}
		}
		return false
	case *Binary:
		switch x.Op {
		case syntax.BinComposeFwd, syntax.BinComposeBack:
			// The composed closure retains both operands.
			return exprTaintedL(x.X, b, locals) || exprTaintedL(x.Y, b, locals)
		case syntax.BinPipeFwd, syntax.BinPipeBack, syntax.BinPipeTry:
			// The pipe's produced value is the callee's result: a
			// literal callee's body tail is inspectable, anything
			// else is the callee's own value — not a capture the
			// pipe manufactures.
			fn := x.Y
			if x.Op == syntax.BinPipeBack {
				fn = x.X
			}
			if lam, ok := fn.(*Lambda); ok {
				return exprTaintedL(lam.Body, b, nil)
			}
			return false
		default:
			// Arithmetic and comparisons consume their operands.
			return false
		}
	case *GoExpr, *Let, *Ignore, *For, *FieldAssign,
		*Lit, *StrInterp, *Hole, *Unary:
		// Unit producers, literals, and consumers that cannot carry
		// a closure in their produced value.
		return false
	default:
		panic("unreachable")
	}
}

func (c *checker) scopeEscapeErr(sp diag.Span) {
	c.diags.Add(diag.Error("E0419",
		"this construction would hold a `Scope` value — `Scope` cannot be stored or returned").
		At(sp))
}
