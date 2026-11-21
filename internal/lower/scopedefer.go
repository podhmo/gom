package lower

// SC-3: lower eligible `Scope.run` / `Scope.run_result` calls to
// native Go `defer` (docs/decisions/2026-11-22-sc3-native-defer.md).
//
// A scope body converts when every use of its scope handle is a
// `Scope.defer`/`Scope.defer_err`/`Scope.on_err` registration inside
// the same emitted Go function — `defer` keeps the runtime stack's
// LIFO order and additionally covers panics. Any other use — a
// nested closure, a `go {}` body, a non-registration call — keeps
// the SC-1 runtime scope for that call.
//
// The call site keeps creating a scope (`rt.NewScope`) so the
// body's parameter still has a value, but the close machinery is
// the body's own defers — the run call becomes an ordinary call to
// the body function. Result-observing registrations (`defer_err`
// and `on_err` over a `run_result` scope) name the body function's
// result through its Body's ResBind.

import (
	"slices"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// nativeScopeRuns runs the SC-3 lowering over a lowered file.
func (l *lowerer) nativeScopeRuns(out *core.File) {
	p := &scopeDeferPass{
		l:   l,
		fns: map[*resolve.Symbol]*core.Func{},
	}
	for _, d := range out.Decls {
		if fd, ok := d.(*core.FuncDecl); ok && fd.Fn != nil {
			p.fns[fd.Sym] = fd.Fn
		}
	}
	for _, d := range out.Decls {
		switch d := d.(type) {
		case *core.FuncDecl:
			if d.Fn != nil {
				p.body(d.Fn.Body, nil)
			}
		case *core.VarDecl:
			if d.Body != nil {
				p.body(d.Body, nil)
			}
		case *core.TestDecl:
			if d.Fn != nil {
				p.body(d.Fn.Body, nil)
			}
		}
	}
}

type scopeDeferPass struct {
	l   *lowerer
	fns map[*resolve.Symbol]*core.Func
}

// scopeCtx maps the let-bound symbols visible at the body under
// walk to their bound expressions, so a named body argument of a
// scope runner resolves like the inline lambda.
type scopeCtx struct {
	lets map[*resolve.Symbol]core.Expr
}

func (c *scopeCtx) child() *scopeCtx {
	lets := make(map[*resolve.Symbol]core.Expr, len(c.lets))
	if c != nil {
		for s, e := range c.lets {
			lets[s] = e
		}
	}
	return &scopeCtx{lets: lets}
}

// body scans lets and the tail for runner calls, then recurses into
// the expressions' nested bodies.
func (p *scopeDeferPass) body(b *core.Body, ctx *scopeCtx) {
	if ctx == nil {
		ctx = &scopeCtx{}
	}
	ctx = ctx.child()
	for i := 0; i < len(b.Lets); i++ {
		lt := b.Lets[i]
		if lt.Name != nil && lt.Name.Sym != nil {
			ctx.lets[lt.Name.Sym] = lt.X
		}
		if p.tryRun(lt.X, b, i, ctx) {
			continue
		}
		p.expr(lt.X, ctx)
	}
	if b.Tail != nil {
		if !p.tryRun(b.Tail, b, -1, ctx) {
			p.expr(b.Tail, ctx)
		}
	}
}

// expr descends into e's nested bodies — inline bodies keep ctx;
// closures and spawned bodies begin a new emitted function.
func (p *scopeDeferPass) expr(e core.Expr, ctx *scopeCtx) {
	switch x := e.(type) {
	case *core.If:
		p.body(x.Then, ctx)
		if x.Else != nil {
			p.body(x.Else, ctx)
		}
	case *core.Match:
		p.dtree(x.Tree, ctx)
	case *core.SelectExpr:
		for _, a := range x.Arms {
			switch a := a.(type) {
			case *core.SelRecv:
				p.body(a.Body, ctx)
			case *core.SelSend:
				p.body(a.Body, ctx)
			case *core.SelDefault:
				p.body(a.Body, ctx)
			default:
				panic("unreachable")
			}
		}
	case *core.ForRange:
		p.body(x.Body, ctx)
	case *core.Spawn:
		p.body(x.Body, ctx)
	case *core.Closure:
		p.body(x.Fn.Body, nil)
	case *core.Uncurry:
		p.expr(x.X, ctx)
	case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFuncRef, *core.FieldGet, *core.FieldSet, *core.GoFunc,
		*core.Unary, *core.Binary, *core.TupleNew, *core.ListNew,
		*core.RecordNew, *core.RecordUpdate, *core.CallDirect,
		*core.CallFunc, *core.CallGo, *core.CallBuiltin, *core.CtorApp,
		*core.TailCall, *core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.ErrorFromGo,
		*core.TupleFromGo, *core.Conv, *core.Defer, *core.Equal:
		// atoms and atom-only expressions hold no bodies.
	default:
		panic("unreachable")
	}
}

func (p *scopeDeferPass) dtree(t core.DTree, ctx *scopeCtx) {
	switch x := t.(type) {
	case *core.DSwitch:
		for _, c := range x.Cases {
			p.body(c.Body, ctx)
		}
		if x.Default != nil {
			p.dtree(x.Default, ctx)
		}
	case *core.DGuard:
		p.dtree(x.Then, ctx)
		p.dtree(x.Else, ctx)
	case *core.DLeaf:
		p.body(x.Body, ctx)
	case *core.DPanic:
	default:
		panic("unreachable")
	}
}

var scopeRegKinds = map[string]bool{
	"ScopeDefer":    true,
	"ScopeDeferErr": true,
	"ScopeOnErr":    true,
}

// reg is one registration site — a `Scope.defer`-family call at let
// index i of a body, or at that body's tail.
type reg struct {
	body   *core.Body
	index  int
	tail   bool
	call   *core.CallGo
	callee string
}

// tryRun rewrites a runner call at index i of b's lets (i < 0: the
// tail) when its body is eligible: the registrations inside the
// body become core.Defer nodes and the call becomes an ordinary
// `body(NewScope())` — the body's own defers are the close
// machinery. Returns false when e is not a runner or the scope
// does not qualify.
func (p *scopeDeferPass) tryRun(e core.Expr, b *core.Body, i int, ctx *scopeCtx) bool {
	call, ok := e.(*core.CallGo)
	if !ok || call.Callee == nil {
		return false
	}
	resultScope := false
	switch call.Callee.Name {
	case "ScopeRun":
	case "ScopeRunResult":
		resultScope = true
	default:
		return false
	}
	if len(call.Args) != 1 {
		return false
	}
	fn := p.bodyFn(call.Args[0], ctx)
	if fn == nil || len(fn.Params) != 1 || fn.Body == nil {
		return false
	}
	if fn.TailLoop || fn.ConsLoop {
		// The body runs inside a `for` — defers would hold one
		// iteration instead of closing per run.
		return false
	}
	s := fn.Params[0].Sym
	if s == nil {
		return false
	}
	regs := collectRegs(fn.Body, s)
	if regs == nil {
		return false
	}
	if len(regs) > 0 {
		var hasErr, hasOnErr bool
		for _, r := range regs {
			switch r.callee {
			case "ScopeDeferErr":
				hasErr = true
			case "ScopeOnErr":
				hasOnErr = true
			}
		}
		var res, errs *core.Bind
		if resultScope {
			if hasErr || hasOnErr {
				// Result-observing cleanups name the body
				// function's own result.
				res = p.l.tmpBind(call.Sp, call.Typ)
				fn.Body.ResBind = res
			}
			if hasErr {
				errs = p.l.tmpBind(call.Sp, types.GoSlice(types.GoError))
			}
		}
		for _, r := range regs {
			kind := core.DeferPlain
			switch r.callee {
			case "ScopeDeferErr":
				if resultScope {
					kind = core.DeferErr
				}
			case "ScopeOnErr":
				kind = core.DeferOnErr
			}
			d := &core.Defer{
				Base: core.Base{Sp: r.call.Sp, Typ: types.Unit},
				Kind: kind, F: r.call.Args[1], Res: res, Errs: errs,
			}
			if r.tail {
				r.body.Lets = append(r.body.Lets, &core.Let{
					Sp:   call.Sp,
					Name: p.l.discardBind(call.Sp, types.Unit),
					X:    d,
				})
				r.body.Tail = p.l.litUnit(call.Sp)
			} else {
				r.body.Lets[r.index].X = d
			}
		}
		if errs != nil {
			// The fold registers first so its defer runs last — the
			// scope's outermost close step.
			fold := &core.Defer{
				Base: core.Base{Sp: call.Sp, Typ: types.Unit},
				Kind: core.DeferErrFold, Res: res, Errs: errs,
			}
			fn.Body.Lets = append([]*core.Let{{
				Sp:   call.Sp,
				Name: p.l.discardBind(call.Sp, types.Unit),
				X:    fold,
			}}, fn.Body.Lets...)
		}
	}

	// The run call becomes an ordinary `body(NewScope())` — the
	// body's own defers are its close machinery.
	s2 := p.l.tmpBind(call.Sp, types.Scope)
	mk := &core.CallGo{
		Sp: call.Sp, Typ: types.Scope,
		Callee: &resolve.Symbol{
			Name: "NewScope", Kind: resolve.KindFunc, IsGo: true,
			Package: "github.com/podhmo/gom/rt",
		},
		Arity:   0,
		Results: core.GoResultsOne,
	}
	if i < 0 {
		b.Lets = append(b.Lets, &core.Let{Sp: call.Sp, Name: s2, X: mk})
		b.Tail = p.invokeRun(call, s2)
		return true
	}
	b.Lets[i].X = p.invokeRun(call, s2)
	b.Lets = slices.Insert(b.Lets, i, &core.Let{Sp: call.Sp, Name: s2, X: mk})
	return true
}

// invokeRun is the rewritten run site `body(s)` — a direct call for
// a declared body, an indirect call for a let-bound one.
func (p *scopeDeferPass) invokeRun(call *core.CallGo, s2 *core.Bind) core.Expr {
	sarg := varOf(call.Sp, s2)
	switch a := call.Args[0].(type) {
	case *core.FuncRef:
		return &core.CallDirect{
			Sp: call.Sp, Typ: call.Typ,
			Callee: a.Sym, TypeArgs: a.TypeArgs,
			Args: []core.Atom{sarg}, Arity: 1,
		}
	case *core.Var:
		return &core.CallFunc{
			Sp: call.Sp, Typ: call.Typ,
			Fn: a, Args: []core.Atom{sarg}, FlatArity: 1,
		}
	default:
		// bodyFn accepted only Var and FuncRef arguments.
		panic("unreachable")
	}
}

// bodyFn resolves a runner's body argument to its function — a
// let-bound closure or a declared function.
func (p *scopeDeferPass) bodyFn(a core.Atom, ctx *scopeCtx) *core.Func {
	switch a := a.(type) {
	case *core.Var:
		if x, ok := ctx.lets[a.Sym]; ok {
			switch x := x.(type) {
			case *core.Closure:
				return x.Fn
			case *core.FuncRef:
				return p.fns[x.Sym]
			}
		}
		if fd, ok := p.fns[a.Sym]; ok {
			return fd
		}
	case *core.FuncRef:
		if fd, ok := p.fns[a.Sym]; ok {
			return fd
		}
	case *core.Lit, *core.CtorRef, *core.GoFuncRef, *core.FieldGet:
	default:
		panic("unreachable")
	}
	return nil
}

// collectRegs gathers the scope handle's registration sites inside
// a scope body. Eligibility: the body's total uses of s (counted
// through nested closures, where the walker still sees atoms)
// must be exactly its registration arg0 uses — a registration
// trapped inside a nested closure would defer for the wrong
// function's lifetime, and any other use is opaque.
func collectRegs(b *core.Body, s *resolve.Symbol) []*reg {
	uses := 0
	walkTailrec(b, func(e core.Expr, inSpawn, inClosure bool) {
		if v, ok := e.(*core.Var); ok && v.Sym == s {
			uses++
		}
	})
	regs := []*reg{}
	var walk func(b *core.Body)
	var walkE func(e core.Expr)
	var walkT func(t core.DTree)
	walk = func(b *core.Body) {
		for i, l := range b.Lets {
			if !regAt(b, i, false, l.X, s, &regs) {
				walkE(l.X)
			}
		}
		if b.Tail != nil && !regAt(b, -1, true, b.Tail, s, &regs) {
			walkE(b.Tail)
		}
	}
	walkE = func(e core.Expr) {
		switch x := e.(type) {
		case *core.If:
			walk(x.Then)
			if x.Else != nil {
				walk(x.Else)
			}
		case *core.Match:
			walkT(x.Tree)
		case *core.SelectExpr:
			for _, a := range x.Arms {
				switch a := a.(type) {
				case *core.SelRecv:
					walk(a.Body)
				case *core.SelSend:
					walk(a.Body)
				case *core.SelDefault:
					walk(a.Body)
				default:
					panic("unreachable")
				}
			}
		case *core.ForRange:
			walk(x.Body)
		case *core.Uncurry:
			walkE(x.X)
		case *core.Closure, *core.Spawn, *core.GoFunc:
			// New emitted function — uses inside are counted by
			// walkTailrec and disqualify the scope.
		case *core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
			*core.GoFuncRef, *core.FieldGet, *core.FieldSet,
			*core.Unary, *core.Binary, *core.TupleNew, *core.ListNew,
			*core.RecordNew, *core.RecordUpdate, *core.CallDirect,
			*core.CallFunc, *core.CallGo, *core.CallBuiltin,
			*core.CtorApp, *core.TailCall, *core.EscapeResult,
			*core.EscapeCall, *core.ResultFromGo, *core.OptionFromGo,
			*core.ErrorFromGo, *core.TupleFromGo, *core.Conv,
			*core.Defer, *core.Equal:
		default:
			panic("unreachable")
		}
	}
	walkT = func(t core.DTree) {
		switch x := t.(type) {
		case *core.DSwitch:
			for _, c := range x.Cases {
				walk(c.Body)
			}
			if x.Default != nil {
				walkT(x.Default)
			}
		case *core.DGuard:
			walkT(x.Then)
			walkT(x.Else)
		case *core.DLeaf:
			walk(x.Body)
		case *core.DPanic:
		default:
			panic("unreachable")
		}
	}
	walk(b)
	if len(regs) != uses {
		return nil
	}
	return regs
}

// regAt records a registration at (b, i) — or b's tail when tail —
// when e is a `Scope.*` registration of s. s must be exactly the
// first argument.
func regAt(b *core.Body, i int, tail bool, e core.Expr, s *resolve.Symbol, regs *[]*reg) bool {
	c, ok := e.(*core.CallGo)
	if !ok || c.Callee == nil || !scopeRegKinds[c.Callee.Name] {
		return false
	}
	if len(c.Args) != 2 {
		return false
	}
	v, ok := c.Args[0].(*core.Var)
	if !ok || v.Sym != s {
		return false
	}
	*regs = append(*regs, &reg{body: b, index: i, tail: tail, call: c, callee: c.Callee.Name})
	return true
}
