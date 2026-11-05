package lower

import (
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
)

// This file holds the TailLoop capture repair
// (docs/problems/2026-11-02-tailloop-param-capture.md). Codegen
// compiles a TailLoop function to `for { ...; p0, p1 = a0, a1;
// continue }` — one shared set of parameter variables rebound every
// iteration. A nested Closure or Spawn that captures a parameter
// therefore reads the cell *after* later rebinds and observes the
// final value, not the value of the call that created it (and, for
// Spawn, races the rebind itself). Gom semantics bind fresh
// parameters per call, so the lowering pass below restores them.

// snapshotCapturedParams gives a TailLoop function per-iteration
// parameter bindings for everything a capture can reach.
//
// For every parameter referenced under a capture position — inside
// a nested Closure or a Spawn body — it prepends a copy `p' = p` to
// the body and rebinds each use of p in the original body to p'.
// Rebinding is uniform — capture positions and synchronous
// positions alike — so a use can never accidentally read the loop
// register; the copy and the register agree within an iteration
// anyway (the register changes only at TailCall, which continues
// immediately). fn.Params keeps the original symbols: they stay the
// registers the TailCall multi-assignment writes and each copy
// reads. Because the copies are Lets inside the loop body, codegen
// declares fresh Go variables every iteration and the captured
// closure/goroutine sees a stable value.
func (l *lowerer) snapshotCapturedParams(fn *core.Func) {
	params := map[*resolve.Symbol]*core.Bind{}
	for _, p := range fn.Params {
		if p != nil && p.Sym != nil && !p.Discard {
			params[p.Sym] = p
		}
	}
	if len(params) == 0 {
		return
	}
	captured := map[*resolve.Symbol]bool{}
	eachVar(fn.Body, func(v *core.Var, inside bool) {
		if inside {
			if _, ok := params[v.Sym]; ok {
				captured[v.Sym] = true
			}
		}
	})
	if len(captured) == 0 {
		return
	}
	// The copies are built before the rename but inserted after it:
	// their `p` reads are the point and must keep the parameter
	// symbol — walking them through subst would produce `p' = p'`.
	var copies []*core.Let
	subst := map[*resolve.Symbol]*resolve.Symbol{}
	for _, p := range fn.Params {
		if p == nil || p.Sym == nil || !captured[p.Sym] {
			continue
		}
		// The snapshot keeps the parameter's name — codegen dedups
		// the binder (`n` → `n1`), which reads like the shadow it is.
		fresh := &resolve.Symbol{Name: p.Sym.Name, Kind: resolve.KindVar}
		subst[p.Sym] = fresh
		copies = append(copies, &core.Let{
			Sp:   p.Sp,
			Name: &core.Bind{Sp: p.Sp, Sym: fresh, Typ: p.Typ},
			X:    &core.Var{Base: core.Base{Sp: p.Sp, Typ: p.Typ}, Sym: p.Sym},
		})
	}
	eachVar(fn.Body, func(v *core.Var, _ bool) {
		if ns, ok := subst[v.Sym]; ok {
			v.Sym = ns
		}
	})
	fn.Body.Lets = append(copies, fn.Body.Lets...)
}

