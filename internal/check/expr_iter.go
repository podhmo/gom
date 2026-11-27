package check

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// pendingIter records a `for` loop whose iterable's type was a meta
// tied to deferred work — a member alias's standing meta or a
// deferred call's result meta. checkFor minted the element meta
// without pinning `List`; finishIters runs the iterable-shape rules
// once the meta settles (or binds the List default when it never
// does).
type pendingIter struct {
	t    types.Type // the meta the iterable's type chases to
	elem types.Type // the element meta the pattern checked against
	iter Expr       // the checked iterable node (span, resolved type)
	done bool       // fillIters drained it
}

// iterElemCon extracts the element type of a concrete iterable —
// builtin List, a List-derived View (same `range` lowering as List:
// the shared backing slice), `Go.Slice` (decisions/for-over-go-slice:
// index order, nil iterates zero times), or a receivable channel.
// ok=false means the shape does not iterate.
func (c *checker) iterElemCon(t *types.Con, sp diag.Span) (types.Type, bool) {
	if t.Qual == "" && t.Name == "List" && len(t.Args) == 1 &&
		!c.shadowedBuiltin("List") {
		return t.Args[0], true
	}
	if t.Qual == "" && t.Name == "View" && len(t.Args) == 1 &&
		!c.shadowedBuiltin("View") {
		return t.Args[0], true
	}
	if t.Qual == "Go" && t.Name == "Slice" && len(t.Args) == 1 {
		return t.Args[0], true
	}
	if el, dir, ok := c.chanElem(t); ok {
		if dir == "send" {
			c.diags.Add(diag.Error("E0401",
				"cannot iterate a send-only channel").At(sp))
		}
		return el, true
	}
	return nil, false
}

// iterPendingOn reports whether the meta the iterable's type chases
// to is owned by deferred work still awaiting resolution — a pending
// member selection's standing meta or a pending application's
// result meta — whose real solution a later finalize round
// supplies. A processed selection (resolved or already failed:
// its quiet standing meta is debris) no longer claims the iterable.
func (c *checker) iterPendingOn(t *types.Meta) bool {
	for _, me := range c.pendingSels {
		if c.selDone[me] {
			continue
		}
		if types.Chase(me.Type()) == types.Type(t) {
			return true
		}
	}
	for _, q := range c.pendingApplies {
		if !q.done && types.Chase(q.ap.Type()) == types.Type(t) {
			return true
		}
	}
	return false
}

// iterFailedOn reports whether the meta the iterable's type chases
// to is the standing meta of a selection that already failed or the
// result of an application dropped as dead work — its type was
// never established, so the iterable must not take the List
// default (or its shadowing diagnostic) at all.
func (c *checker) iterFailedOn(t *types.Meta) bool {
	if c.selDone != nil {
		for _, me := range c.pendingSels {
			if c.selDone[me] && types.Chase(me.Type()) == types.Type(t) {
				return true
			}
		}
	}
	for _, q := range c.pendingApplies {
		// Only applies dropped as dead work or whose replay failed —
		// a committed apply whose result is still an open meta is a
		// live iterable, not a failure, and must still take the
		// List default.
		if (q.dropped || q.failed) && types.Chase(q.ap.Type()) == types.Type(t) {
			return true
		}
	}
	return false
}

