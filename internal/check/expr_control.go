package check

import (
	"fmt"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// armSelfContained reports whether an arm body can check without
// the join slot's contextual type: lambdas, record literals and
// nested joins consume `want` for parameter types and field/name
// disambiguation, so detaching them would drop the context they
// need — they keep sharing the settled slot instead (the `any`
// absorption then cannot reach them, which is the pre-fix
// behavior preserved for those roots).
func (c *checker) armSelfContained(e syntax.Expr) bool {
	switch e.(type) {
	case *syntax.Lambda, *syntax.RecordLit, *syntax.If, *syntax.Match, *syntax.SelectExpr:
		return false
	case *syntax.Apply, *syntax.Ascribe, *syntax.Binary, *syntax.Block,
		*syntax.For, *syntax.GoExpr, *syntax.Hole, *syntax.Ident,
		*syntax.Assign, *syntax.Ignore, *syntax.Let, *syntax.ListLit, *syntax.Lit,
		*syntax.Select, *syntax.Spread, *syntax.StrInterp, *syntax.Try,
		*syntax.TupleLit, *syntax.Unary:
		return true
	default:
		panic("unreachable")
	}
}

// armDetached reports whether the shared arm-type slot settled into
// a shape the next arm should no longer check against: a concrete
// type carrying no `Go.Any`, or a constrained meta — an `any`-typed
// arm could only fail the settled judgment, so it checks on its own
// meta and the join absorbs it (the `any` arm wins the join, the
// concrete side boxes — spec 8.1.7). An open join meta or a settled
// type carrying `Go.Any` keeps sharing: the latter seeds the arm's
// own constructor pins (`Some 1` under `Option Go.Any` builds
// `Option[any]`).
func (c *checker) armDetached(bt types.Type) bool {
	cb := types.Chase(bt)
	if containsGoAny(cb) {
		return false
	}
	if m, ok := cb.(*types.Meta); ok {
		r := c.recOf(m)
		return r.num || r.ord || r.flo || r.intg || r.eq
	}
	return true
}

// joinArm folds a detached arm's checked body into the accumulated
// branch type: a bare `Go.Any` arm absorbs the join — every prior
// arm conforms into it (boxing whole values, spec 8.1.7) — and any
// other pair conforms the arm into the settled type, the same
// judgment the shared-slot path applies. It returns the new join
// type and the arm body to record; prior arms needing the box are
// rewritten through `prev`.
func (c *checker) joinArm(bt types.Type, arm Expr, armSp diag.Span, prev []*Expr) (types.Type, Expr) {
	if isGoAny(types.Chase(arm.Type())) && !isGoAny(types.Chase(bt)) {
		for _, p := range prev {
			*p = c.conformExpr(*p, arm.Type(), (*p).Span())
		}
		return arm.Type(), arm
	}
	return bt, c.conformExpr(arm, bt, armSp)
}

// checkIf types `if c { a } else { b }`. Without else the then-branch
// must be Unit — a value-producing then without else is E0403.
func (c *checker) checkIf(e *syntax.If, want types.Type) Expr {
	cond := c.checkExpr(e.Cond, types.Bool)
	// The condition must be the builtin Bool — which a file-local
	// `type Bool` shadows away entirely.
	if c.shadowedBuiltin("Bool") {
		c.shadowedErr(e.Sp, "the `if` condition", "Bool")
	}
	if e.Else == nil {
		then := c.checkExpr(e.Then, nil)
		switch t := types.Chase(then.Type()).(type) {
		case *types.Meta:
			// An unconstrained unknown may still be Unit; a
			// constrained one (numeric, ordered, ...) can never
			// be, so this is the missing-else shape. A literal
			// meta reports its default rather than the raw `?n`,
			// and a defaultless one the renumbered placeholder —
			// the E0701 convention.
			if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
				shown := then.Type()
				if r.dflt != nil {
					shown = r.dflt
				}
				td := types.Display(shown, metaNamer())
				c.diags.Add(diag.Error("E0403",
					fmt.Sprintf("`if` without `else` requires a Unit then-branch, got %s", td)).
					At(e.Sp).Set("type", td))
			} else {
				c.bind(t, types.Unit, e.Then.Span())
			}
		case *types.Con:
			if !isCon(t, "Unit") {
				td := types.Display(then.Type(), metaNamer())
				c.diags.Add(diag.Error("E0403",
					fmt.Sprintf("`if` without `else` requires a Unit then-branch, got %s", td)).
					At(e.Sp).Set("type", td))
			}
		case *types.Var, *types.Func, *types.Tuple, *types.Go:
			td := types.Display(then.Type(), metaNamer())
			c.diags.Add(diag.Error("E0403",
				fmt.Sprintf("`if` without `else` requires a Unit then-branch, got %s", td)).
				At(e.Sp).Set("type", td))
		default:
			panic("unreachable")
		}
		return &If{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`if` without `else`")), Cond: cond, Then: then}
	}
	bt := want
	if bt == nil {
		bt = c.newMeta(e.Sp, "the if branches' type")
	}
	then := c.checkExpr(e.Then, bt)
	elsW := bt
	if want == nil && c.armSelfContained(e.Else) && c.armDetached(bt) {
		// The then arm settled the join to a concrete,
		// `Go.Any`-free type — an `any`-typed else could only
		// fail it. Detach the else and let the join absorb it:
		// arm order stops mattering under a bare `Go.Any` want.
		elsW = nil
	}
	els := c.checkExpr(e.Else, elsW)
	if elsW == nil {
		bt, els = c.joinArm(bt, els, e.Else.Span(), []*Expr{&then})
	}
	return &If{Base: baseOf(e.Sp, bt), Cond: cond, Then: then, Else: els}
}

// checkMatch types `match subj { arms }`. Arm patterns check against
// the subject type; bodies unify on one result type. Exhaustiveness
// and reachability are P1-6's, not this phase's.
func (c *checker) checkMatch(e *syntax.Match, want types.Type) Expr {
	subj := c.checkExpr(e.Subj, nil)
	bt := want
	if bt == nil {
		bt = c.newMeta(e.Sp, "the match arms' type")
	}
	arms := make([]MatchArm, len(e.Arms))
	prev := make([]*Expr, 0, len(e.Arms))
	for i, a := range e.Arms {
		// The pattern binds before the guard runs: `a == b` in a
		// guard must see the pattern variables' own types, not
		// fresh metas minted against an empty binding table.
		pat := c.checkPat(a.Pat, subj.Type())
		var guard Expr
		if a.Guard != nil {
			guard = c.checkExpr(a.Guard, types.Bool)
			if c.shadowedBuiltin("Bool") {
				c.shadowedErr(a.Guard.Span(), "a match guard", "Bool")
			}
		}
		w := bt
		detached := want == nil && c.armSelfContained(a.Body) && c.armDetached(bt)
		if detached {
			w = nil
		}
		body := c.checkExpr(a.Body, w)
		if detached {
			bt, body = c.joinArm(bt, body, a.Body.Span(), prev)
		}
		arms[i] = MatchArm{
			Sp:    a.Sp,
			Pat:   pat,
			Guard: guard,
			Body:  body,
		}
		prev = append(prev, &arms[i].Body)
	}
	c.checkTypeTestArms(e, arms, subj.Type())
	return &Match{Base: baseOf(e.Sp, bt), Subj: subj, Arms: arms}
}

// checkTypeTestArms validates the `p : T` arm shape against the
// scrutinee (decisions/2026-11-24-match-type-test-arms.md):
// type-test arms are only legal over a Go.Any-ish subject (the
// empty interface or a named Go interface, interfaces included);
// a match containing one must consist of TypePat arms plus a
// trailing catch-all (`_` or a plain variable); targets must be
// concrete and pairwise distinct by their emitted Go type
// (Char and Int32 assert the same `case`); guards are not yet
// supported on a type-test match.
func (c *checker) checkTypeTestArms(e *syntax.Match, arms []MatchArm, subjT types.Type) {
	hasTypePat := false
	for i := range arms {
		if _, ok := arms[i].Pat.(*TypePat); ok {
			hasTypePat = true
			break
		}
	}
	if !hasTypePat {
		return
	}
	anyLike := isGoAny(types.Chase(subjT))
	if !anyLike {
		// Whether the subject emits an interface decides arm
		// legality — the method-set listing is a stricter oracle
		// that also fails on unmappable signatures.
		if ms, ok := c.src.(InterfaceKindSource); ok && ms.GoInterface(types.Chase(subjT)) {
			anyLike = true
		} else if ms, ok := c.src.(InterfaceMethodSource); ok {
			if _, ok := ms.InterfaceMethods(types.Chase(subjT)); ok {
				anyLike = true
			}
		}
	}
	seen := map[string]diag.Span{}
	sawCatchAll := false
	for i := range arms {
		a := &arms[i]
		tp, isTP := a.Pat.(*TypePat)
		if !isTP {
			_, isWild := a.Pat.(*WildPat)
			_, isVar := a.Pat.(*VarPat)
			if isWild || isVar {
				if i != len(arms)-1 {
					c.diags.Add(diag.Error("E0307",
						"the catch-all arm of a type-test match must come last — later arms are unreachable").At(a.Sp))
				}
				sawCatchAll = true
			} else {
				c.diags.Add(diag.Error("E0307",
					"a type-test match arm must be `name : T`, `_ : T`, or a catch-all `_`").At(a.Sp))
			}
		} else if !anyLike {
			c.diags.Add(diag.Error("E0306",
				"a `p : T` type-test arm is only legal in a match over `Go.Any` or a named Go interface type").At(a.Sp))
		} else {
			// Metas and type parameters name no Go type — the repr
			// is "" — so a single concreteness test suffices.
			key := c.typeTestKey(tp.Target)
			if key == "" {
				c.diags.Add(diag.Error("E0307",
					"a type-test target must be a concrete type").At(a.Sp))
			} else if _, dup := seen[key]; dup {
				c.diags.Add(diag.Error("E0309",
					"duplicate type-test arm: an earlier arm already tests `"+key+"`").At(a.Sp))
			} else {
				seen[key] = a.Sp
			}
			// `case any` or an empty interface matches every value —
			// it is a catch-all and must be the last arm.
			if tp.Iface != nil && len(tp.Iface) == 0 {
				if i != len(arms)-1 {
					c.diags.Add(diag.Error("E0307",
						"a type-test arm matching every type must come last — later arms are unreachable").At(a.Sp))
				}
				sawCatchAll = true
			}
		}
		if a.Guard != nil {
			c.diags.Add(diag.Error("E0307",
				"a match guard is not supported on a type-test match").At(a.Guard.Span()))
		}
	}
	if !sawCatchAll {
		c.diags.Add(diag.Error("E0308",
			"a type-test match must end with a catch-all arm (`_` or a variable) — a value of any other Go type, including nil, falls through").At(e.Sp))
	}
}

// checkBlock types `{ stmts }`. Every statement but the last must
// have type Unit — a mid-block value is E0402 — and the block's type
// is the last statement's (Unit when empty). When the context passes
// an expected type the node takes it directly: the last statement
// was already checked against it, so reporting the block itself
// against it again would double every mismatch.
func (c *checker) checkBlock(e *syntax.Block, want types.Type) Expr {
	out := &Block{Base: baseOf(e.Sp, types.Unit), Stmts: make([]Expr, 0, len(e.Stmts))}
	for i, s := range e.Stmts {
		var st Expr
		if i == len(e.Stmts)-1 {
			st = c.checkExpr(s, want)
		} else {
			st = c.checkExpr(s, nil)
			switch t := types.Chase(st.Type()).(type) {
			case *types.Meta:
				// An unknown mid-block result is discarded: it must
				// be Unit. A constrained meta (numeric, ordered...)
				// can never be — that is the E0402 shape too. A
				// literal meta reports its default rather than the
				// raw `?n`, and a defaultless one the renumbered
				// placeholder — the E0701 convention.
				if r := c.recOf(t); r != nil && (r.num || r.flo || r.intg || r.ord) {
					shown := st.Type()
					if r.dflt != nil {
						shown = r.dflt
					}
					c.diags.Add(midBlockUnitDiag(s, types.Display(shown, metaNamer())))
				} else {
					c.bind(t, types.Unit, s.Span())
				}
			case *types.Con:
				if t.Qual == "" && t.Name == "Unit" {
					// The mid-block rule names the builtin Unit —
					// a file-local `type Unit` is an ADT, not it.
					if c.shadowedBuiltin("Unit") {
						c.shadowedErr(s.Span(), "a mid-block expression", "Unit")
					}
				} else {
					c.diags.Add(midBlockUnitDiag(s, types.Display(st.Type(), metaNamer())))
				}
			case *types.Var, *types.Func, *types.Tuple, *types.Go:
				c.diags.Add(midBlockUnitDiag(s, types.Display(st.Type(), metaNamer())))
			default:
				panic("unreachable")
			}
		}
		out.Stmts = append(out.Stmts, st)
	}
	if n := len(out.Stmts); n > 0 && want != nil {
		out.Base = baseOf(e.Sp, want)
	} else if n := len(out.Stmts); n > 0 {
		out.Base = baseOf(e.Sp, out.Stmts[n-1].Type())
	}
	// An empty block stays Unit — the context's want unifies against
	// it in checkExpr's trailing step (an `fn f () -> a = {}` is a
	// mismatch, not a silently ascribed `a`).
	return out
}

// midBlockUnitDiag builds the E0402 diagnostic of a non-Unit
// mid-block statement s; td is the statement's type already rendered
// through types.Display (the renumbered `?n` convention). It carries
// the "discard the value with `ignore`" fix: wrapping the statement
// in `ignore (...)` makes the discard the code already performs
// explicit, so the fix is ApplicabilitySafe and `gom fix` applies it
// (spec section 1.1.2, section 12 item 5). The parens are needed —
// `ignore` binds at unary level, so `ignore a + b` would parse as
// `(ignore a) + b`.
func midBlockUnitDiag(s syntax.Expr, td string) diag.Diagnostic {
	sp := s.Span()
	return diag.Error("E0402",
		fmt.Sprintf("a mid-block expression must have type Unit, got %s", td)).
		At(sp).
		Set("type", td).
		AddFix(diag.Fix{
			Title:         "discard the value with `ignore`",
			Applicability: diag.ApplicabilitySafe,
			Edits: []diag.Edit{
				{Span: diag.Point(sp.File, sp.Start), Text: "ignore ("},
				{Span: diag.Point(sp.File, sp.End), Text: ")"},
			},
		})
}

// checkLet types a block-level `let pat = e`: the binding's type is
// Unit and the bound names go into scope for the rest of the block.
// Local lets are monomorphic (spec 3.3): an annotation's type
// variables have no binder — a fresh `a` the enclosing signature
// never declared is E0412 (one declared there resolves to it).
func (c *checker) checkLet(e *syntax.Let) Expr {
	var want types.Type
	var annScope *tscope
	if e.Type != nil {
		annScope = c.newTScope()
		want = c.typeExpr(e.Type, annScope)
		c.checkScopeTypeParam(e.Type, want, "let annotation")
	} else {
		want = c.newMeta(e.Sp, "the let binding's type")
	}
	// The annotation's variables stay visible inside the value —
	// `let y : List a = xs` under `fn f (xs: List a)` resolves `a`
	// to f's variable, and a nested annotation can name it too.
	prevScope := c.curTScope
	if annScope != nil {
		c.curTScope = annScope
	}
	var val Expr
	if e.Type != nil {
		val = c.checkExprAt(e.Value, want, e.Type.Span())
	} else {
		val = c.checkExpr(e.Value, want)
	}
	c.curTScope = prevScope
	pat := c.checkPat(e.Pat, want)
	if e.Type != nil {
		c.unboundVars(want, e.Type.Span(), annScope)
	}
	return &Let{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`let`")), Pat: pat, Value: val}
}

// checkTry types `try e`: e must be `Result a err`; the expression
// is `a`, and `err` must agree with the error channel of the
// innermost enclosing function boundary — which must itself return
// `Result _ err` (E0410, E0411). A boundary on the far side of a
// `go {}` is not an escape target (E0702).
func (c *checker) checkTry(e *syntax.Try) Expr {
	x := c.checkExpr(e.X, nil)
	a, errT, ok := c.resultArgs(x.Type(), e.X.Span())
	if !ok {
		if c.isShadowedCon(x.Type(), "Result") {
			c.shadowedErr(e.X.Span(), "`try`", "Result")
		} else {
			c.mismatch(e.X.Span(), "Result a e", x.Type())
		}
		// Recovery: the tried value is the operand itself. The
		// boundary shape checks below still run (they are separate
		// rules), but the error-channel unification is skipped —
		// binding the boundary's return to `Result ?a ?e` here
		// would only mint unsolvable metas on top of the mismatch
		// already reported.
		a = x.Type()
	}
	if len(c.bounds) == 0 {
		c.diags.Add(diag.Error("E0410",
			"`try` escapes to the enclosing function, but there is no enclosing function").
			At(e.Sp))
		return &Try{Base: baseOf(e.Sp, a), X: x}
	}
	b := c.bounds[len(c.bounds)-1]
	if b.goDepth != c.goDepth {
		c.diags.Add(diag.Error("E0702",
			"`try` cannot escape across a `go` boundary; the spawned task cannot return a Result").
			At(e.Sp).Set("boundary", b.kind))
		return &Try{Base: baseOf(e.Sp, a), X: x}
	}
	if ok {
		bv, berr, bok := c.resultArgs(b.ret, e.Sp)
		if !bok {
			if c.isShadowedCon(b.ret, "Result") {
				c.shadowedErr(e.Sp,
					fmt.Sprintf("the enclosing %s's declared return type", b.kind), "Result")
			} else {
				// A lambda boundary's ret is a meta, which the
				// body's earlier inference may have bound to a
				// non-Result shape carrying its own metas (`List
				// (Result ?a ?e)`); display renumbers them.
				rd := types.Display(b.ret, metaNamer())
				c.diags.Add(diag.Error("E0410",
					fmt.Sprintf("`try` requires the enclosing %s to return Result _ e, but it returns %s", b.kind, rd)).
					At(e.Sp).Set("return", rd).Set("boundary", b.kind))
			}
			return &Try{Base: baseOf(e.Sp, a), X: x}
		}
		_ = bv
		if !c.unifyErr(berr, errT, e.X.Span()) {
			// Either channel can still hold an unsolved meta —
			// the tried error arg of an inferred `Result Int
			// (Option ?a)` is one; both share the diagnostic's
			// namer so coinciding unknowns keep one spelling.
			name := metaNamer()
			td, bd := types.Display(errT, name), types.Display(berr, name)
			c.diags.Add(diag.Error("E0411",
				fmt.Sprintf("the error type of the tried Result (%s) does not match the %s's error type (%s)", td, b.kind, bd)).
				At(e.Sp).Set("tried", td).Set("expected", bd))
		}
	}
	return &Try{Base: baseOf(e.Sp, a), X: x}
}
