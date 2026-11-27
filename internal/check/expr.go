package check

import (
	"fmt"
	"slices"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// checkExpr checks a surface expression and returns the typed node.
// want is the type the context expects — a concrete type or a meta —
// or nil when nothing is expected. The result unifies with want
// before returning.
func (c *checker) checkExpr(e syntax.Expr, want types.Type) Expr {
	return c.checkExprAt(e, want, diag.Span{})
}

// checkExprAt is checkExpr plus the source position that introduced the
// expected type. A mismatch against want labels that position as the
// expectation's origin.
func (c *checker) checkExprAt(e syntax.Expr, want types.Type, expectedFrom diag.Span) Expr {
	if e == nil {
		// A missing expression (a .gomi signature body, a recovery
		// hole in the surface tree) is a meta-typed unit. Quiet:
		// the hole's own diagnostic already ran.
		m := c.quietMeta(diag.Span{}, "a missing expression")
		if want != nil {
			c.unify(want, m, diag.Span{})
		}
		return &Lit{Base: baseOf(diag.Span{}, m), Kind: syntax.LitUnit, Text: "()"}
	}
	var out Expr
	// The branching checkers (if/match/select) stamp `want` on the
	// node itself and thread it into every arm's body — under a
	// Go `any` expectation (a `...any` Go-call argument) that would
	// pin Go.Any on the node while the branches keep their natural
	// types, which lower's node-type invariants then reject (E0901).
	// `any` bounds without seeding: infer the branch type, and the
	// trailing unify below conforms it — an `any` slot accepts
	// every type (spec 8.1.7).
	bw := want
	if w := types.Chase(bw); isGoAny(w) {
		bw = nil
	}
	branchExpectedFrom := expectedFrom
	if bw == nil {
		// A bare Go.Any expectation bounds without seeding the branch
		// type, so its annotation is not the arms' expected type.
		branchExpectedFrom = diag.Span{}
	}
	switch e := e.(type) {
	case *syntax.Ident:
		out = c.checkIdent(e)
	case *syntax.Lit:
		lit := &Lit{Base: baseOf(e.Sp, c.litType(e)), Kind: e.Kind, Text: e.Text}
		c.watchLit(lit)
		out = lit
	case *syntax.StrInterp:
		out = c.checkStrInterp(e)
	case *syntax.Apply:
		out = c.checkApply(e, bw)
	case *syntax.Select:
		out = c.checkSelect(e)
	case *syntax.Assign:
		out = c.checkAssign(e)
	case *syntax.Unary:
		out = c.checkUnary(e)
	case *syntax.Binary:
		out = c.checkBinary(e, want)
	case *syntax.Hole:
		// `_` is only legal as a Go-call argument; the apply checker
		// never calls checkExpr on one, so reaching here is misplaced.
		c.diags.Add(diag.Error("E0504",
			"`_` is only allowed as an argument of a Go call").At(e.Sp))
		out = &Hole{Base: baseOf(e.Sp, c.quietMeta(e.Sp, "the placeholder's type"))}
	case *syntax.Lambda:
		out = c.checkLambda(e, bw)
	case *syntax.If:
		out = c.checkIf(e, bw, branchExpectedFrom)
	case *syntax.Match:
		out = c.checkMatch(e, bw, branchExpectedFrom)
	case *syntax.Block:
		out = c.checkBlock(e, bw, branchExpectedFrom)
	case *syntax.Let:
		out = c.checkLet(e)
	case *syntax.Ignore:
		x := c.checkExpr(e.X, nil)
		out = &Ignore{Base: baseOf(e.Sp, types.Unit), X: x}
	case *syntax.Try:
		out = c.checkTry(e)
	case *syntax.GoExpr:
		out = c.checkGoExpr(e)
	case *syntax.For:
		out = c.checkFor(e)
	case *syntax.SelectExpr:
		out = c.checkSelectExpr(e, bw)
	case *syntax.ListLit:
		out = c.checkListLit(e, bw)
	case *syntax.Spread:
		// A `..e` outside a list literal or a variadic Go argument.
		x := c.checkExpr(e.X, nil)
		c.diags.Add(diag.Error("E0505",
			"`..e` is only allowed as the last element of a list literal or the last argument of a variadic Go call").
			At(e.Sp))
		out = &Spread{Base: baseOf(e.Sp, x.Type()), X: x}
	case *syntax.TupleLit:
		out = c.checkTupleLit(e, bw)
	case *syntax.RecordLit:
		out = c.checkRecordLit(e, bw)
	case *syntax.Ascribe:
		out = c.checkAscribe(e)
	default:
		panic("unreachable")
	}
	return c.conformExprAt(out, want, e.Span(), expectedFrom)
}

// checkAssign types `ptr.Field = v` — a field write through a
// `Ptr T` handle where T is a named Go struct (spec 8.3,
// decisions/2026-11-19-struct-field-write.md). Only that shape is
// legal: record fields, Ref values, and Go values held directly
// (not through a pointer) are rejected — a write to a value copy
// would silently drop the effect.
func (c *checker) checkAssign(e *syntax.Assign) Expr {
	unit := func() Expr {
		return &Lit{Base: baseOf(e.Sp, types.Unit), Kind: syntax.LitUnit, Text: "()"}
	}
	sel, ok := e.Target.(*syntax.Select)
	if !ok || sel.Sel == nil {
		c.diags.Add(diag.Error("E0511",
			"the assignment target must be a field selection `ptr.Field`").
			At(e.Target.Span()))
		c.checkExpr(e.Target, nil)
		c.checkExpr(e.Value, nil)
		// The write is dead — metas the discarded RHS check minted
		// (an untyped `[]`, a bare lambda param) are debris that would
		// only pile an E0407 onto the E0511.
		c.quietMetasIn(e.Value.Span())
		return unit()
	}
	t := c.checkExpr(sel, nil)
	me, ok := t.(*MemberExpr)
	if !ok {
		c.diags.Add(diag.Error("E0511",
			"the assignment target must be an exported field of a `Ptr T` where T is a named Go struct").
			At(sel.Sp))
		c.checkExpr(e.Value, nil)
		c.quietMetasIn(e.Value.Span())
		return unit()
	}
	if me.Sym == nil && me.GoFieldType == nil {
		if hasUnsolvedMeta(me.Recv.Type()) {
			// The selection deferred on the receiver's unsolved meta
			// (pendingSels) — a bare `?m` or a shape like `Ptr ?m`;
			// admit the write provisionally — revisitSelects validates
			// the shape and completes Sym once the member resolves.
			// The RHS is checked then too, not here: its want is the
			// member's standing meta, so checking eagerly would leave
			// `Go.funcN` conversions and inline lambdas blind to the
			// field's real signature.
			fa := &FieldAssign{
				Base: baseOf(e.Sp, c.unitResult(e.Sp, "field assignment")),
				Recv: me.Recv,
				Sel:  sel.Sel,
			}
			c.pendingAssigns = append(c.pendingAssigns, &assignTarget{
				Sel: sel, Member: me, Assign: fa, ValueSyn: e.Value,
				bounds:  append([]boundary{}, c.bounds...),
				goDepth: c.goDepth, inTest: c.inTest, tscope: c.curTScope,
				diagMark: -1,
			})
			return fa
		}
	}
	if d := c.assignShapeDiag(sel, me); d != nil {
		c.diags.Add(*d)
		c.checkExpr(e.Value, nil)
		c.quietMetasIn(e.Value.Span())
		return unit()
	}
	v := c.checkExpr(e.Value, me.Type())
	return &FieldAssign{
		Base:        baseOf(e.Sp, c.unitResult(e.Sp, "field assignment")),
		Recv:        me.Recv,
		Sel:         sel.Sel,
		Sym:         me.Sym,
		GoFieldType: me.GoFieldType,
		Value:       v,
	}
}

// assignTarget pairs a deferred field assignment with the syntax
// selection its target checked — revisitSelects validates the
// resolved shape and completes the FieldAssign's Sym/GoFieldType.
type assignTarget struct {
	Sel      *syntax.Select
	Member   *MemberExpr
	Assign   *FieldAssign
	ValueSyn syntax.Expr // the surface RHS — checked at revisit under the resolved member's want

	// The RHS is checked during finalize — after the enclosing fn/
	// lambda boundary, test context and rigid-variable scope were
	// torn down. The deferred checkExpr restores them so `try`,
	// `assert` and `let y : List a` behave as written.
	bounds  []boundary
	goDepth int
	inTest  bool
	tscope  *tscope

	// diagMark/diagEnd delimit the diagnostics the provisional
	// nil-want fill emitted — a member that resolves later re-checks
	// the value under its real signature, and the speculative pass's
	// diagnostics are stale (a `Go.funcN` under no want rejects the
	// very lambda the field accepts). Only that range is rolled
	// back; errors a later round emitted for OTHER deferred work sit
	// after diagEnd and must survive. -1 while unfilled.
	diagMark int
	diagEnd  int
}

// checkAssignRHS runs the deferred RHS check under the context
// captured when the assignment deferred — finalize-time c.bounds,
// inTest and curTScope are already empty.
func (c *checker) checkAssignRHS(pa *assignTarget, want types.Type) Expr {
	savedBounds, savedDepth, savedTest, savedScope := c.bounds, c.goDepth, c.inTest, c.curTScope
	c.bounds, c.goDepth, c.inTest, c.curTScope = pa.bounds, pa.goDepth, pa.inTest, pa.tscope
	v := c.checkExpr(pa.ValueSyn, want)
	c.bounds, c.goDepth, c.inTest, c.curTScope = savedBounds, savedDepth, savedTest, savedScope
	return v
}

// assignShapeDiag validates the resolved shape of a `ptr.Field = v`
// target: an exported Go field reached through a `Ptr T` over a
// named Go struct. Shared by checkAssign for members resolved on
// the spot and by revisitSelects for selections deferred on a
// receiver meta. A nil result admits the write.
func (c *checker) assignShapeDiag(sel *syntax.Select, me *MemberExpr) *diag.Diagnostic {
	if me.Sym == nil || me.GoFieldType == nil {
		d := diag.Error("E0511",
			"the assignment target must be an exported field of a `Ptr T` where T is a named Go struct").
			At(sel.Sp)
		return &d
	}
	rt := types.Chase(me.Recv.Type())
	ptr, isPtr := rt.(*types.Con)
	if !isPtr || !c.isBuiltinCon(ptr, "Ptr") || len(ptr.Args) != 1 {
		rd := types.Display(me.Recv.Type(), metaNamer())
		d := diag.Error("E0511",
			fmt.Sprintf("field assignment is only allowed through a `Ptr T` (T a named Go struct); the receiver has type %s", rd)).
			At(sel.Recv.Span()).Set("type", rd)
		return &d
	}
	if _, isGo := types.Chase(ptr.Args[0]).(*types.Go); !isGo {
		rd := types.Display(me.Recv.Type(), metaNamer())
		d := diag.Error("E0511",
			fmt.Sprintf("field assignment is only allowed on a `Ptr T` where T is a named Go struct; the receiver has type %s", rd)).
			At(sel.Recv.Span()).Set("type", rd)
		return &d
	}
	return nil
}

// conformExprAt is conformExpr with the source position that introduced
// the expected type. The label context is active only while this final
// conformance unifies, so diagnostics inside nested expressions do not
// inherit it.
func (c *checker) conformExprAt(out Expr, want types.Type, sp diag.Span, expectedFrom diag.Span) Expr {
	if expectedFrom.IsZero() {
		return c.conformExpr(out, want, sp)
	}
	prev := c.expectedFrom
	c.expectedFrom = expectedFrom
	defer func() { c.expectedFrom = prev }()
	return c.conformExpr(out, want, sp)
}

// conformExpr applies the want-context conformance to an already
// typed expression — the trailing half of checkExpr, shared with the
// deferred-apply replay whose arguments checked provisionally. An
// interface-typed Go want may route through pendingIfaces or an
// Ascribe conversion; a mismatch reports at sp and quiets want so no
// E0407 piles on.
func (c *checker) conformExpr(out Expr, want types.Type, sp diag.Span) Expr {
	if want == nil {
		return out
	}
	// Go.Any keeps its dedicated all-Gom-values boxing path.
	ifaceWant := c.goOriginType(want) && !isGoAny(types.Chase(want))
	if m, ok := types.Chase(out.Type()).(*types.Meta); ok && c.deferInterfaceSource(m) && ifaceWant {
		if src, ok := c.src.(InterfaceSource); ok && src.InterfaceAssignable(want, want) {
			conv := &Ascribe{Base: baseOf(sp, want), X: out, Conv: true, Iface: true}
			if !c.expectedFrom.IsZero() {
				if c.pendingIfaceFrom == nil {
					c.pendingIfaceFrom = map[*Ascribe]diag.Span{}
				}
				c.pendingIfaceFrom[conv] = c.expectedFrom
			}
			c.pendingIfaces = append(c.pendingIfaces, conv)
			return conv
		}
	}
	if !types.Equal(want, out.Type()) {
		if src, ok := c.src.(InterfaceSource); ok && c.goOriginType(out.Type()) && ifaceWant && src.InterfaceAssignable(out.Type(), want) {
			return &Ascribe{Base: baseOf(sp, want), X: out, Conv: true, Iface: true}
		}
		if ifaceWant && c.implementsIface(want, out.Type()) {
			return &Ascribe{Base: baseOf(sp, want), X: out, Conv: true, Iface: true}
		}
		if conv := c.sliceConv(want, out.Type(), out, sp); conv != nil {
			return conv
		}
		if conv := c.chanDirConv(want, out, sp); conv != nil {
			return conv
		}
		if conv := c.scalarLitConv(want, out, sp); conv != nil {
			return conv
		}
	}
	// A failed expectation left want unsolved: silence its E0407 —
	// the mismatch diagnostic at sp already tells the user what to
	// fix. The silence reaches every meta inside want, not just a
	// top one: `Chan.close (Chan.recvOnly ch)` reports "expected
	// SendChan ?3, got RecvChan ?4" and the callee's still-unbound
	// type argument ?3 must not pile on an E0407 of its own.
	// markSilent, not markQuiet: an unsolved want can be shared by
	// sibling if/match/select arms, and a genuine mismatch at the
	// next arm must still report (docs/implementation-gaps.md).
	if !c.unify(want, out.Type(), sp) {
		if c.wantFailureSink != nil {
			c.wantFailureSink[out] = true
		}
		metasIn(want, func(m *types.Meta) { c.markSilent(m) })
		return out
	}
	// unlabeled→labeled introduction admitted this value under the
	// expected labeled contract: publish that contract on the node
	// itself. Without the ascription the value keeps its unlabeled
	// type — a `let f : ~x: T -> U = fn x -> ...` would publish an
	// unlabeled signature (later `f ~x: v` fails E0406) and a
	// `-> ~x: T -> U` body would fail the residual-type check the
	// Core validator runs (E0901).
	if lentLabels(want, out.Type()) {
		return &Ascribe{Base: baseOf(sp, want), X: out}
	}
	// A value conforming to a `Go.Any` slot materializes the box:
	// the Ascribe lowers through ConvBoxAny so the emitted Go type
	// is the declared `any`, not the value's natural type. Without
	// it a `let x : Go.Any` keeps the concrete representation —
	// `var X string` where the interface advertises `any` — and a
	// `-> Go.Any` body never produces the residual type the Core
	// validator demands.
	if isGoAny(types.Chase(want)) && !isGoAny(types.Chase(out.Type())) {
		return &Ascribe{Base: baseOf(sp, want), X: out, Conv: true}
	}
	return out
}

// scalarLitConv conforms a String/Char/Bool/Unit literal to a
// defined-type declared type (`type X F` — a one-field record
// emitting as `type X string`) whose field is the matching builtin
// — Go's untyped-constant assignability, extended from the numeric
// literals' meta-constraint path to the concretely typed literals.
// Only the literal node qualifies: a typed String variable stays
// rejected, as in Go.
func (c *checker) scalarLitConv(want types.Type, out Expr, sp diag.Span) Expr {
	l, ok := out.(*Lit)
	if !ok {
		return nil
	}
	var name string
	switch l.Kind {
	case syntax.LitStr:
		name = "String"
	case syntax.LitChar:
		name = "Char"
	case syntax.LitBool:
		name = "Bool"
	case syntax.LitUnit:
		name = "Unit"
	default:
		return nil
	}
	w := types.Chase(want)
	u := c.definedUnderlying(w)
	if u == w {
		return nil
	}
	uc, ok := u.(*types.Con)
	if !ok || uc.Qual != "" || uc.Name != name {
		return nil
	}
	// A file-local record's field was spelled in this file, where
	// the caller's `type String` may shadow the builtin — the same
	// scope rule the numeric constraint peel applies in satisfies.
	if wc, ok := w.(*types.Con); ok && wc.Qual == "" && c.shadowedBuiltin(name) {
		return nil
	}
	return &Ascribe{Base: baseOf(sp, want), X: out, Conv: true, Mat: true}
}

// goWant marks want as a live Go-call parameter type for the extent
// of the returned function — chanDirConv consults it for the
// implicit channel direction conversion spec section 10.2 grants Go
// boundaries. checkGoApply and replayGoApply register each
// parameter's type around the argument check that conforms to it.
func (c *checker) goWant(want types.Type) func() {
	if want == nil {
		return func() {}
	}
	if c.goParamWants == nil {
		c.goParamWants = map[types.Type]bool{}
	}
	c.goParamWants[want] = true
	return func() { delete(c.goParamWants, want) }
}

// chanDirConv converts a `Chan a` argument feeding a `SendChan a` or
// `RecvChan a` Go-call parameter into an Ascribe — the implicit
// direction narrowing of spec section 10.2 (`Chan a` may go anywhere
// a directional channel is wanted, matching Go's assignability). The
// conversion only fires when want is a live Go parameter type
// (goParamWants): a Gom `SendChan` parameter or a type annotation
// keeps the explicit `Chan.sendOnly`/`Chan.recvOnly` rule.
func (c *checker) chanDirConv(want types.Type, out Expr, sp diag.Span) Expr {
	if !c.goParamWants[want] {
		return nil
	}
	we, wdir, wok := c.chanElem(want)
	ge, gdir, gok := c.chanElem(out.Type())
	if !wok || !gok || gdir != "both" || wdir == "both" {
		return nil
	}
	if !types.Equal(we, ge) {
		// The elements must agree. An unsolved element meta — a
		// generic callee's type argument — binds through ordinary
		// unification; a concrete mismatch is reported better by
		// the outer unify ("expected SendChan String, got Chan Int").
		if !hasUnboundMeta(we) && !hasUnboundMeta(ge) {
			return nil
		}
		if !c.unify(we, ge, sp) {
			metasIn(want, func(m *types.Meta) { c.markSilent(m) })
			return out
		}
	}
	return &Ascribe{Base: baseOf(sp, want), X: out, Conv: true, ChanDir: true}
}

// hasUnboundMeta reports whether t contains a meta still awaiting a
// binding — the case element agreement needs real unification rather
// than a type-equality check.
func hasUnboundMeta(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Meta:
		return true
	case *types.Con:
		if slices.ContainsFunc(t.Args, hasUnboundMeta) {
			return true
		}
	case *types.Func:
		for _, p := range t.Params {
			if hasUnboundMeta(p.Type) {
				return true
			}
		}
		return hasUnboundMeta(t.Ret)
	case *types.Tuple:
		if slices.ContainsFunc(t.Elems, hasUnboundMeta) {
			return true
		}
	case *types.Go:
		// An instantiated Go named type — `gopkg.Box ?m` — can
		// carry metas in its arguments like Con does.
		if slices.ContainsFunc(t.Args, hasUnboundMeta) {
			return true
		}
	case *types.Var:
		return false
	default:
		panic("unreachable")
	}
	return false
}

// checkIdent resolves a value-position identifier.
func (c *checker) checkIdent(e *syntax.Ident) Expr {
	sym := c.useSym(e)
	typ, targs := c.valOf(sym, e.Sp)
	if sym != nil && sym.Kind == resolve.KindCtor {
		// A constructor's TypeArgs instantiate its ADT — the
		// result's own argument order, not the signature's.
		targs = ctorADTArgs(typ)
	}
	return &Var{Base: baseOf(e.Sp, typ), Sym: sym, TypeArgs: targs}
}

// checkStrInterp types an interpolated string. Each splice is checked
// for its own type — the spec leaves the splice's allowed types open,
// so any value is accepted and lowering renders it.
func (c *checker) checkStrInterp(e *syntax.StrInterp) Expr {
	parts := make([]StrPart, len(e.Parts))
	for i, p := range e.Parts {
		switch p := p.(type) {
		case *syntax.StrText:
			parts[i] = &StrText{Sp: p.Sp, Text: p.Text}
		case *syntax.StrExpr:
			parts[i] = &StrExpr{Sp: p.Sp, X: c.checkExpr(p.X, nil)}
		default:
			panic("unreachable")
		}
	}
	return &StrInterp{Base: baseOf(e.Sp, types.String), Parts: parts}
}
