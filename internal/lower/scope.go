package lower

import (
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// This file is SC-3 (docs/decisions/2026-11-21-scoped-cleanup.md):
// a `Scope.run` / `Scope.run_result` whose body is a lambda that
// registers every cleanup in its own Go function lowers to native Go
// `defer`s. The body Func gains ScopeInfo, each registration becomes
// a core.Defer, and the rt call becomes a direct call of the body.
// Any other shape — the Scope handed to a helper, a registration
// inside a nested closure or go{} — keeps SC-1's runtime stack.

const (
	scopeRtPkg     = "github.com/podhmo/gom/rt"
	scopeResultPkg = "github.com/podhmo/gom/stdlib/result"
)

// scopeCallee classifies a CallGo callee among the Scope members.
func scopeCallee(c *core.CallGo) (run, result bool, k core.DeferKind) {
	if c == nil || c.Callee == nil || c.Recv != nil {
		return false, false, 0
	}
	switch c.Callee.Package + "." + c.Callee.Name {
	case scopeRtPkg + ".ScopeRun":
		return true, false, 0
	case scopeResultPkg + ".ScopeRunResult":
		return true, true, 0
	case scopeRtPkg + ".ScopeDefer":
		return false, false, core.DeferAlways
	case scopeResultPkg + ".ScopeDeferErr":
		return false, false, core.DeferErr
	case scopeRtPkg + ".ScopeOnErr":
		return false, false, core.DeferOnErr
	}
	return false, false, 0
}

// nativeScopes rewrites every qualifying Scope body in the file.
func (l *lowerer) nativeScopes(out *core.File) {
	for _, d := range out.Decls {
		var b *core.Body
		switch d := d.(type) {
		case *core.FuncDecl:
			if d.Fn != nil {
				b = d.Fn.Body
			}
		case *core.TestDecl:
			if d.Fn != nil {
				b = d.Fn.Body
			}
		case *core.VarDecl:
			b = d.Body
		case *core.TypeDecl:
		default:
			panic("unreachable")
		}
		if b != nil {
			eachBody(b, func(b *core.Body) { l.nativeScopeBody(b) })
		}
	}
}

// nativeScopeBody rewrites `let k = fn s -> ...; ... ScopeRun(k)`
// pairs bound in b.
func (l *lowerer) nativeScopeBody(b *core.Body) {
	for i, lt := range b.Lets {
		c, ok := lt.X.(*core.Closure)
		if !ok || lt.Name.Sym == nil || c.Fn == nil {
			continue
		}
		k := lt.Name.Sym
		set, call, result := scopeRunSite(b, i, k)
		if set == nil || !scopeUsedOnce(b, k) {
			continue
		}
		if repl := l.nativeScope(c, call, result, k); repl != nil {
			lt.Name.Typ = c.Typ
			set(repl)
		}
	}
}

// scopeRunSite finds the Scope.run call applying k after let i: a
// later let's value or the tail. set replaces it.
func scopeRunSite(b *core.Body, i int, k *resolve.Symbol) (set func(core.Expr), call *core.CallGo, result bool) {
	match := func(e core.Expr) (*core.CallGo, bool) {
		c, ok := e.(*core.CallGo)
		if !ok || len(c.Args) != 1 {
			return nil, false
		}
		run, res, _ := scopeCallee(c)
		if v, ok := c.Args[0].(*core.Var); !run || !ok || v.Sym != k {
			return nil, false
		}
		return c, res
	}
	for _, lt := range b.Lets[i+1:] {
		if c, res := match(lt.X); c != nil {
			return func(e core.Expr) { lt.X = e }, c, res
		}
	}
	if c, res := match(b.Tail); c != nil {
		return func(e core.Expr) { b.Tail = e }, c, res
	}
	return nil, nil, false
}

func scopeUsedOnce(b *core.Body, k *resolve.Symbol) bool {
	n := 0
	eachVar(b, func(v *core.Var, _ bool) {
		if v.Sym == k {
			n++
		}
	})
	return n == 1
}

// scopeReg is one registration found in a Scope body's own function.
type scopeReg struct {
	set  func(core.Expr)
	call *core.CallGo
	kind core.DeferKind
}

// nativeScope rewrites c's Func into a native-defer Scope body and
// returns the expression replacing call, or nil when the body does
// not qualify.
func (l *lowerer) nativeScope(c *core.Closure, call *core.CallGo, result bool, k *resolve.Symbol) core.Expr {
	fn := c.Fn
	if fn.Scope != nil || fn.TailLoop || fn.ConsLoop || len(fn.Params) != 1 ||
		fn.Params[0].Sym == nil || fn.Sig == nil || len(fn.Sig.Params) != 1 ||
		!types.Equal(call.Typ, fn.Sig.Ret) {
		return nil
	}
	s := fn.Params[0].Sym
	var regs []scopeReg
	escapes := false
	isReg := func(e core.Expr) (*core.CallGo, core.DeferKind) {
		r, ok := e.(*core.CallGo)
		if !ok || len(r.Args) != 2 {
			return nil, 0
		}
		_, _, kind := scopeCallee(r)
		if v, ok := r.Args[0].(*core.Var); kind == 0 || !ok || v.Sym != s {
			return nil, 0
		}
		return r, kind
	}
	note := func(e core.Expr) {
		switch e := e.(type) {
		case *core.EscapeResult:
			escapes = escapes || e.To == fn
		case *core.EscapeCall:
			escapes = escapes || e.To == fn
		}
	}
	scopeOwnBodies(fn.Body, func(b *core.Body) {
		for _, lt := range b.Lets {
			note(lt.X)
			if r, kind := isReg(lt.X); r != nil && (lt.Name.Discard || lt.Name.Sym == nil) {
				regs = append(regs, scopeReg{set: func(e core.Expr) { lt.X = e }, call: r, kind: kind})
			}
		}
		note(b.Tail)
		if r, kind := isReg(b.Tail); r != nil {
			regs = append(regs, scopeReg{set: func(e core.Expr) { b.Tail = e }, call: r, kind: kind})
		}
	})
	uses, captured := 0, false
	eachVar(fn.Body, func(v *core.Var, capt bool) {
		if v.Sym == s {
			uses++
			captured = captured || capt
		}
	})
	if captured || uses != len(regs) {
		return nil
	}
	info := &core.ScopeInfo{Result: result}
	for _, r := range regs {
		switch r.kind {
		case core.DeferAlways:
		case core.DeferErr:
			info.Errs = true
		case core.DeferOnErr:
			info.OnErr = true
		default:
			panic("unreachable")
		}
	}
	// A plain Scope.run body that a `try` leaves early returns
	// normally, which the finished flag would read as failure.
	if info.OnErr && !result && escapes {
		return nil
	}
	for _, r := range regs {
		r.set(&core.Defer{
			Base: core.Base{Sp: r.call.Sp, Typ: types.Unit},
			Kind: r.kind, F: r.call.Args[1], To: fn,
		})
	}
	sig := &types.Func{Params: []types.FuncParam{{Type: types.Unit}}, Ret: fn.Sig.Ret}
	fn.Params = []*core.Bind{{Sp: fn.Params[0].Sp, Typ: types.Unit, Discard: true}}
	fn.Sig = sig
	fn.Scope = info
	c.Typ = sig
	return &core.CallFunc{
		Base: core.Base{Sp: call.Sp, Typ: call.Typ},
		Fn:   &core.Var{Base: core.Base{Sp: call.Args[0].Span(), Typ: sig}, Sym: k},
		Args: []core.Atom{l.litUnit(call.Sp)},
	}
}

// scopeOwnBodies yields b and the bodies nested in it that run in
// the same Go function — if/match/select arms and loop bodies, not
// closures, Go adapters or go{} blocks.
func scopeOwnBodies(b *core.Body, f func(*core.Body)) {
	var body func(b *core.Body)
	var tree func(t core.DTree)
	expr := func(e core.Expr) {
		switch e := e.(type) {
		case *core.If:
			body(e.Then)
			body(e.Else)
		case *core.Match:
			tree(e.Tree)
		case *core.SelectExpr:
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
		case *core.ForRange:
			body(e.Body)
		case *core.Closure, *core.GoFunc, *core.Spawn, *core.Uncurry,
			*core.Binary, *core.CallBuiltin, *core.CallDirect, *core.Defer,
			*core.CallFunc, *core.CallGo, *core.Conv, *core.CtorApp,
			*core.CtorRef, *core.Equal, *core.ErrorFromGo, *core.EscapeCall,
			*core.EscapeResult, *core.FieldGet, *core.FieldSet, *core.FuncRef, *core.GoFuncRef,
			*core.ListNew, *core.Lit, *core.OptionFromGo, *core.RecordNew,
			*core.RecordUpdate, *core.ResultFromGo, *core.TailCall,
			*core.TupleFromGo, *core.TupleNew, *core.Unary, *core.Var:
		default:
			panic("unreachable")
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
	body = func(b *core.Body) {
		if b == nil {
			return
		}
		f(b)
		for _, lt := range b.Lets {
			expr(lt.X)
		}
		expr(b.Tail)
	}
	body(b)
}