// fillIters drains the pendingIters whose iterable has settled:
// a concrete shape reconciles the real element type with the
// pattern's meta — early, inside the finalize rounds, so member
// selections and literals minted on the loop variable (`v.N`,
// `v + 1`) still resolve under it. With defaults=true a still-
// unbound iterable takes the `List elem` default the eager path
// would have bound at check time — same stable-round gate as
// applyDefaults. Returns whether it did work.
func (c *checker) fillIters(defaults bool) bool {
	progressed := false
	for _, pi := range c.pendingIters {
		if pi.done {
			continue
		}
		sp := pi.iter.Span()
		switch t := types.Chase(pi.t).(type) {
		case *types.Meta:
			if !defaults {
				continue
			}
			// A meta claimed as pending work's RESULT — a member's
			// standing meta or a deferred call's result — is left
			// for that resolution: its real shape (a channel, a
			// Go.Slice) must arrive before any List default would
			// pin it. A meta claimed only as pending work's
			// RECEIVER defaults here so the member can resolve —
			// the call's replay then sees the List's signature
			// (and its labeled parameters) instead of a committed
			// `Unit -> ?r` fallback.
			if c.iterPendingOn(t) {
				continue
			}
			if c.iterFailedOn(t) {
				// Claimed by a selection that already FAILED: no
				// type was ever established, so binding `List`
				// would both invent an iterable and (when List is
				// shadowed) pile a spurious shadowing error on the
				// missing-member diagnostic. Leave the quiet meta
				// alone; the member's own error covers the loop.
				// Quieting `t` itself matters for failed replays —
				// their result meta was never quieted by the drop,
				// and leaving it loud asks for a type annotation
				// on top of the replay's own diagnostic.
				c.markQuiet(t)
				c.markQuiet(pi.elem)
				pi.done = true
				progressed = true
				continue
			}
			if !c.bind(t, types.List(pi.elem), sp) {
				c.markQuiet(pi.elem)
			}
			// A quiet iterable meta — dead deferred work whose own
			// diagnostic already reported the failure — quiets the
			// element the default introduces too: otherwise an
			// unrelated E0407 piles onto the member's error.
			if c.recOf(t).quiet {
				c.markQuiet(pi.elem)
			}
			if c.shadowedBuiltin("List") {
				c.shadowedErr(sp, "`for` iteration", "List")
				c.markQuiet(pi.elem)
			}
		case *types.Con:
			if el, ok := c.iterElemCon(t, sp); ok {
				if isGoAny(types.Chase(el)) && types.Chase(pi.elem) != el {
					// `unify` treats a `Go.Any` want as a slot
					// that accepts any value — it would leave
					// elem unbound and the loop variable
					// typeless. The element IS `Go.Any`.
					if m, isMeta := types.Chase(pi.elem).(*types.Meta); isMeta {
						c.bind(m, el, sp)
					} else {
						c.unify(el, pi.elem, sp)
					}
				} else {
					c.unify(el, pi.elem, sp)
				}
			} else {
				if t.Qual == "" && c.shadowedBuiltin(t.Name) &&
					(t.Name == "List" || t.Name == "View" || t.Name == "Chan" || t.Name == "RecvChan" || t.Name == "SendChan") {
					c.shadowedErr(sp, "`for` iteration", t.Name)
				} else {
					c.mismatch(sp, "a List, a Go.Slice, or a channel", pi.iter.Type())
				}
				c.markQuiet(pi.elem)
			}
		case *types.Var, *types.Func, *types.Tuple, *types.Go:
			c.mismatch(sp, "a List, a Go.Slice, or a channel", pi.iter.Type())
			c.markQuiet(pi.elem)
		default:
			panic("unreachable")
		}
		pi.done = true
		progressed = true
	}
	return progressed
}

// finishIters is the tail pass for iterables that never settled —
// dead deferred work whose quiet meta takes the List default (quiet
// propagates through the bind).
func (c *checker) finishIters() {
	c.fillIters(true)
	c.pendingIters = nil
}

// unifySlicePair reconciles the List ↔ Go.Slice boundary pair —
// same runtime slice — when a deferred type (a member's standing
// meta, a deferred call's result) bound across it before its real
// type arrived: an argument slot expecting `Go.Slice` pins the meta
// ahead of the member's `List`. The return tells the caller what
// the pair implied:
//
//	0 — not a List↔Go.Slice pair; unify normally
//	1 — reconciled; no copy needed (the producer's view outranks
//	    the pin, and the standard conv paths still see it)
//	2 — reconciled, but the claimed side is `Go.Slice` while the
//	    producer yields `List`: the value must still be copied, so
//	    the caller records the declared type for lowering
//
// A `Go.Any` element never reconciles — the implicit copy only
// boxes INTO `Go.Slice Go.Any` (sliceConv), so an `any` mismatch
// falls back to the ordinary unify's diagnostic.
func (c *checker) unifySlicePair(want, got types.Type, sp diag.Span) int {
	wc, wok := types.Chase(want).(*types.Con)
	gc, gok := types.Chase(got).(*types.Con)
	if !wok || !gok || len(wc.Args) != 1 || len(gc.Args) != 1 {
		return 0
	}
	isGoSlice := func(con *types.Con) bool { return con.Qual == "Go" && con.Name == "Slice" }
	isList := func(con *types.Con) bool {
		return con.Qual == "" && con.Name == "List" && !c.shadowedBuiltin("List")
	}
	we, ge := wc.Args[0], gc.Args[0]
	if !types.Equal(we, ge) &&
		(isGoAny(types.Chase(we)) || isGoAny(types.Chase(ge))) {
		// An `any` element reconciles only when both sides name
		// it — the implicit copy boxes INTO `Go.Slice Go.Any`
		// (sliceConv), so a one-sided any falls back to the
		// ordinary mismatch.
		return 0
	}
	switch {
	case isGoSlice(wc) && isList(gc):
		if !c.unify(we, ge, sp) {
			return 1
		}
		return 2
	case isList(wc) && isGoSlice(gc):
		c.unify(we, ge, sp)
		return 1
	default:
		return 0
	}
}

