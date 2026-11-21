package codegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// This file is the destination-passing emitter (docs/design.md
// section 4.3): every Core IR expression is emitted under a
// destination that tells it where its value goes — Return for a
// body's tail, Assign for a declared variable, Discard for a value
// that is evaluated and dropped. Complex expressions emit statements
// whose branch bodies inherit the destination, so no temporaries and
// no IIFEs appear (the design's var-decl IIFE is the single
// exception, in decl.go).

// destKind selects where an expression's value goes.
type destKind int

const (
	dReturn  destKind = iota + 1 // the body's tail result
	dAssign                      // into a `var` declared in an outer block
	dDeclare                     // the node declares the name itself — `let`'s selfDeclares path only
	dDiscard                     // evaluate and drop
	dConsBuf                     // a ConsLoop function's build buffer — `buf = append(buf, tail...)`
)

// dest is the destination context. name is set for dAssign, dDeclare
// and dConsBuf (the buffer variable).
type dest struct {
	kind destKind
	name string
}

var (
	ret     = dest{kind: dReturn}
	discard = dest{kind: dDiscard}
)

func assign(name string) dest  { return dest{kind: dAssign, name: name} }
func declare(name string) dest { return dest{kind: dDeclare, name: name} }

// consBuf is the destination a ConsLoop body's tails write to — the
// result list accumulates in the named buffer variable (docs/design.md
// section 4.5).
func consBuf(name string) dest { return dest{kind: dConsBuf, name: name} }

// body emits an ordered binding sequence then the tail under d.
func (g *gen) body(b *core.Body, d dest) {
	for i, l := range b.Lets {
		if !l.Name.Discard && l.Name.Sym != nil && bodyUsesFrom(b, l.Name.Sym, i+1) {
			g.let(l)
			continue
		}
		// `let _ =` or a named bind no later expression references:
		// Go rejects declared-and-unused variables, so the value is
		// evaluated for its effect only (walk.go symbol use).
		g.emit(l.X, discard)
	}
	g.emit(b.Tail, d)
}

// let emits one `name = x` binding. A single-expression x becomes
// `name := x`; a statement-level x declares the variable first and
// assigns under the Assign destination so the statements bind it.
func (g *gen) let(l *core.Let) {
	if l.Name.Discard {
		// emit writes the node's own //line — the let's span would
		// only duplicate it.
		g.emit(l.X, discard)
		return
	}
	name := g.bindName(l.Name)
	if isSimpleExpr(l.X) {
		g.line(l.Sp)
		g.raw(name + " := ")
		// `:=` re-infers the variable's Go type from the RHS, which
		// for two RHS shapes is narrower than the binder's declared
		// type — the spelling must then appear explicitly as a
		// conversion (letConv):
		//   - an untyped constant (`x := 6 * 7`) defaults to `int`,
		//     not the `int64` Gom Int maps to, and later int64 call
		//     sites reject it;
		//   - a sealed-ADT constructor literal binds the concrete
		//     variant struct, not the ADT interface — a `==` or a
		//     type switch over the variable then does not compile.
		if t := g.letConv(l.Name.Typ, l.X); t != nil {
			g.typ(t)
			g.raw("(")
			g.exprValue(l.X)
			g.raw(")")
		} else {
			g.exprValue(l.X)
		}
		g.nl()
		return
	}
	if selfDeclares(l.X) {
		// `n, err := f()` — the node's own //line covers it. The
		// fused `:=` is legal only on this direct-let path: inside
		// a nested block it would shadow rather than bind (see
		// escapeCall).
		g.emit(l.X, declare(name))
		return
	}
	g.line(l.Sp)
	g.raw("var " + name + " ")
	g.typ(g.goType(l.Name.Typ))
	g.nl()
	g.emit(l.X, assign(name))
}

// selfDeclares reports whether a statement-level let value declares
// its binder with `:=` itself — the fused `(v, err)` escape is
// `n, err := f()`, so a `var n` would be a dead declaration. Only a
// let whose RHS *is* the node qualifies: under dAssign the same node
// may run inside a nested block, where `:=` would shadow the outer
// `var` instead of assigning it.
func selfDeclares(e core.Expr) bool {
	ec, ok := e.(*core.EscapeCall)
	return ok && ec.Call.Results == core.GoResultsErr
}

// emit emits expression e under destination d. The statement-level
// nodes each take a dedicated emitter; every other variant is a
// single-expression node handled by valueExpr — the two groups are
// spelled out so the sumtype check verifies the split.
func (g *gen) emit(e core.Expr, d dest) {
	switch e := e.(type) {
	case *core.If:
		g.ifExpr(e, d)
	case *core.Match:
		g.dtree(e.Tree, d)
	case *core.SelectExpr:
		g.selExpr(e, d)
	case *core.Spawn:
		g.spawnExpr(e, d)
	case *core.Defer:
		g.deferStmt(e)
	case *core.ForRange:
		g.forExpr(e, d)
	case *core.TailCall:
		g.tailCall(e, d)
	case *core.EscapeResult:
		g.escapeResult(e, d)
	case *core.EscapeCall:
		g.escapeCall(e, d)
	case *core.ResultFromGo:
		g.resultFromGo(e, d)
	case *core.OptionFromGo:
		g.optionFromGo(e, d)
	case *core.TupleFromGo:
		g.tupleFromGo(e, d)
	case *core.ErrorFromGo:
		g.errorFromGo(e, d)
	case *core.RecordUpdate:
		g.recordUpdate(e, d)
	case *core.FieldSet:
		g.fieldSet(e, d)
	case *core.Closure,
		*core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFunc, *core.GoFuncRef, *core.FieldGet,
		*core.Unary, *core.Binary, *core.Equal,
		*core.CallDirect, *core.CallFunc, *core.CallGo, *core.CallBuiltin,
		*core.CtorApp, *core.TupleNew, *core.ListNew, *core.RecordNew,
		*core.Conv, *core.Uncurry:
		g.valueExpr(e, d)
	default:
		panic("unreachable")
	}
}

// valueExpr emits a single-expression node under its destination.
// Closure takes this path too — its function literal is one
// expression whose body is emitted inside.
func (g *gen) valueExpr(e core.Expr, d dest) {
	g.line(e.Span())
	switch d.kind {
	case dReturn:
		g.raw("return ")
		g.exprValue(e)
	case dAssign:
		g.raw(d.name + " = ")
		g.exprValue(e)
	case dConsBuf:
		g.raw(d.name + " = append(" + d.name + ", ")
		g.exprValue(e)
		g.raw("...)")
	case dDiscard:
		// A call discards its results cleanly as a bare statement —
		// needed for zero- and multi-result Go calls, which `_ =`
		// cannot bind. A Conv renders the same `T(x)` syntax but is
		// a conversion to Go, so it still needs `_ =`. A Closure's
		// literal is not a CallExpr either, so `_ = func...` it is.
		if c, ok := e.(*core.Closure); ok {
			g.raw("_ = ")
			g.funcLit(c.Fn)
			break
		}
		if c, ok := e.(*core.GoFunc); ok {
			g.raw("_ = ")
			g.goFuncLit(c)
			break
		}
		if c, ok := e.(*core.Uncurry); ok {
			g.raw("_ = ")
			g.uncurryLit(c)
			break
		}
		x := g.simpleExpr(e)
		if _, ok := x.(*ast.CallExpr); ok && !emitsConversion(e) {
			g.expr(x)
		} else {
			g.raw("_ = ")
			g.expr(x)
		}
	default:
		panic("unreachable")
	}
	g.nl()
}

