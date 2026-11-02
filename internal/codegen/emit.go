package codegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"

	"github.com/podhmo/gom/internal/core"
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
)

// dest is the destination context. name is set for dAssign and
// dDeclare.
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
	case *core.ForRange:
		g.forExpr(e, d)
	case *core.TailCall:
		g.tailCall(e)
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
	case *core.RecordUpdate:
		g.recordUpdate(e, d)
	case *core.Closure,
		*core.Var, *core.Lit, *core.FuncRef, *core.CtorRef,
		*core.GoFuncRef, *core.FieldGet,
		*core.Unary, *core.Binary, *core.Equal,
		*core.CallDirect, *core.CallFunc, *core.CallGo, *core.CallBuiltin,
		*core.CtorApp, *core.TupleNew, *core.ListNew, *core.RecordNew,
		*core.Conv:
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
		x := g.simpleExpr(e)
		_, isConv := e.(*core.Conv)
		if _, ok := x.(*ast.CallExpr); ok && !isConv {
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

// exprValue writes the expression part — a function literal for a
// Closure, the single expression otherwise.
func (g *gen) exprValue(e core.Expr) {
	if c, ok := e.(*core.Closure); ok {
		g.funcLit(c.Fn)
		return
	}
	g.expr(g.simpleExpr(e))
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
	var targs []types.Type
	if con, ok := types.Chase(s.Subj.Type()).(*types.Con); ok {
		targs = con.Args
	}
	g.caseLabel(func() {
		g.raw("case ")
		g.expr(g.ctorGoType(ct.Ctor, s.Subj.Type(), targs))
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
		g.dcaseValue(c, d)
	}
	g.ddefault(s, d)
	g.close()
}

func (g *gen) dcaseValue(c core.DCase, d dest) {
	g.line(c.Sp)
	lt, ok := c.Test.(*core.DLitTest)
	if !ok {
		panic("codegen: SwitchValue case test is not a DLitTest")
	}
	g.caseLabel(func() {
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
// through rt.OptionFromGo.
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
		g.raw(g.bindName(a.Bind) + " := " + g.pkgQual(rtPath) + "OptionFromGo(" + v + ", " + ok + ")")
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
// the semantics the node guarantees.
//
// A pair `p = p` rebinds a parameter to itself — a no-op that
// `go vet` reports as self-assignment (and the compile canary's
// vet-equivalent checks flag the same way). Such pairs are dropped:
// a self-call passing every parameter unchanged emits the bare
// `continue`, and a mixed call keeps only the changing parameters.
func (g *gen) tailCall(e *core.TailCall) {
	g.line(e.Sp)
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
// `result.Err[T, E](err)`, `fmt.Errorf("step: %w", err)` around it
// when Wrap names a pipeline step. Inside a test the Err arm fails
// the test instead.
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
	g.escapeErr(&ast.SelectorExpr{X: subj, Sel: ast.NewIdent("Error")}, e.Wrap, e.To)
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
		g.errIf(err, e.Wrap, e.To)
		if d.kind != dDeclare {
			g.writeDest(d, ast.NewIdent(v))
		}
	case core.GoResultsErrOnly:
		g.raw(err + " := ")
		g.expr(call)
		g.nl()
		g.errIf(err, e.Wrap, e.To)
		g.unitValue(d)
	default:
		panic("codegen: EscapeCall over non-error result shape")
	}
}

// errIf emits `if err != nil { <escape> }` for a bound error var.
func (g *gen) errIf(errVar, wrap string, to *core.Func) {
	g.raw("if " + errVar + " != nil")
	g.open()
	g.escapeErr(ast.NewIdent(errVar), wrap, to)
	g.close()
}

// escapeErr emits the error arm of an escape: `return
// result.Err[T, E](err)` at a function boundary, `t.Errorf; return`
// at a test boundary. T and E come from the boundary's residual
// `Result _ e` type (validator guarantees the shape).
func (g *gen) escapeErr(errExpr ast.Expr, wrap string, to *core.Func) {
	if wrap != "" {
		errExpr = &ast.CallExpr{
			Fun: ast.NewIdent(g.pkgQual("fmt") + "Errorf"),
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(wrap + ": %w")},
				errExpr,
			},
		}
	}
	// Only an escape that targets the *test function itself* fails
	// the test — a closure nested inside a test still escapes to
	// its own Result boundary (testFn, not inTest: g.inTest would
	// misfire on nested closures).
	if to == g.testFn && g.testFn != nil {
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
// `rt.ResultFromGo(v, err)` / `rt.ResultFromErr(err)` (call.go —
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
		g.writeDest(d, &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(rtPath) + "ResultFromGo"),
			Args: []ast.Expr{g.convOf(e.ValConv, payloadType(e.Type(), 0), ast.NewIdent(v)), ast.NewIdent(err)},
		})
	case core.GoResultsErrOnly:
		err := g.fresh("err")
		g.raw(err + " := ")
		g.expr(call)
		g.nl()
		g.writeDest(d, &ast.CallExpr{
			Fun:  ast.NewIdent(g.pkgQual(rtPath) + "ResultFromErr"),
			Args: []ast.Expr{ast.NewIdent(err)},
		})
	default:
		panic("codegen: ResultFromGo over non-error result shape")
	}
}

