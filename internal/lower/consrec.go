package lower

import (
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// This file holds the cons-recursion specialization
// (docs/decisions/2026-11-18-cons-recursion-specialization.md — the
// match-cons recursive shape `[h, .., f args]` compiled to a buffer
// loop instead of a recursive call plus an O(n) cons copy).
//
// markConsCalls rewrites the leaf shape
//
//	<leaf lets>: …; let t = self a₀ … aₙ    (the leaf's last let)
//	<leaf tail>: [h₀, .., hₖ, ..t]
//
// into TailCall{Prepend: [h₀..hₖ], Args: a₀..aₙ} and sets ConsLoop on
// the function, so codegen emits `out = append(out, h₀, …); args…;
// continue` inside the same `for` TailLoop already wraps the body
// in, and routes every other tail through `out = append(out,
// tail…)` (docs/design.md section 4.5).
//
// The shape boundary is deliberately narrow — every condition must
// hold or the leaf keeps its ordinary compilation:
//
//   - the leaf's tail is a ListNew whose `..rest` is a Var;
//   - that Var is bound by the leaf's own last let — a self-call
//     bound earlier or in an enclosing body is invisible here;
//   - the let binds an ordinary self CallDirect: the declared
//     arity, the identity self-instantiation (the same constraint
//     markTailCalls documents — the loop reloads the same parameter
//     registers), and not a value-adapter call (an Adapter call is
//     a value's firing call, not a call site — rewriting it would
//     corrupt @tailrec's R3 accounting);
//   - the rest var is read nowhere else in the leaf — the rewrite
//     drops its let, so a second use would lose the value;
//   - every head atom is *stable* — its read yields the same value
//     at the leaf and at the unwind point the recursive version
//     reads it at (stableHead). Complex heads already sit in the
//     leaf's lets as evaluated temporaries (Go field reads are
//     snapshot-bound for exactly this reason); the only atom that
//     still observes time is a Go package variable, which any deeper
//     iteration may reassign.
//
// Equivalence of the accepted shape: appending the cons heads to the
// buffer, rebinding the arguments and continuing is the same ordered
// list construction the recursion produced — `[h, ..f t]` is
// head-then-tail, and the loop emits heads in call order then
// appends whichever base leaf ends the iteration. A leaf that does
// not convert keeps its semantics under the buffer destination too:
// its whole list value is appended as the iteration's remainder.
// The rewrite is silent — a missed shape keeps the recursive
// compilation it always had, per the decision's "取りこぼし時の沈黙".
func (l *lowerer) markConsCalls(fn *core.Func, self *resolve.Symbol, tparams []*types.Var) {
	found := false
	var body func(b *core.Body)
	var tail func(b *core.Body)
	var tree func(t core.DTree)

	body = func(b *core.Body) {
		if b != nil && b.Tail != nil {
			tail(b)
		}
	}
	tree = func(t core.DTree) {
		switch t := t.(type) {
		case *core.DSwitch:
			for i := range t.Cases {
				body(t.Cases[i].Body)
			}
			if t.Default != nil {
				tree(t.Default)
			}
		case *core.DGuard:
			tree(t.Then)
			tree(t.Else)
		case *core.DLeaf:
			body(t.Body)
		case *core.DPanic:
		default:
			panic("unreachable")
		}
	}
	tail = func(b *core.Body) {
		// Partial probes stay as if-assertions (markTailCalls idiom):
		// core.Expr is a sumtype and the checker requires exhaustive
		// switches.
		if e, ok := b.Tail.(*core.If); ok {
			body(e.Then)
			if e.Else != nil {
				body(e.Else)
			}
			return
		}
		if e, ok := b.Tail.(*core.Match); ok {
			tree(e.Tree)
			return
		}
		if e, ok := b.Tail.(*core.SelectExpr); ok {
			for _, a := range e.Arms {
				switch a := a.(type) {
				case *core.SelRecv:
					body(a.Body)
				case *core.SelSend:
					body(a.Body)
				case *core.SelDefault:
					body(a.Body)
				default:
					panic("unreachable")
				}
			}
			return
		}
		if e, ok := b.Tail.(*core.ListNew); ok {
			l.consLeaf(b, e, fn, self, tparams, &found)
		}
	}
	body(fn.Body)
	if found {
		fn.ConsLoop = true
		fn.TailLoop = true
	}
}

// consLeaf tries the rewrite on one leaf body ending in a ListNew —
// every bail keeps the leaf exactly as lowered.
func (l *lowerer) consLeaf(b *core.Body, ln *core.ListNew, fn *core.Func, self *resolve.Symbol, tparams []*types.Var, found *bool) {
	rest, ok := ln.Rest.(*core.Var)
	if !ok || rest.Sym == nil {
		return
	}
	n := len(b.Lets)
	if n == 0 {
		return
	}
	last := b.Lets[n-1]
	if last == nil || last.Name == nil || last.Name.Sym != rest.Sym {
		return
	}
	call, ok := last.X.(*core.CallDirect)
	if !ok || call.Callee != self || call.Adapter ||
		len(call.Args) != len(fn.Params) ||
		!identityInstantiation(call.TypeArgs, tparams) {
		return
	}
	// The rewrite drops the call's let, so the var may not be read
	// anywhere else in the leaf — a second use (inside the cons
	// heads, a nested closure, a later expression) would lose the
	// value the buffer path never materializes.
	uses := 0
	eachVar(b, func(v *core.Var, _ bool) {
		if v.Sym == rest.Sym {
			uses++
		}
	})
	if uses != 1 {
		return
	}
	// The rewrite moves each head's read from the unwind point —
	// after the self-call returns — to before the loop continues.
	// Heads that can observe that shift stay recursive.
	for _, h := range ln.Elems {
		if !stableHead(h) {
			return
		}
	}
	b.Lets = b.Lets[:n-1]
	b.Tail = &core.TailCall{
		Base:    ln.Base,
		To:      fn,
		Args:    call.Args,
		Prepend: ln.Elems,
	}
	*found = true
}

// stableHead reports whether an atom in Prepend position observes
// the same value before the loop restart and at the unwind point
// the recursive version reads it. Everything an atom can reach is
// Gom data — immutable — except a Go package variable (`os.Stdout`,
// a Go `var`), which a deeper iteration may reassign; rejecting
// those keeps the head reads at their original timing.
func stableHead(a core.Atom) bool {
	switch a := a.(type) {
	case *core.Var:
		return a.Sym == nil || !a.Sym.IsGo
	case *core.Lit, *core.FuncRef, *core.CtorRef:
		return true
	case *core.GoFuncRef:
		return a.Recv == nil || stableHead(a.Recv)
	case *core.FieldGet:
		return stableHead(a.Recv)
	default:
		panic("unreachable")
	}
}