// emitsConversion reports whether a discard-position expression
// renders as a `T(x)` conversion — valid in expression position but
// rejected as a statement, so it still needs `_ =` even though it
// prints as a CallExpr. An unwrapping one-field-record FieldGet
// prints the same `T(x)` (`int64(u)` alone is "not used"), and a Lit
// checked at a named Go type joins the explicit Conv here.
func emitsConversion(e core.Expr) bool {
	if _, ok := e.(*core.Conv); ok {
		return true
	}
	if f, ok := e.(*core.FieldGet); ok && f.Unwrap {
		return true
	}
	l, ok := e.(*core.Lit)
	if !ok {
		return false
	}
	_, named := types.Chase(l.Type()).(*types.Go)
	return named
}

// exprValue writes the expression part — a function literal for a
// Closure, the single expression otherwise.
func (g *gen) exprValue(e core.Expr) {
	if c, ok := e.(*core.Closure); ok {
		g.funcLit(c.Fn)
		return
	}
	if c, ok := e.(*core.GoFunc); ok {
		g.goFuncLit(c)
		return
	}
	if c, ok := e.(*core.Uncurry); ok {
		g.uncurryLit(c)
		return
	}
	g.expr(g.simpleExpr(e))
}

// uncurryLit emits the flat `func(p0 T0, ..., pk-1 Tk-1) R { ... }`
// an Uncurry carries. A FuncRef or CtorRef whose declared arity is
// the slot's arity already emits the flat face — the reference
// stands verbatim; any other arity adapts through a direct call
// (the codegen under-application eta covers a shorter source).
// A Closure flattens its one-level-per-arrow chain into the single
// literal — params and per-level Lets merge, evaluation order is
// unchanged since the callee applies the flat argument in one shot.
// Every other atom wraps canonically: `x(p0)(p1)...`; a GoFuncRef,
// a flat Go function, takes the arguments at once.
func (g *gen) uncurryLit(e *core.Uncurry) {
	g.line(e.Sp)
	ps, ret := flatArrow(e.Typ)
	var binds []*core.Bind
	var lets []*core.Let
	var tail core.Expr

	bindOf := func(x core.Expr) core.Atom {
		if a, ok := x.(core.Atom); ok {
			return a
		}
		tb := &core.Bind{Sp: e.Sp, Sym: &resolve.Symbol{
			Name: fmt.Sprintf("u%d", len(lets)), Kind: resolve.KindVar}, Typ: x.Type()}
		lets = append(lets, &core.Let{Sp: e.Sp, Name: tb, X: x})
		return &core.Var{Sp: e.Sp, Typ: x.Type(), Sym: tb.Sym}
	}

	synth := func(j int) *core.Bind {
		return &core.Bind{Sp: e.Sp, Sym: &resolve.Symbol{
			Name: fmt.Sprintf("p%d", j), Kind: resolve.KindVar}, Typ: ps[min(j, len(ps)-1)].Type}
	}
	pvOf := func(b *core.Bind) core.Atom {
		return &core.Var{Sp: e.Sp, Typ: b.Typ, Sym: b.Sym}
	}

	switch x := e.X.(type) {
	case *core.FuncRef:
		// Verbatim only when the declaration's emitted signature IS
		// the flat slot shape — same arity AND no flat (≥2-arrow)
		// declared params of its own. A decl taking binary
		// callbacks (`func f_(g func(A,B)C, ...)`) is not the
		// canonical-slot value (`func(g func(A)func(B)C, ...)`) the
		// flat literal hands it, so its params need per-slot
		// adaptation like any call site.
		fps, _ := flatArrow(x.Type())
		// The flat convention is positional on the *declared* arrow
		// chain — FuncRef.FlatParams — not the instantiated
		// reference type: substitution can widen a declared 1-arrow
		// parameter into a >= 2-arrow value, which must not turn a
		// canonical slot flat.
		declFlat := func(i int) int {
			if i < len(x.FlatParams) {
				return x.FlatParams[i]
			}
			if i < len(fps) {
				return flatArity(fps[i].Type)
			}
			return 0
		}
		flatParams := false
		for i := 0; i < x.Arity; i++ {
			if declFlat(i) >= 2 {
				flatParams = true
			}
		}
		if x.Arity == e.Arity && !flatParams {
			g.expr(g.qualRef(x.Sym, x.TypeArgs))
			return
		}
		for len(binds) < e.Arity {
			binds = append(binds, synth(len(binds)))
		}
		n := min(e.Arity, x.Arity)
		var args []core.Atom
		for i, b := range binds[:n] {
			var arg core.Atom = pvOf(b)
			if fa := declFlat(i); fa >= 2 {
				arg = bindOf(&core.Uncurry{Base: core.Base{Sp: e.Sp, Typ: b.Typ}, Arity: fa, X: arg})
			}
			args = append(args, arg)
		}
		if x.Arity > e.Arity {
			// Under-application: the flat literal's result is the
			// canonical chain saturating the rest of the call.
			var qs []*core.Bind
			for i := e.Arity; i < x.Arity; i++ {
				qt := ret
				if i < len(fps) {
					qt = fps[i].Type
				}
				qs = append(qs, &core.Bind{Sp: e.Sp, Sym: &resolve.Symbol{
					Name: fmt.Sprintf("q%d", i), Kind: resolve.KindVar}, Typ: qt})
				var arg core.Atom = pvOf(qs[len(qs)-1])
				if fa := declFlat(i); fa >= 2 {
					arg = bindOf(&core.Uncurry{Base: core.Base{Sp: e.Sp, Typ: qt}, Arity: fa, X: arg})
				}
				args = append(args, arg)
			}
			var call core.Expr = &core.CallDirect{Base: core.Base{Sp: x.Sp, Typ: ret}, Callee: x.Sym,
				Args: args, Arity: x.Arity, TypeArgs: x.TypeArgs}
			for i := len(qs) - 1; i >= 0; i-- {
				call = &core.Closure{Base: core.Base{Sp: e.Sp, Typ: ret},
					Fn: &core.Func{Sp: e.Sp, Params: []*core.Bind{qs[i]},
						Body: &core.Body{Tail: call}}}
			}
			tail = call
		} else {
			var call core.Expr = &core.CallDirect{Base: core.Base{Sp: x.Sp, Typ: ret}, Callee: x.Sym,
				Args: args, Arity: x.Arity, TypeArgs: x.TypeArgs}
			for i := n; i < e.Arity; i++ {
				a := bindOf(call)
				call = &core.CallFunc{Base: core.Base{Sp: e.Sp, Typ: ret}, Fn: a,
					Args: []core.Atom{pvOf(binds[i])}}
			}
			tail = call
		}
	case *core.CtorRef:
		// No verbatim shortcut: a ctor reference emits the
		// canonical one-func-per-field chain (ctorRef), never a
		// flat literal — always emit the adapter shape.
		cArity := len(x.Fields)
		for len(binds) < e.Arity {
			binds = append(binds, synth(len(binds)))
		}
		n := min(e.Arity, cArity)
		var args []core.Atom
		for _, b := range binds[:n] {
			args = append(args, pvOf(b))
		}
		if cArity > e.Arity {
			var qs []*core.Bind
			for i := e.Arity; i < cArity; i++ {
				qs = append(qs, &core.Bind{Sp: e.Sp, Sym: &resolve.Symbol{
					Name: fmt.Sprintf("q%d", i), Kind: resolve.KindVar}, Typ: ret})
				args = append(args, pvOf(qs[len(qs)-1]))
			}
			var call core.Expr = &core.CtorApp{Base: core.Base{Sp: x.Sp, Typ: ret}, Ctor: x.Sym,
				Args: args, Arity: cArity, Fields: x.Fields}
			for i := len(qs) - 1; i >= 0; i-- {
				call = &core.Closure{Base: core.Base{Sp: e.Sp, Typ: ret},
					Fn: &core.Func{Sp: e.Sp, Params: []*core.Bind{qs[i]},
						Body: &core.Body{Tail: call}}}
			}
			tail = call
		} else {
			var call core.Expr = &core.CtorApp{Base: core.Base{Sp: x.Sp, Typ: ret}, Ctor: x.Sym,
				Args: args, Arity: cArity, Fields: x.Fields}
			for i := n; i < e.Arity; i++ {
				a := bindOf(call)
				call = &core.CallFunc{Base: core.Base{Sp: e.Sp, Typ: ret}, Fn: a,
					Args: []core.Atom{pvOf(binds[i])}}
			}
			tail = call
		}
	case *core.Closure:
		cur := x
		for cur != nil && len(binds) < e.Arity {
			binds = append(binds, cur.Fn.Params...)
			lets = append(lets, cur.Fn.Body.Lets...)
			next, ok := cur.Fn.Body.Tail.(*core.Closure)
			if !ok {
				tail = cur.Fn.Body.Tail
				cur = nil
				break
			}
			cur = next
		}
		if cur != nil {
			tail = cur
		}
		for len(binds) < e.Arity {
			b := synth(len(binds))
			binds = append(binds, b)
			if tail != nil {
				a := bindOf(tail)
				tail = &core.CallFunc{Base: core.Base{Sp: e.Sp, Typ: ret}, Fn: a, Args: []core.Atom{
					&core.Var{Sp: e.Sp, Typ: b.Typ, Sym: b.Sym}}}
			}
		}
	case *core.GoFuncRef:
		// A GoFuncRef taking the flat call.
		for len(binds) < e.Arity {
			binds = append(binds, synth(len(binds)))
		}
		args := make([]core.Atom, len(binds))
		for i, b := range binds {
			args[i] = &core.Var{Sp: e.Sp, Typ: b.Typ, Sym: b.Sym}
		}
		tail = &core.CallFunc{Base: core.Base{Sp: e.Sp, Typ: ret}, Fn: x, Args: args, FlatArity: len(args)}
	case *core.Var, *core.Lit, *core.FieldGet, *core.GoFunc, *core.Uncurry,
		*core.Unary, *core.Binary, *core.Equal, *core.If, *core.Match,
		*core.SelectExpr, *core.Spawn, *core.ForRange,
		*core.TupleNew, *core.ListNew, *core.RecordNew, *core.RecordUpdate,
		*core.CallDirect, *core.CallFunc, *core.CallGo, *core.CallBuiltin,
		*core.CtorApp, *core.TailCall, *core.EscapeResult, *core.EscapeCall,
		*core.ResultFromGo, *core.OptionFromGo, *core.ErrorFromGo,
		*core.TupleFromGo, *core.Conv, *core.FieldSet:
		// The generic adapter over a canonical atom.
		tail = e.X
		for len(binds) < e.Arity {
			b := synth(len(binds))
			binds = append(binds, b)
			a := bindOf(tail)
			tail = &core.CallFunc{Base: core.Base{Sp: e.Sp, Typ: ret}, Fn: a, Args: []core.Atom{
				&core.Var{Sp: e.Sp, Typ: b.Typ, Sym: b.Sym}}}
		}
	default:
		panic("unreachable")
	}
	if tail == nil {
		// Defensive — the lowerer only emits an Uncurry whose Arity
		// the source chain can cover.
		panic("unreachable")
	}
	fn := &core.Func{Sp: e.Sp, Sig: &types.Func{Params: ps[:min(e.Arity, len(ps))], Ret: ret},
		Params: binds, Body: &core.Body{Lets: lets, Tail: tail}}
	g.funcLit(fn)
}