// optionFromGo emits `v, ok := call` then `rt.OptionFromGo(v, ok)` —
// the comma-ok boundary conversion (call.go OptionFromGo).
func (g *gen) optionFromGo(e *core.OptionFromGo, d dest) {
	g.line(e.Sp)
	v, ok := g.fresh("v"), g.fresh("ok")
	g.raw(v + ", " + ok + " := ")
	g.expr(g.callGo(e.Call))
	g.nl()
	g.writeDest(d, &ast.CallExpr{
		Fun:  ast.NewIdent(g.pkgQual(rtPath) + "OptionFromGo"),
		Args: []ast.Expr{g.convOf(e.ValConv, payloadType(e.Type(), 0), ast.NewIdent(v)), ast.NewIdent(ok)},
	})
}

// tupleFromGo emits `a, b := call` then an rt.Tuple2 literal.
func (g *gen) tupleFromGo(e *core.TupleFromGo, d dest) {
	g.line(e.Sp)
	a, b := g.fresh("a"), g.fresh("b")
	g.raw(a + ", " + b + " := ")
	g.expr(g.callGo(e.Call))
	g.nl()
	var convA, convB core.ConvKind
	if len(e.ElemConv) > 0 {
		convA = e.ElemConv[0]
	}
	if len(e.ElemConv) > 1 {
		convB = e.ElemConv[1]
	}
	lit := &ast.CompositeLit{Type: g.goType(e.Type()), Elts: []ast.Expr{
		&ast.KeyValueExpr{Key: ast.NewIdent("V0"), Value: g.convOf(convA, payloadType(e.Type(), 0), ast.NewIdent(a))},
		&ast.KeyValueExpr{Key: ast.NewIdent("V1"), Value: g.convOf(convB, payloadType(e.Type(), 1), ast.NewIdent(b))},
	}}
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
	case core.ConvBoxAny:
		return &ast.CallExpr{Fun: ast.NewIdent("any"), Args: []ast.Expr{x}}
	case core.ConvSliceToGo:
		return &ast.CallExpr{Fun: &ast.SelectorExpr{X: x, Sel: ast.NewIdent("ToSlice")}}
	case core.ConvSliceFromGo:
		return &ast.CallExpr{Fun: ast.NewIdent(g.pkgQual(listPath) + "FromSlice"), Args: []ast.Expr{x}}
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

// funcBody emits a Func's body — wrapped in `for { ... }` when the
// TailLoop flag says self-tail calls reassign-and-continue.
func (g *gen) funcBody(fn *core.Func, d dest) {
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
	g.typ(g.goType(fn.ResidualType()))
	g.open()
	g.funcBody(fn, ret)
	g.closeInline()
}

// params renders a parameter list `a T0, b T1` — consecutive
// same-type parameters are grouped `a, b T`.
func (g *gen) params(binds []*core.Bind) {
	// Render each parameter's type to a string so adjacent equal
	// types can share one mention.
	typeText := make([]string, len(binds))
	for i, b := range binds {
		mark := g.buf.Len()
		g.typ(g.goType(b.Typ))
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