// unifyMember solves a deferred member's standing meta against the
// member's resolved type, reconciling a standing meta an earlier
// constraint already bound across the List ↔ Go.Slice boundary. A
// `Go.Slice` claim over a `List` member marks the read: lowering
// emits the copy the List view promises.
func (c *checker) unifyMember(me *MemberExpr, typ types.Type) {
	switch c.unifySlicePair(me.Type(), typ, me.Sel.Sp) {
	case 0:
		c.unify(me.Type(), typ, me.Sel.Sp)
	case 2:
		me.CopyFrom = typ
	}
}

// checkFor types `for pat in iter { body }`: iter is a List, a
// Go.Slice, or a receivable channel; the pattern binds each element;
// the body is Unit.
func (c *checker) checkFor(e *syntax.For) Expr {
	iter := c.checkExpr(e.Iter, nil)
	var elem types.Type
	switch t := types.Chase(iter.Type()).(type) {
	case *types.Con:
		if el, ok := c.iterElemCon(t, e.Iter.Span()); ok {
			elem = el
		} else {
			if t.Qual == "" && c.shadowedBuiltin(t.Name) &&
				(t.Name == "List" || t.Name == "View" || t.Name == "Chan" || t.Name == "RecvChan" || t.Name == "SendChan") {
				c.shadowedErr(e.Iter.Span(),
					"`for` iteration", t.Name)
			} else {
				c.mismatch(e.Iter.Span(), "a List, a Go.Slice, or a channel", iter.Type())
			}
			// Recovery: pretend the value iterates itself so the
			// pattern still has a concrete type to bind.
			elem = iter.Type()
		}
	case *types.Meta:
		elem = c.newMeta(e.Iter.Span(), "the iterated element's type")
		if c.iterPendingOn(t) {
			// The iterable's type waits on a deferred member or
			// call (`for v in f a` with `f` a member alias):
			// binding `List elem` here would pin the pending
			// result meta before the member's signature —
			// possibly View, a Go slice or a channel — arrives.
			// finishIters reconciles the element once the
			// iterable settles at finalize.
			c.pendingIters = append(c.pendingIters,
				&pendingIter{t: t, elem: elem, iter: iter})
			break
		}
		if !c.bind(t, types.List(elem), e.Iter.Span()) {
			c.markQuiet(elem)
		}
		// The bound meta spells `List` — under a file-local `type
		// List` that is the local ADT, which `for` cannot range
		// over: the element type needs an explicit channel or
		// builtin-List annotation. Quiet the minted element meta so
		// the E0415 does not grow a dangling-type cascade.
		if c.shadowedBuiltin("List") {
			c.shadowedErr(e.Iter.Span(), "`for` iteration", "List")
			c.markQuiet(elem)
		}
	case *types.Var, *types.Func, *types.Tuple, *types.Go:
		c.mismatch(e.Iter.Span(), "a List, a Go.Slice, or a channel", iter.Type())
		elem = iter.Type()
	default:
		panic("unreachable")
	}
	pat := c.checkPat(e.Pat, elem)
	body := c.checkExpr(e.Body, types.Unit)
	return &For{Base: baseOf(e.Sp, c.unitResult(e.Sp, "`for`")), Pat: pat, Iter: iter, Body: body}
}