// writeDest writes an already-built expression to a destination.
func (g *gen) writeDest(d dest, x ast.Expr) {
	switch d.kind {
	case dReturn:
		g.raw("return ")
	case dAssign:
		g.raw(d.name + " = ")
	case dDiscard:
		g.raw("_ = ")
	case dConsBuf:
		g.raw(d.name + " = ")
		g.expr(&ast.CallExpr{Fun: ast.NewIdent("append"),
			Args:     []ast.Expr{ast.NewIdent(d.name), x},
			Ellipsis: token.Pos(1)})
		g.nl()
		return
	default:
		panic("unreachable")
	}
	g.expr(x)
	g.nl()
}

// unitValue writes `struct{}{}` to a non-discard destination — the
// value of statement-typed nodes (else-less if, spawn, for).
func (g *gen) unitValue(d dest) {
	switch d.kind {
	case dReturn:
		g.raw("return struct{}{}")
		g.nl()
	case dAssign:
		g.raw(d.name + " = struct{}{}")
		g.nl()
	case dDiscard:
	default:
		panic("unreachable")
	}
}

// ifExpr emits `if c { ... } else { ... }` with the destination
// propagated into the branch bodies. A missing else is legal only
// for a Unit result: the statement runs for effect and the Unit
// value is produced after it when a destination wants it.
func (g *gen) ifExpr(e *core.If, d dest) {
	g.line(e.Sp)
	g.raw("if ")
	g.expr(g.atom(e.Cond))
	g.open()
	g.body(e.Then, d)
	if e.Else != nil {
		g.midElse()
		g.body(e.Else, d)
		g.close()
		return
	}
	g.close()
	// else-less if produces Unit after the statement.
	g.unitValue(d)
}

// dtree emits a decision tree under destination d.
func (g *gen) dtree(t core.DTree, d dest) {
	switch t := t.(type) {
	case *core.DLeaf:
		g.body(t.Body, d)
	case *core.DPanic:
		g.line(t.Sp)
		g.raw("panic(" + g.pkgQual(rtPath) + "InvalidVariant(")
		g.expr(&ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(t.TypeName)})
		g.raw(", ")
		g.expr(g.atom(t.Subj))
		g.raw("))")
		g.nl()
	case *core.DGuard:
		g.line(t.Sp)
		g.raw("if ")
		g.expr(g.atom(t.Cond))
		g.open()
		g.dtree(t.Then, d)
		g.midElse()
		g.dtree(t.Else, d)
		g.close()
	case *core.DSwitch:
		g.dswitch(t, d)
	default:
		panic("unreachable")
	}
}

// dswitch emits the switch a DSwitch's kind fixes (docs/design.md
// section 4.2 — the discrimination mechanism comes from the node).
func (g *gen) dswitch(s *core.DSwitch, d dest) {
	g.line(s.Sp)
	switch s.Kind {
	case core.SwitchType:
		g.switchType(s, d)
	case core.SwitchTag:
		g.switchTag(s, d)
	case core.SwitchValue:
		g.switchValue(s, d)
	case core.SwitchList:
		g.switchList(s, d)
	default:
		panic("unreachable")
	}
}

