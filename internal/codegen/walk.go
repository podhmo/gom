package codegen

import (
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
)

// This file walks a Core IR file to collect names — every binder in
// the file registers its emitted identifier so generated temporaries
// (escape err vars, record-update copies, select receive pairs,
// type-switch guards) can never collide with a user name.

// walkDeclBinders calls fn on every *core.Bind in one declaration —
// parameters before their body, lets in order, nested arms and
// closures as encountered.
func walkDeclBinders(d core.Decl, fn func(*core.Bind)) {
	switch d := d.(type) {
	case *core.FuncDecl:
		walkFunc(d.Fn, fn)
	case *core.VarDecl:
		walkBody(d.Body, fn)
	case *core.TestDecl:
		walkFunc(d.Fn, fn)
	case *core.TypeDecl:
		// type declarations introduce no value binders
	default:
		panic("unreachable")
	}
}

func walkFunc(fn *core.Func, f func(*core.Bind)) {
	if fn == nil {
		return
	}
	for _, b := range fn.Params {
		f(b)
	}
	walkBody(fn.Body, f)
}

func walkBody(b *core.Body, f func(*core.Bind)) {
	if b == nil {
		return
	}
	for _, l := range b.Lets {
		f(l.Name)
		walkExpr(l.X, f)
	}
	walkExpr(b.Tail, f)
}

func walkExpr(e core.Expr, f func(*core.Bind)) {
	switch e := e.(type) {
	case *core.Closure:
		walkFunc(e.Fn, f)
	case *core.If:
		walkBody(e.Then, f)
		walkBody(e.Else, f)
	case *core.Match:
		walkDTree(e.Tree, f)
	case *core.SelectExpr:
		for _, a := range e.Arms {
			walkSelArm(a, f)
		}
	case *core.Spawn:
		walkBody(e.Body, f)
	case *core.ForRange:
		f(e.Elem)
		walkBody(e.Body, f)
	case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFuncRef, *core.FieldGet,
		*core.Unary, *core.Binary, *core.Equal,
		*core.CallDirect, *core.CallFunc, *core.CallGo, *core.CallBuiltin,
		*core.CtorApp, *core.TupleNew, *core.ListNew,
		*core.RecordNew, *core.RecordUpdate, *core.Conv,
		*core.TailCall, *core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.TupleFromGo:
		// Atoms, calls, conversions, records, escapes and tail
		// calls introduce no binders.
	default:
		panic("unreachable")
	}
}

func walkSelArm(a core.SelArm, f func(*core.Bind)) {
	switch a := a.(type) {
	case *core.SelRecv:
		f(a.Bind)
		walkBody(a.Body, f)
	case *core.SelSend:
		walkBody(a.Body, f)
	case *core.SelDefault:
		walkBody(a.Body, f)
	default:
		panic("unreachable")
	}
}

func walkDTree(t core.DTree, f func(*core.Bind)) {
	switch t := t.(type) {
	case *core.DLeaf:
		walkBody(t.Body, f)
	case *core.DGuard:
		walkDTree(t.Then, f)
		walkDTree(t.Else, f)
	case *core.DSwitch:
		for _, c := range t.Cases {
			for _, b := range c.Binds {
				f(b)
			}
			walkBody(c.Body, f)
		}
		if t.Default != nil {
			walkDTree(t.Default, f)
		}
	case *core.DPanic:
	default:
		panic("unreachable")
	}
}

// --- symbol use ---

// Go rejects a declared-and-never-used variable, so binders whose
// symbol is never referenced in their body are elided — a type-switch
// guard, a case payload, a select receive pair, a loop element. This
// is mechanical: the Core nodes declare what may be bound, and an
// unused symbol simply produces no Go variable (the construct already
// permits the unbound form).

// bindUsed reports whether the binder's symbol is referenced inside
// its scope — the body for case/receive arms and loops.
func bindUsed(b *core.Bind, body *core.Body) bool {
	if b == nil || b.Discard || b.Sym == nil {
		return false
	}
	return bodyUses(body, b.Sym)
}

func bodyUses(b *core.Body, sym *resolve.Symbol) bool {
	return bodyUsesFrom(b, sym, 0)
}

// bodyUsesFrom reports whether sym is referenced by the let's at or
// after index start, or by the tail — the scope of a binding at
// index start-1.
func bodyUsesFrom(b *core.Body, sym *resolve.Symbol, start int) bool {
	if b == nil {
		return false
	}
	for _, l := range b.Lets[start:] {
		if exprUses(l.X, sym) {
			return true
		}
	}
	return exprUses(b.Tail, sym)
}

