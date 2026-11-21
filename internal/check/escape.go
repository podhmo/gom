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
	default:
		return isScopeType(t) || !c.containsScope(t)
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
	default:
		return !c.containsScope(t)
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
// param-flag input).
type escapeBody struct {
	aliases map[*resolve.Symbol]bool
	danger  map[*resolve.Symbol]bool
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
	}
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
		for i, a := range e.Args {
			// The call's own position does not leak into its
			// arguments: an argument escapes only when the callee
			// retains that parameter (or the construction stores
			// it). An enclosing escaping context reaches the arg
			// through `enclosed` instead.
			d := ctor || c.callParamEscapes(e.Call, i)
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
		// captured: a Scope value (or alias) bound outside the
		// lambda and used inside it escapes with the closure.
		// The body's own positions keep their ordinary rules —
		// the value the body produces is not stored, and a
		// Scope-returning body is caught by the type check
		// below.
		for sym, sp := range capturedScopeVars(e, b) {
			if danger {
				b.danger[sym] = true
				if report {
					c.diags.Add(diag.Error("E0419",
						fmt.Sprintf("`%s` is a `Scope` value (or closes over one) captured by a closure that escapes its scope", sym.Name)).
						At(sp))
				}
			}
		}
		c.escWalk(e.Body, false, b, report)
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
		c.escWalk(e.Body, true, b, report)
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
		c.escWalk(e.X, false, b, report)
		c.escWalk(e.Y, false, b, report)
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

// capturedScopeVars returns the tainted variables a lambda closes
// over — `Scope`-typed vars and aliases used inside its body but
// bound outside it — mapped to a use site. The bound set collects
// every pattern the body can introduce (params, lets, for/match
// bindings), so a Scope var the lambda declares itself does not
// count as a capture.
func capturedScopeVars(lam *Lambda, b *escapeBody) map[*resolve.Symbol]diag.Span {
	bound := map[*resolve.Symbol]bool{}
	for _, p := range lam.Params {
		for _, s := range patBoundSyms(p.Pat) {
			bound[s] = true
		}
	}
	var out map[*resolve.Symbol]diag.Span
	WalkExpr(lam.Body, func(y Expr) {
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
			if isScopeType(y.Type()) || b.aliases[y.Sym] {
				if out == nil {
					out = map[*resolve.Symbol]diag.Span{}
				}
				if _, seen := out[y.Sym]; !seen {
					out[y.Sym] = y.Sp
				}
			}
		}
	})
	return out
}

// exprTainted reports whether e can carry a live Scope value into a
// binding: the expression's type is Scope, or it is/contains a
// lambda that closes over a tainted variable.
func exprTainted(e Expr, b *escapeBody) bool {
	if isScopeType(e.Type()) {
		return true
	}
	tainted := false
	WalkExpr(e, func(x Expr) {
		if tainted {
			return
		}
		if lam, ok := x.(*Lambda); ok && len(capturedScopeVars(lam, b)) > 0 {
			tainted = true
		}
	})
	return tainted
}

func (c *checker) scopeEscapeErr(sp diag.Span) {
	c.diags.Add(diag.Error("E0419",
		"this construction would hold a `Scope` value — `Scope` cannot be stored or returned").
		At(sp))
}