// switchType emits a Go type switch over a sealed ADT. The guard
// variable `v` is bound only when a case needs the narrowed subject;
// each binding case aliases it to the case's name. The default is
// the validator-required DPanic (tree.go: a wildcard arm was already
// expanded over the uncovered variants).
func (g *gen) switchType(s *core.DSwitch, d dest) {
	guard := ""
	for _, c := range s.Cases {
		// The guard var exists only when some arm narrows-and-uses
		// its bind — an unused guard is a Go compile error.
		if len(c.Binds) > 0 && bindUsed(c.Binds[0], c.Body) {
			guard = g.fresh("v")
			break
		}
	}
	g.raw("switch ")
	if guard != "" {
		g.raw(guard + " := ")
	}
	g.expr(g.atom(s.Subj))
	g.raw(".(type)")
	g.open()
	for _, c := range s.Cases {
		g.dcaseType(c, guard, s, d)
	}
	g.ddefault(s, d)
	g.close()
}

// dcaseType emits `case Type_Ctor:` for a SwitchType arm, plus the
// narrowed-subject alias when the case binds one. The case type is
// the variant's struct type, instantiated with the subject's type
// arguments for a generic ADT.
func (g *gen) dcaseType(c core.DCase, guard string, s *core.DSwitch, d dest) {
	g.line(c.Sp)
	ct, ok := c.Test.(*core.DCtorTest)
	if !ok {
		panic("codegen: SwitchType case test is not a DCtorTest")
	}
	g.caseLabel(func() {
		g.raw("case ")
		g.expr(g.ctorGoType(ct.Ctor, s.Subj.Type()))
	})
	// A SwitchType case carries at most one bind — the narrowed
	// subject alias (tree.go).
	if len(c.Binds) == 1 && guard != "" && bindUsed(c.Binds[0], c.Body) {
		g.raw(g.bindName(c.Binds[0]) + " := " + guard)
		g.nl()
	}
	g.body(c.Body, d)
}

// switchTag emits `switch s.Tag()` over the flat2 Result
// representation: `case result.ResultTagOk:` binds `s.Value`,
// `case result.ResultTagErr:` binds `s.Error` (P0-4).
func (g *gen) switchTag(s *core.DSwitch, d dest) {
	g.raw("switch ")
	g.expr(g.atom(s.Subj))
	g.raw(".Tag()")
	g.open()
	for _, c := range s.Cases {
		g.dcaseTag(c, s.Subj, d)
	}
	g.ddefault(s, d)
	g.close()
}

func (g *gen) dcaseTag(c core.DCase, subj core.Atom, d dest) {
	g.line(c.Sp)
	ct, ok := c.Test.(*core.DCtorTest)
	if !ok {
		panic("codegen: SwitchTag case test is not a DCtorTest")
	}
	g.caseLabel(func() {
		g.raw("case " + g.pkgQual(resultPath) + "ResultTag" + exportName(ct.Ctor.Name))
	})
	for _, b := range c.Binds {
		if !bindUsed(b, c.Body) {
			continue
		}
		field := "Value"
		if ct.Ctor.Name == "Err" {
			field = "Error"
		}
		g.raw(g.bindName(b) + " := ")
		g.expr(&ast.SelectorExpr{X: g.atom(subj), Sel: ast.NewIdent(field)})
		g.nl()
	}
	g.body(c.Body, d)
}

// switchValue emits `switch subj` with literal cases.
func (g *gen) switchValue(s *core.DSwitch, d dest) {
	g.raw("switch ")
	g.expr(g.atom(s.Subj))
	g.open()
	for _, c := range s.Cases {
		g.dcaseValue(c, s.Subj, d)
	}
	g.ddefault(s, d)
	g.close()
}

func (g *gen) dcaseValue(c core.DCase, subj core.Atom, d dest) {
	g.line(c.Sp)
	lt, ok := c.Test.(*core.DLitTest)
	if !ok {
		panic("codegen: SwitchValue case test is not a DLitTest")
	}
	g.caseLabel(func() {
		// An `any` subject compares a boxed value, so the case
		// literal must carry its Gom type's rep — `int64(42)`, not
		// Go's untyped `int` default `42`, which never matches.
		if con, ok := types.Chase(subj.Type()).(*types.Con); ok &&
			con.Qual == "Go" && con.Name == "Any" && lt.Typ != nil {
			g.raw("case ")
			g.expr(&ast.CallExpr{
				Fun:  g.goType(lt.Typ),
				Args: []ast.Expr{ast.NewIdent(lt.Text)},
			})
			return
		}
		g.raw("case " + lt.Text)
	})
	g.body(c.Body, d)
}

// switchList emits the list-shape test `switch { case len(s) == 0:
// ... case len(s) != 0: ... }` — a nil slice is a valid empty list,
// so both shapes cover every value and the default is the
// unreachable DPanic filler (tree.go SwitchList).
func (g *gen) switchList(s *core.DSwitch, d dest) {
	g.raw("switch")
	g.open()
	for _, c := range s.Cases {
		g.dcaseList(c, s.Subj, d)
	}
	g.ddefault(s, d)
	g.close()
}

func (g *gen) dcaseList(c core.DCase, subj core.Atom, d dest) {
	g.line(c.Sp)
	lt, ok := c.Test.(*core.DListTest)
	if !ok {
		panic("codegen: SwitchList case test is not a DListTest")
	}
	g.caseLabel(func() {
		g.raw("case len(")
		g.expr(g.atom(subj))
		if lt.Cons {
			g.raw(") != 0")
		} else {
			g.raw(") == 0")
		}
	})
	if lt.Cons {
		for i, b := range c.Binds {
			if !bindUsed(b, c.Body) {
				continue
			}
			g.raw(g.bindName(b) + " := ")
			if i == 0 {
				g.expr(&ast.IndexExpr{X: g.atom(subj), Index: &ast.BasicLit{Kind: token.INT, Value: "0"}})
			} else {
				g.expr(&ast.SliceExpr{X: g.atom(subj), Low: &ast.BasicLit{Kind: token.INT, Value: "1"}})
			}
			g.nl()
		}
	}
	g.body(c.Body, d)
}

// ddefault emits the `default:` clause of a switch — always a tree
// (a DPanic for the kind-required arms, or the catch-all leaf for
// value/list switches).
func (g *gen) ddefault(s *core.DSwitch, d dest) {
	if s.Default == nil {
		return
	}
	g.caseLabel(func() { g.raw("default") })
	g.dtree(s.Default, d)
}

// selExpr emits a Go select. A receive arm's channel evaluation was
// already ordered by the enclosing body's bindings (expr.go
// SelectExpr); the `v, ok` pair becomes the arm's Option bind
// through option.FromGo.
func (g *gen) selExpr(e *core.SelectExpr, d dest) {
	g.line(e.Sp)
	g.raw("select")
	g.open()
	for _, a := range e.Arms {
		g.selArm(a, d)
	}
	g.close()
}