func atomUses(a core.Atom, sym *resolve.Symbol) bool {
	switch a := a.(type) {
	case *core.Var:
		return a.Sym == sym
	case *core.Lit:
		return false
	case *core.FuncRef:
		return a.Sym == sym
	case *core.CtorRef:
		return a.Sym == sym
	case *core.GoFuncRef:
		return a.Recv != nil && atomUses(a.Recv, sym)
	case *core.FieldGet:
		return atomUses(a.Recv, sym)
	default:
		panic("unreachable")
	}
}

func atomsUses(as []core.Atom, sym *resolve.Symbol) bool {
	for _, a := range as {
		if atomUses(a, sym) {
			return true
		}
	}
	return false
}

func exprUses(e core.Expr, sym *resolve.Symbol) bool {
	switch e := e.(type) {
	case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFuncRef, *core.FieldGet:
		return atomUses(e.(core.Atom), sym)
	case *core.Closure:
		return funcUses(e.Fn, sym)
	case *core.CallDirect:
		return atomsUses(e.Args, sym)
	case *core.CtorApp:
		return atomsUses(e.Args, sym)
	case *core.CallFunc:
		return atomUses(e.Fn, sym) || atomsUses(e.Args, sym)
	case *core.CallGo:
		return callGoUses(e, sym)
	case *core.CallBuiltin:
		return atomsUses(e.Args, sym)
	case *core.Unary:
		return atomUses(e.X, sym)
	case *core.Binary:
		return atomUses(e.X, sym) || atomUses(e.Y, sym)
	case *core.Equal:
		return atomUses(e.X, sym) || atomUses(e.Y, sym)
	case *core.If:
		return atomUses(e.Cond, sym) || bodyUses(e.Then, sym) || bodyUses(e.Else, sym)
	case *core.Match:
		return dtreeUses(e.Tree, sym)
	case *core.SelectExpr:
		for _, a := range e.Arms {
			if selArmUses(a, sym) {
				return true
			}
		}
		return false
	case *core.Spawn:
		return bodyUses(e.Body, sym)
	case *core.ForRange:
		return atomUses(e.Iter, sym) || bodyUses(e.Body, sym)
	case *core.TupleNew:
		return atomsUses(e.Elems, sym)
	case *core.ListNew:
		return atomsUses(e.Elems, sym) || (e.Rest != nil && atomUses(e.Rest, sym))
	case *core.RecordNew:
		for _, f := range e.Fields {
			if atomUses(f.Value, sym) {
				return true
			}
		}
		return false
	case *core.RecordUpdate:
		if atomUses(e.Rec, sym) {
			return true
		}
		for _, f := range e.Fields {
			if atomUses(f.Value, sym) {
				return true
			}
		}
		return false
	case *core.TailCall:
		return atomsUses(e.Args, sym)
	case *core.EscapeResult:
		return atomUses(e.X, sym)
	case *core.EscapeCall:
		return callGoUses(e.Call, sym)
	case *core.ResultFromGo:
		return callGoUses(e.Call, sym)
	case *core.OptionFromGo:
		return callGoUses(e.Call, sym)
	case *core.TupleFromGo:
		return callGoUses(e.Call, sym)
	case *core.Conv:
		return atomUses(e.X, sym)
	default:
		panic("unreachable")
	}
}

func funcUses(fn *core.Func, sym *resolve.Symbol) bool {
	return fn != nil && bodyUses(fn.Body, sym)
}

func callGoUses(c *core.CallGo, sym *resolve.Symbol) bool {
	return (c.Recv != nil && atomUses(c.Recv, sym)) || atomsUses(c.Args, sym)
}

func selArmUses(a core.SelArm, sym *resolve.Symbol) bool {
	switch a := a.(type) {
	case *core.SelRecv:
		return atomUses(a.Chan, sym) || bodyUses(a.Body, sym)
	case *core.SelSend:
		return atomUses(a.Chan, sym) || atomUses(a.Value, sym) || bodyUses(a.Body, sym)
	case *core.SelDefault:
		return bodyUses(a.Body, sym)
	default:
		panic("unreachable")
	}
}

func dtreeUses(t core.DTree, sym *resolve.Symbol) bool {
	switch t := t.(type) {
	case *core.DLeaf:
		return bodyUses(t.Body, sym)
	case *core.DGuard:
		return atomUses(t.Cond, sym) || dtreeUses(t.Then, sym) || dtreeUses(t.Else, sym)
	case *core.DSwitch:
		if atomUses(t.Subj, sym) {
			return true
		}
		for _, c := range t.Cases {
			if bodyUses(c.Body, sym) {
				return true
			}
		}
		return t.Default != nil && dtreeUses(t.Default, sym)
	case *core.DPanic:
		return atomUses(t.Subj, sym)
	default:
		panic("unreachable")
	}
}