// eachVar calls f on every Var node in the tree rooted at b. The
// flag argument tells f whether the occurrence sits under a capture
// position — a nested function boundary (Closure) or a spawned
// body — whose reads escape the enclosing iteration's own timeline.
// The flag is monotone: once a boundary is crossed, every deeper
// use counts as captured (a closure inside a spawn still captures).
// Boundary back-pointers (TailCall.To, EscapeResult.To, ...) are
// never followed — they name enclosing functions, not subtrees.
func eachVar(b *core.Body, f func(v *core.Var, captured bool)) {
	var body func(b *core.Body, captured bool)
	var expr func(e core.Expr, captured bool)
	var atom func(a core.Atom, captured bool)
	var atoms func(as []core.Atom, captured bool)
	var callGo func(c *core.CallGo, captured bool)
	var tree func(t core.DTree, captured bool)
	var selArm func(a core.SelArm, captured bool)

	body = func(b *core.Body, captured bool) {
		if b == nil {
			return
		}
		for _, l := range b.Lets {
			if l != nil {
				expr(l.X, captured)
			}
		}
		expr(b.Tail, captured)
	}
	atom = func(a core.Atom, captured bool) {
		switch a := a.(type) {
		case *core.Var:
			f(a, captured)
		case *core.Lit, *core.FuncRef, *core.CtorRef:
		case *core.GoFuncRef:
			if a.Recv != nil {
				atom(a.Recv, captured)
			}
		case *core.FieldGet:
			atom(a.Recv, captured)
		default:
			panic("unreachable")
		}
	}
	atoms = func(as []core.Atom, captured bool) {
		for _, a := range as {
			atom(a, captured)
		}
	}
	callGo = func(c *core.CallGo, captured bool) {
		if c == nil {
			return
		}
		if c.Recv != nil {
			atom(c.Recv, captured)
		}
		atoms(c.Args, captured)
	}
	tree = func(t core.DTree, captured bool) {
		switch t := t.(type) {
		case *core.DLeaf:
			body(t.Body, captured)
		case *core.DPanic:
			atom(t.Subj, captured)
		case *core.DGuard:
			atom(t.Cond, captured)
			tree(t.Then, captured)
			tree(t.Else, captured)
		case *core.DSwitch:
			atom(t.Subj, captured)
			for i := range t.Cases {
				body(t.Cases[i].Body, captured)
			}
			if t.Default != nil {
				tree(t.Default, captured)
			}
		default:
			panic("unreachable")
		}
	}
	selArm = func(a core.SelArm, captured bool) {
		switch a := a.(type) {
		case *core.SelRecv:
			atom(a.Chan, captured)
			body(a.Body, captured)
		case *core.SelSend:
			atom(a.Chan, captured)
			atom(a.Value, captured)
			body(a.Body, captured)
		case *core.SelDefault:
			body(a.Body, captured)
		default:
			panic("unreachable")
		}
	}
	expr = func(e core.Expr, captured bool) {
		switch e := e.(type) {
		case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
			*core.GoFuncRef, *core.FieldGet:
			atom(e.(core.Atom), captured)
		case *core.Closure:
			if e.Fn != nil {
				body(e.Fn.Body, true)
			}
		case *core.CallDirect:
			atoms(e.Args, captured)
		case *core.CallFunc:
			atom(e.Fn, captured)
			atoms(e.Args, captured)
		case *core.CallGo:
			callGo(e, captured)
		case *core.CallBuiltin:
			atoms(e.Args, captured)
		case *core.CtorApp:
			atoms(e.Args, captured)
		case *core.Unary:
			atom(e.X, captured)
		case *core.Binary:
			atom(e.X, captured)
			atom(e.Y, captured)
		case *core.Equal:
			atom(e.X, captured)
			atom(e.Y, captured)
		case *core.If:
			atom(e.Cond, captured)
			body(e.Then, captured)
			body(e.Else, captured)
		case *core.Match:
			tree(e.Tree, captured)
		case *core.SelectExpr:
			for _, a := range e.Arms {
				selArm(a, captured)
			}
		case *core.Spawn:
			body(e.Body, true)
		case *core.ForRange:
			atom(e.Iter, captured)
			body(e.Body, captured)
		case *core.TupleNew:
			atoms(e.Elems, captured)
		case *core.ListNew:
			atoms(e.Elems, captured)
			if e.Rest != nil {
				atom(e.Rest, captured)
			}
		case *core.RecordNew:
			for _, fi := range e.Fields {
				atom(fi.Value, captured)
			}
		case *core.RecordUpdate:
			atom(e.Rec, captured)
			for _, fi := range e.Fields {
				atom(fi.Value, captured)
			}
		case *core.TailCall:
			atoms(e.Args, captured)
		case *core.EscapeResult:
			atom(e.X, captured)
		case *core.EscapeCall:
			callGo(e.Call, captured)
		case *core.ResultFromGo:
			callGo(e.Call, captured)
		case *core.OptionFromGo:
			callGo(e.Call, captured)
		case *core.TupleFromGo:
			callGo(e.Call, captured)
		case *core.ErrorFromGo:
			callGo(e.Call, captured)
		case *core.Conv:
			atom(e.X, captured)
		default:
			panic("unreachable")
		}
	}
	body(b, false)
}