func (g *gen) selArm(a core.SelArm, d dest) {
	g.line(a.Span())
	switch a := a.(type) {
	case *core.SelRecv:
		if a.Bind == nil || a.Bind.Discard || !bindUsed(a.Bind, a.Body) {
			// receive-and-drop: the Option bind is unused
			g.caseLabel(func() {
				g.raw("case <-")
				g.expr(g.atom(a.Chan))
			})
			g.body(a.Body, d)
			return
		}
		v, ok := g.fresh("v"), g.fresh("ok")
		g.caseLabel(func() {
			g.raw("case " + v + ", " + ok + " := <-")
			g.expr(g.atom(a.Chan))
		})
		g.raw(g.bindName(a.Bind) + " := " + g.pkgQual(optionPath) + "FromGo(" + v + ", " + ok + ")")
		g.nl()
		g.body(a.Body, d)
	case *core.SelSend:
		g.caseLabel(func() {
			g.raw("case ")
			g.expr(g.atom(a.Chan))
			g.raw(" <- ")
			g.expr(g.atom(a.Value))
		})
		g.body(a.Body, d)
	case *core.SelDefault:
		g.caseLabel(func() { g.raw("default") })
		g.body(a.Body, d)
	default:
		panic("unreachable")
	}
}

// spawnExpr emits `go func() { ... }()` — the spawned body's result
// is discarded inside (validator: its type is Unit).
func (g *gen) spawnExpr(e *core.Spawn, d dest) {
	g.line(e.Sp)
	g.raw("go func()")
	g.open()
	g.body(e.Body, discard)
	g.closeInline()
	g.raw("()")
	g.nl()
	g.unitValue(d)
}

// forExpr emits the range loop ForKind fixes (expr.go): ForList is
// `for _, x := range iter`, ForChan `for x := range ch`.
func (g *gen) forExpr(e *core.ForRange, d dest) {
	g.line(e.Sp)
	used := bindUsed(e.Elem, e.Body)
	switch e.Kind {
	case core.ForList:
		if !used {
			g.raw("for range ")
		} else {
			g.raw("for _, " + g.bindName(e.Elem) + " := range ")
		}
	case core.ForChan:
		if !used {
			g.raw("for range ")
		} else {
			g.raw("for " + g.bindName(e.Elem) + " := range ")
		}
	default:
		panic("unreachable")
	}
	g.expr(g.atom(e.Iter))
	g.open()
	g.body(e.Body, discard)
	g.close()
	g.unitValue(d)
}

// tailCall emits reassign-and-continue inside the enclosing TailLoop
// `for` — `p0, p1 = a0, a1` then `continue` (design.md section 4.5).
// The multi-assignment evaluates arguments before rebinding, which is
// the semantics the node guarantees. A cons-prepend call — the
// `[h, .., f args]` shape a ConsLoop carries — first pushes its heads
// to the destination's build buffer: `buf = append(buf, h, ...)`.
//
// A pair `p = p` rebinds a parameter to itself — a no-op that
// `go vet` reports as self-assignment (and the compile canary's
// vet-equivalent checks flag the same way). Such pairs are dropped:
// a self-call passing every parameter unchanged emits the bare
// `continue`, and a mixed call keeps only the changing parameters.
func (g *gen) tailCall(e *core.TailCall, d dest) {
	g.line(e.Sp)
	if len(e.Prepend) > 0 {
		if d.kind != dConsBuf {
			panic("unreachable")
		}
		g.raw(d.name + " = append(" + d.name)
		for _, a := range e.Prepend {
			g.raw(", ")
			g.expr(g.atom(a))
		}
		g.raw(")")
		g.nl()
	}
	var lhs, rhs []ast.Expr
	for i, p := range e.To.Params {
		a := e.Args[i]
		if v, ok := a.(*core.Var); ok && v.Sym == p.Sym {
			continue // p = p — self-assignment, no effect
		}
		lhs = append(lhs, ast.NewIdent(g.bindName(p)))
		rhs = append(rhs, g.atom(a))
	}
	if len(lhs) != 0 {
		for i, l := range lhs {
			if i > 0 {
				g.raw(", ")
			}
			g.expr(l)
		}
		g.raw(" = ")
		for i, r := range rhs {
			if i > 0 {
				g.raw(", ")
			}
			g.expr(r)
		}
		g.nl()
	}
	g.raw("continue")
	g.nl()
}

// escapeResult emits the `try` / `|?>` unwrap on a Result value
// (P0-4): a Tag() switch whose Ok arm writes the payload to the
// destination and whose Err arm returns the boundary's Err —
// `result.Err[T, E](err)`, wrapped in an `rt.StepErr` frame when
// Wrap names a pipeline step (rendered text `step: message`, same
// wording as the old `fmt.Errorf("step: %w", err)`). Inside a test
// the Err arm fails the test instead.
func (g *gen) escapeResult(e *core.EscapeResult, d dest) {
	g.line(e.Sp)
	subj := g.atom(e.X)
	g.raw("switch ")
	g.expr(subj)
	g.raw(".Tag()")
	g.open()
	g.caseLabel(func() {
		g.raw("case " + g.pkgQual(resultPath) + "ResultTagOk")
	})
	g.writeDest(d, &ast.SelectorExpr{X: subj, Sel: ast.NewIdent("Value")})
	g.caseLabel(func() {
		g.raw("case " + g.pkgQual(resultPath) + "ResultTagErr")
	})
	g.escapeErr(e.Sp, &ast.SelectorExpr{X: subj, Sel: ast.NewIdent("Error")}, e.Wrap, e.To)
	g.caseLabel(func() { g.raw("default") })
	g.line(e.Sp)
	g.raw("panic(" + g.pkgQual(rtPath) + "InvalidVariant(" + strconv.Quote("result.Result") + ", ")
	g.expr(subj)
	g.raw("))")
	g.nl()
	g.close()
}

// escapeCall emits the fused `(value, err)` boundary check (design.md
// section 4.6): bind the call results, return the boundary's Err on
// failure, and deliver the value on success — no Result is
// materialized.
func (g *gen) escapeCall(e *core.EscapeCall, d dest) {
	g.line(e.Sp)
	err := g.fresh("err")
	call := g.callGo(e.Call)
	switch e.Call.Results {
	case core.GoResultsErr:
		// Under Declare — reached only from let's selfDeclares
		// path — the call result binds the let name directly:
		// `n, err := f()`. Every other destination takes a fresh
		// variable: the node may emit inside a nested block where
		// `n, err :=` would declare a *new* n shadowing the `var`
		// of the enclosing block (which then goes unused and keeps
		// its zero value).
		v := d.name
		if d.kind != dDeclare {
			v = g.fresh("v")
		}
		g.raw(v + ", " + err + " := ")
		g.expr(call)
		g.nl()
		g.errIf(e.Sp, err, e.Wrap, e.To)
		if e.NilGuard {
			// The declared verbatim `Result T GoError` view
			// (e.g. `Result GoError GoError`, `Result
			// net.Error GoError`) claims the payload is a
			// real value whenever no error escapes — errIf
			// already returned on a non-nil err, so a nil v
			// here is exactly `(nil, nil)`: a contract
			// violation (spec section 8.6).
			g.raw("if " + v + " == nil")
			g.open()
			g.line(e.Sp)
			g.raw("panic(" + strconv.Quote("gom: "+goCallName(e.Call)+" returned (nil, nil) — its binding declares a never-nil payload") + ")")
			g.nl()
			g.close()
		}
		if d.kind != dDeclare {
			g.writeDest(d, ast.NewIdent(v))
		}
	case core.GoResultsErrOnly:
		g.raw(err + " := ")
		g.expr(call)
		g.nl()
		g.errIf(e.Sp, err, e.Wrap, e.To)
		g.unitValue(d)
	default:
		panic("codegen: EscapeCall over non-error result shape")
	}
}

// errIf emits `if err != nil { <escape> }` for a bound error var;
// sp re-anchors the arm's //line so the reported failure position is
// the escape's own, not a drifted offset of the enclosing construct.
func (g *gen) errIf(sp diag.Span, errVar, wrap string, to *core.Func) {
	g.raw("if " + errVar + " != nil")
	g.open()
	g.escapeErr(sp, ast.NewIdent(errVar), wrap, to)
	g.close()
}

// escapeErr emits the error arm of an escape: `return
// result.Err[T, E](err)` at a function boundary, `t.Errorf; return`
// at a test boundary. T and E come from the boundary's residual
// `Result _ e` type (validator guarantees the shape). sp re-anchors
// the arm's //line — a //line covers only the line right after it
// and the arm sits several lines below the escape's own.
func (g *gen) escapeErr(sp diag.Span, errExpr ast.Expr, wrap string, to *core.Func) {
	if wrap != "" {
		errExpr = &ast.CallExpr{
			Fun: ast.NewIdent(g.pkgQual(rtPath) + "StepErr"),
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(wrap)},
				errExpr,
			},
		}
	}
	// Only an escape that targets the *test function itself* fails
	// the test — a closure nested inside a test still escapes to
	// its own Result boundary (testFn, not inTest: g.inTest would
	// misfire on nested closures).
	if to == g.testFn && g.testFn != nil {
		g.line(sp)
		g.raw("t.Errorf(" + strconv.Quote("%v") + ", ")
		g.expr(errExpr)
		g.raw(")")
		g.nl()
		g.raw("return")
		g.nl()
		return
	}
	ret := types.Chase(to.ResidualType())
	con, ok := ret.(*types.Con)
	// The builtin-Result check mirrors the ctor sites (isResultCon +
	// localT): a file-local `type Result` shadows the builtin, and
	// emitting stdlib's result.Err for it would be wrong. The
	// validator cannot tell the two apart yet — see
	// docs/problems/2026-11-02-local-result-vs-builtin-escape.md.
	if !ok || !isResultCon(con) || len(con.Args) != 2 || g.localT["Result"] {
		panic("codegen: escape target's residual type is not the builtin Result")
	}
	g.line(sp)
	g.raw("return " + g.pkgQual(resultPath) + "Err[")
	g.expr(g.goType(con.Args[0]))
	g.raw(", ")
	g.expr(g.goType(con.Args[1]))
	g.raw("](")
	g.expr(errExpr)
	g.raw(")")
	g.nl()
}

// resultFromGo emits `v, err := call` then materializes the Result —
// `result.FromGo(v, err)` / `result.FromErr(err)` (call.go —
// the non-fused boundary conversion).
func (g *gen) resultFromGo(e *core.ResultFromGo, d dest) {
	g.line(e.Sp)
	call := g.callGo(e.Call)
	switch e.Call.Results {
	case core.GoResultsErr:
		v, err := g.fresh("v"), g.fresh("err")
		g.raw(v + ", " + err + " := ")
		g.expr(call)
		g.nl()
		if e.NilGuard {
			// A verbatim `Result T GoError` payload claims the
			// payload is a real value whenever the second
			// result is nil — a `(nil, nil)` return violates
			// it; a `(nil, err)` pair keeps the ordinary Err
			// reading, so the check guards `err == nil` too.
			g.raw("if " + err + " == nil && " + v + " == nil")
			g.open()
			g.line(e.Sp)
			g.raw("panic(" + strconv.Quote("gom: "+goCallName(e.Call)+" returned (nil, nil) — its binding declares a never-nil payload") + ")")
			g.nl()
			g.close()
		}
		g.writeDest(d, &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(resultPath) + "FromGo"),
			Args: []ast.Expr{g.convOf(e.ValConv, payloadType(e.Type(), 0), ast.NewIdent(v)), ast.NewIdent(err)},
		})
	case core.GoResultsErrOnly:
		err := g.fresh("err")
		g.raw(err + " := ")
		g.expr(call)
		g.nl()
		g.writeDest(d, &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(resultPath) + "FromErr"),
			Args: []ast.Expr{ast.NewIdent(err)},
		})
	case core.GoResultsTupErr:
		// `v0, v1, ..., err := call` →
		// `result.FromGo(rt.TupleN{...}, err)` — the N-ary
		// trailing-error fold (spec section 8.3).
		tupT := payloadType(e.Type(), 0)
		n := 0
		if tup, ok := types.Chase(tupT).(*types.Tuple); ok {
			n = len(tup.Elems)
		}
		vs := make([]string, n)
		for i := range vs {
			vs[i] = g.fresh("v")
		}
		err := g.fresh("err")
		g.raw(strings.Join(append(vs, err), ", ") + " := ")
		g.expr(call)
		g.nl()
		lit := &ast.CompositeLit{Type: g.goType(tupT)}
		for i, v := range vs {
			var conv core.ConvKind
			if len(e.ElemConv) > i {
				conv = e.ElemConv[i]
			}
			lit.Elts = append(lit.Elts, &ast.KeyValueExpr{
				Key:   ast.NewIdent(fmt.Sprintf("V%d", i)),
				Value: g.convOf(conv, payloadType(tupT, i), ast.NewIdent(v)),
			})
		}
		g.writeDest(d, &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(resultPath) + "FromGo"),
			Args: []ast.Expr{lit, ast.NewIdent(err)},
		})
	default:
		panic("codegen: ResultFromGo over non-error result shape")
	}
}

// optionFromGo emits the presence/absence read a declared Option
// needs: `v, ok := call` then `option.FromGo(v, ok)` for a comma-ok
// result, or `v := call` then `option.FromGo(v, v != nil)` for a
// `*T` result an extern declared `Option` for (call.go OptionFromGo,
// spec section 8.4).
func (g *gen) optionFromGo(e *core.OptionFromGo, d dest) {
	g.line(e.Sp)
	var okTest ast.Expr
	switch e.Call.Results {
	case core.GoResultsOK:
		v, ok := g.fresh("v"), g.fresh("ok")
		g.raw(v + ", " + ok + " := ")
		g.expr(g.callGo(e.Call))
		g.nl()
		okTest = ast.NewIdent(ok)
		g.optionFromGoDest(d, v, okTest, e)
	case core.GoResultsNil:
		v := g.fresh("v")
		g.raw(v + " := ")
		g.expr(g.callGo(e.Call))
		g.nl()
		okTest = &ast.BinaryExpr{X: ast.NewIdent(v), Op: token.NEQ, Y: ast.NewIdent("nil")}
		g.optionFromGoDest(d, v, okTest, e)
	default:
		panic("codegen: OptionFromGo over non-option result shape")
	}
}

// optionFromGoDest writes `option.FromGo(<conv>v, okTest)` to the
// destination — the tail shared by the comma-ok and nil-test
// readings.
func (g *gen) optionFromGoDest(d dest, v string, okTest ast.Expr, e *core.OptionFromGo) {
	g.writeDest(d, &ast.CallExpr{
		Fun:  ast.NewIdent(g.pkgQual(optionPath) + "FromGo"),
		Args: []ast.Expr{g.convOf(e.ValConv, payloadType(e.Type(), 0), ast.NewIdent(v)), okTest},
	})
}

// errorFromGo emits `v := call` then the never-nil check a
// `-> GoError` declared view claims (call.go ErrorFromGo, spec
// section 8.4): `if v == nil { panic }` — a nil result proves the
// binding's claim false, a contract violation in the sense of spec
// section 8.6, so it panics at the boundary instead of smuggling a
// nil error into Gom. The check is uniform: it also guards the
// compiler-curated standard bindings, where it doubles as drift
// insurance (docs/decisions/2026-11-05-error-result-bindings.md).
func (g *gen) errorFromGo(e *core.ErrorFromGo, d dest) {
	g.line(e.Sp)
	v := g.fresh("v")
	g.raw(v + " := ")
	g.expr(g.callGo(e.Call))
	g.nl()
	g.raw("if " + v + " == nil")
	g.open()
	g.line(e.Sp)
	g.raw("panic(" + strconv.Quote("gom: "+goCallName(e.Call)+" returned nil error — its binding declares a never-nil GoError result") + ")")
	g.nl()
	g.close()
	g.writeDest(d, ast.NewIdent(v))
}

// goCallName renders the callee for diagnostics — `errors.New`
// style (`pkg.Name`, the package's base name); a method call (no
// declared binding can produce one today) reports bare `Name`.
func goCallName(c *core.CallGo) string {
	if c.Fn != nil {
		return "the func value"
	}
	if c.Recv != nil || c.Callee.Package == "" {
		return c.Callee.Name
	}
	return pathBase(c.Callee.Package) + "." + c.Callee.Name
}

// tupleFromGo emits `a, b := call` then an rt.TupleN literal — the
// pair keeps the short `a, b` names, an N-ary call (three or more
// results) binds `v0, v1, ...`.
func (g *gen) tupleFromGo(e *core.TupleFromGo, d dest) {
	g.line(e.Sp)
	n := 2
	if tup, ok := types.Chase(e.Type()).(*types.Tuple); ok {
		n = len(tup.Elems)
	}
	vs := make([]string, n)
	if n == 2 {
		vs[0], vs[1] = g.fresh("a"), g.fresh("b")
	} else {
		for i := range vs {
			vs[i] = g.fresh("v")
		}
	}
	g.raw(strings.Join(vs, ", ") + " := ")
	g.expr(g.callGo(e.Call))
	g.nl()
	lit := &ast.CompositeLit{Type: g.goType(e.Type())}
	for i, v := range vs {
		var conv core.ConvKind
		if len(e.ElemConv) > i {
			conv = e.ElemConv[i]
		}
		lit.Elts = append(lit.Elts, &ast.KeyValueExpr{
			Key:   ast.NewIdent(fmt.Sprintf("V%d", i)),
			Value: g.convOf(conv, payloadType(e.Type(), i), ast.NewIdent(v)),
		})
	}
	g.writeDest(d, lit)
}

// convOf wraps x in the conversion kind names — the
// lowering-recorded `int` → `int64` (and friends) adjustments
// inside Go-result consumers. kind 0 means none; the rendering
// mirrors conv in expr.go for a value already in hand.
func (g *gen) convOf(kind core.ConvKind, target types.Type, x ast.Expr) ast.Expr {
	switch kind {
	case 0:
		return x
	case core.ConvNum, core.ConvNamed, core.ConvIface:
		return &ast.CallExpr{Fun: g.goType(target), Args: []ast.Expr{x}}
	case core.ConvChanDir:
		// Parenthesized like in conv — `<-chan T` cannot stand bare
		// in call position.
		return &ast.CallExpr{Fun: &ast.ParenExpr{X: g.goType(target)}, Args: []ast.Expr{x}}
	case core.ConvBoxAny:
		return &ast.CallExpr{Fun: ast.NewIdent("any"), Args: []ast.Expr{x}}
	case core.ConvSliceToGo:
		return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: ast.NewIdent("ToSlice")}}
	case core.ConvSliceFromGo:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(listPath) + "FromSlice"), Args: []ast.Expr{x}}
	case core.ConvSliceBox:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(rtPath) + "BoxSlice"), Args: []ast.Expr{x}}
	case core.ConvOptNil:
		// `option.FromGo(v, v != nil)` — the nil-able payload's
		// absence read inside a consumer (spec section 8.4).
		return &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(optionPath) + "FromGo"),
			Args: []ast.Expr{x, &ast.BinaryExpr{X: x, Op: token.NEQ, Y: ast.NewIdent("nil")}},
		}
	case core.ConvOptToNil:
		// `option.NilOr(v)` — the write side of the `(T, error)`
		// boundary: `Some` is the payload, `None` the nil.
		return &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(optionPath) + "NilOr"),
			Args: []ast.Expr{x},
		}
	default:
		panic("unreachable")
	}
}

// payloadType returns the i-th component type of a consumer node's
// materialized type: Result/Option's payload, a Tuple's element.
// Callers only invoke it when the corresponding conversion is set,
// so the shape is guaranteed by lowering.
func payloadType(t types.Type, i int) types.Type {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if len(t.Args) > i {
			return t.Args[i]
		}
	case *types.Tuple:
		if len(t.Elems) > i {
			return t.Elems[i]
		}
	case *types.Var, *types.Meta, *types.Func, *types.Go:
		panic("unreachable")
	default:
		panic("unreachable")
	}
	panic(fmt.Sprintf("codegen: no payload type %d in %s", i, t))
}

// recordUpdate emits copy-then-set: `u := rec; u.F = v; ...` then the
// copy to the destination (expr.go RecordUpdate — the structural
// update is a copy, not a mutation).
func (g *gen) recordUpdate(e *core.RecordUpdate, d dest) {
	g.line(e.Sp)
	u := g.fresh("u")
	g.raw(u + " := ")
	g.expr(g.atom(e.Rec))
	g.nl()
	for _, f := range e.Fields {
		g.line(f.Sp)
		g.raw(u + "." + exportName(f.Sym.Name) + " = ")
		g.expr(g.atom(f.Value))
		g.nl()
	}
	g.writeDest(d, ast.NewIdent(u))
}

// fieldSet emits `recv.Name = v` — a statement-only node. Only the
// discard destination is reachable: lower binds the write to `_`.
// The Go field name is used verbatim, like fieldGet's IsGo branch.
func (g *gen) fieldSet(e *core.FieldSet, d dest) {
	if d.kind != dDiscard {
		panic("unreachable")
	}
	g.line(e.Sp)
	g.expr(&ast.SelectorExpr{X: g.atom(e.Recv), Sel: ast.NewIdent(e.Field.Name)})
	g.raw(" = ")
	g.expr(g.atom(e.Value))
	g.nl()
}

// funcBody emits a Func's body — wrapped in `for { ... }` when the
// TailLoop flag says self-tail calls reassign-and-continue. A
// ConsLoop body adds the build buffer: `var consN List` before the
// loop, every tail appends to it, and the loop's fall-through edge
// — the base leaf — returns it (a TailCall's `continue` never
// reaches the return).
func (g *gen) funcBody(fn *core.Func, d dest) {
	if fn.ConsLoop {
		out := g.fresh("cons")
		g.raw("var " + out + " ")
		g.typ(g.goType(fn.ResidualType()))
		g.nl()
		g.raw("for")
		g.open()
		g.body(fn.Body, consBuf(out))
		g.raw("return " + out)
		g.nl()
		g.close()
		return
	}
	if fn.TailLoop {
		g.raw("for")
		g.open()
		g.body(fn.Body, d)
		g.close()
		return
	}
	g.body(fn.Body, d)
}

// funcLit writes a function literal's header and body, keeping the
// body's //line directives (the reason closures take the statement
// path rather than the ast.Expr one).
func (g *gen) funcLit(fn *core.Func) {
	g.raw("func(")
	g.params(fn.Params)
	g.raw(") ")
	g.resultSig(fn.ResidualType(), fn.Body)
	g.open()
	g.funcBody(fn, ret)
	g.closeInline()
}

// paramsFlat is params for a function *declaration*'s parameter
// list — a parameter of ≥2-arrow function type emits the flat repr
// `func(T1', ..., Tk') R'` (docs/decisions/2026-11-18-flat-fn-params.md).
// Closure and adapter literals keep `params`: their values are
// canonical.
func (g *gen) paramsFlat(binds []*core.Bind) {
	g.paramsGo(binds, func(b *core.Bind) ast.Expr {
		if flatArity(b.Typ) >= 2 {
			return g.flatFuncType(b.Typ)
		}
		return g.goType(b.Typ)
	})
}

// params renders a parameter list `a T0, b T1` — consecutive
// same-type parameters are grouped `a, b T`.
func (g *gen) params(binds []*core.Bind) {
	g.paramsGo(binds, func(b *core.Bind) ast.Expr { return g.goType(b.Typ) })
}

// paramsGo is params with a caller-chosen type renderer.
func (g *gen) paramsGo(binds []*core.Bind, ty func(b *core.Bind) ast.Expr) {
	// Render each parameter's type to a string so adjacent equal
	// types can share one mention.
	typeText := make([]string, len(binds))
	for i, b := range binds {
		mark := g.buf.Len()
		g.typ(ty(b))
		typeText[i] = g.buf.String()[mark:]
		g.buf.Truncate(mark)
	}
	for i, b := range binds {
		if i > 0 {
			g.raw(", ")
		}
		g.raw(g.bindName(b))
		if i+1 == len(binds) || typeText[i+1] != typeText[i] {
			g.raw(" " + typeText[i])
		}
	}
}

// goFuncLit writes the flat `func(p0 T0', ...) (R'...) { ... }`
// literal a `Go.funcN` conversion emits — parameters and results in
// the raw Go view, the body applying the curried value and
// re-converting what comes back per the node's recorded recipe
// (docs/decisions/2026-11-17-func-interop.md). The error shapes
// unwrap the Result with its own `Unwrap` — an invalid (zero)
// Result still panics there like everywhere else.
func (g *gen) goFuncLit(c *core.GoFunc) {
	g.raw("func(")
	g.params(c.Params)
	g.raw(") ")
	switch len(c.Results) {
	case 0:
	case 1:
		g.typ(g.goType(c.Results[0]))
		g.raw(" ")
	default:
		g.raw("(")
		for i, r := range c.Results {
			if i > 0 {
				g.raw(", ")
			}
			g.typ(g.goType(r))
		}
		g.raw(") ")
	}
	g.open()
	var call ast.Expr = g.atom(c.X)
	if len(c.In) == 0 {
		// A nullary adapter's call is a bare `f()` — the atom by
		// itself would return the function value unapplied.
		call = &ast.CallExpr{Fun: call}
	}
	for _, in := range c.In {
		call = &ast.CallExpr{Fun: call, Args: []ast.Expr{g.simpleExpr(in)}}
	}
	switch c.ResShape {
	case core.GoResultsNone:
		g.expr(call)
		g.nl()
	case core.GoResultsOne:
		if c.ResConv == 0 {
			g.raw("return ")
			g.expr(call)
			g.nl()
			break
		}
		r := g.fresh("r")
		g.raw(r + " := ")
		g.expr(call)
		g.nl()
		g.raw("return ")
		g.expr(g.convOf(c.ResConv, c.Results[0], ast.NewIdent(r)))
		g.nl()
	case core.GoResultsErrOnly:
		err := g.fresh("err")
		g.raw("_, " + err + " := ")
		g.expr(&ast.CallExpr{Fun: &ast.SelectorExpr{X: call, Sel: ast.NewIdent("Unwrap")}})
		g.nl()
		g.raw("return " + err)
		g.nl()
	case core.GoResultsErr:
		v, err := g.fresh("v"), g.fresh("err")
		g.raw(v + ", " + err + " := ")
		g.expr(&ast.CallExpr{Fun: &ast.SelectorExpr{X: call, Sel: ast.NewIdent("Unwrap")}})
		g.nl()
		g.raw("if " + err + " != nil")
		g.open()
		z := g.fresh("z")
		g.raw("var " + z + " ")
		g.typ(g.goType(c.Results[0]))
		g.nl()
		g.raw("return " + z + ", " + err)
		g.nl()
		g.close()
		g.raw("return ")
		g.expr(g.convOf(c.ResConv, c.Results[0], ast.NewIdent(v)))
		g.raw(", nil")
		g.nl()
	case core.GoResultsPair, core.GoResultsNary:
		// The Gom function returns a tuple value; each raw result
		// is a converted element read.
		v := g.fresh("v")
		g.raw(v + " := ")
		g.expr(call)
		g.nl()
		g.raw("return ")
		for i, r := range c.Results {
			if i > 0 {
				g.raw(", ")
			}
			var conv core.ConvKind
			if len(c.ResConvs) > i {
				conv = c.ResConvs[i]
			}
			g.expr(g.convOf(conv, r, &ast.SelectorExpr{
				X:   ast.NewIdent(v),
				Sel: ast.NewIdent(fmt.Sprintf("V%d", i)),
			}))
		}
		g.nl()
	case core.GoResultsTupErr:
		// `Result (Tuple ...) GoError` → the raw (…, error) list:
		// Unwrap splits payload and error; an error escape returns
		// zero payloads beside it.
		v, err := g.fresh("v"), g.fresh("err")
		g.raw(v + ", " + err + " := ")
		g.expr(&ast.CallExpr{Fun: &ast.SelectorExpr{X: call, Sel: ast.NewIdent("Unwrap")}})
		g.nl()
		g.raw("if " + err + " != nil")
		g.open()
		zeros := make([]string, len(c.Results)-1)
		for i := range zeros {
			zeros[i] = g.fresh("z")
			g.raw("var " + zeros[i] + " ")
			g.typ(g.goType(c.Results[i]))
			g.nl()
		}
		g.raw("return " + strings.Join(zeros, ", ") + ", " + err)
		g.nl()
		g.close()
		g.raw("return ")
		for i := range zeros {
			if i > 0 {
				g.raw(", ")
			}
			var conv core.ConvKind
			if len(c.ResConvs) > i {
				conv = c.ResConvs[i]
			}
			g.expr(g.convOf(conv, c.Results[i], &ast.SelectorExpr{
				X:   ast.NewIdent(v),
				Sel: ast.NewIdent(fmt.Sprintf("V%d", i)),
			}))
		}
		g.raw(", nil")
		g.nl()
	default:
		panic("codegen: GoFunc with unexpected result shape")
	}
	g.closeInline()
}

// resultSig emits the result half of a signature: `(res T)` when the
// body names its result for deferred cleanups, a bare `T` otherwise.
func (g *gen) resultSig(t types.Type, b *core.Body) {
	if b != nil && b.ResBind != nil {
		g.raw("(" + g.bindName(b.ResBind) + " ")
		g.typ(g.goType(t))
		g.raw(")")
		return
	}
	g.typ(g.goType(t))
}
