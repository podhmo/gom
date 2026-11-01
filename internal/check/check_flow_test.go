package check

// Control flow: blocks, if/match, let/lambda, loops, select, lists,
// records, test blocks — and the diagnostic codes each carries.

import (
	"testing"

	"github.com/podhmo/gom/internal/syntax"
)

// `{ 1; () }` — a non-Unit mid-block statement is E0402.
func TestBlockMidNonUnit(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(b.ilit("1"), b.ulit()))

	_, diags := b.check(nil, main)
	requireCodes(t, diags, "E0402")
}

// `{ (); () }` is fine.
func TestBlockAllUnit(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(b.ulit(), b.ulit()))

	_, diags := b.check(nil, main)
	requireCodes(t, diags)
}

// An empty block is Unit.
func TestBlockEmpty(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"), b.block())

	f, diags := b.check(nil, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "Unit" {
		t.Errorf("empty block type = %s, want Unit", got)
	}
}

// `if c { () }` — the then-branch is Unit; no else is required.
func TestIfNoElseUnit(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	cp, cs := b.param("c", "", b.tn("Bool"))
	f1, _ := b.fn("f", []syntax.Param{up, cp}, b.tn("Unit"),
		b.ifx(b.ref(cs), b.block(b.ulit()), nil))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// `if c { 1 }` without else is E0403.
func TestIfNoElseNonUnit(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	cp, cs := b.param("c", "", b.tn("Bool"))
	f1, _ := b.fn("f", []syntax.Param{up, cp}, b.tn("Unit"),
		b.ifx(b.ref(cs), b.block(b.ilit("1")), nil))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0403")
}

// `if c { a } else { b }` unifies the branches.
func TestIfElse(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Int"),
		b.ifx(b.blit(true), b.block(b.ilit("1")), b.block(b.ilit("2"))))

	f, diags := b.check(nil, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "Int" {
		t.Errorf("if type = %s, want Int", got)
	}
}

// Branch mismatch is E0401.
func TestIfElseMismatch(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Int"),
		b.ifx(b.blit(true), b.block(b.ilit("1")), b.block(b.slit("s"))))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// A let inside a block binds names for the rest.
func TestLetInBlock(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	xp, xs := b.vp("x")
	body := b.block(
		b.letStmt(xp, b.ilit("1")),
		b.bin(syntax.BinAdd, b.ref(xs), b.ilit("2")))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"), body)

	f, diags := b.check(nil, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "Int" {
		t.Errorf("block type = %s, want Int", got)
	}
}

// A top-level unannotated let of a lambda asks for an annotation —
// the checker does not generalize.
func TestTopLetUnannotatedLambda(t *testing.T) {
	b := newTB()
	xp, xs := b.vp("x")
	fp, fs := b.vp("f")
	d := b.letd(fp, nil, b.lambda([]syntax.Param{
		func() syntax.Param {
			return syntax.Param{Sp: xp.Sp, Pat: xp}
		}(),
	}, b.ref(xs)))
	_ = fs

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0407")
}

// An annotated lambda parameter checks against the annotation.
func TestLambdaTyped(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	fp, _ := b.vp("f")
	d := b.letd(fp, b.tfn(b.tn("Int"), b.tp(b.tn("Int"))),
		b.lambda([]syntax.Param{xp}, b.bin(syntax.BinAdd, b.ref(xs), b.ilit("1"))))

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	if got := ld.Value.Type().String(); got != "Int -> Int" {
		t.Errorf("lambda type = %s, want `Int -> Int`", got)
	}
}

// `1 + 2` defaults to Int; `1 + 2.0` is Float.
func TestArithDefault(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	yp, _ := b.vp("y")
	d1 := b.letd(xp, nil, b.bin(syntax.BinAdd, b.ilit("1"), b.ilit("2")))
	d2 := b.letd(yp, nil, b.bin(syntax.BinAdd, b.ilit("1"), b.flit("2.0")))

	f, diags := b.check(nil, d1, d2)
	requireCodes(t, diags)
	x := findDecl[*LetDecl](f, 0)
	y := findDecl[*LetDecl](f, 1)
	if got := x.Value.Type().String(); got != "Int" {
		t.Errorf("1+2 type = %s, want Int", got)
	}
	if got := y.Value.Type().String(); got != "Float" {
		t.Errorf("1+2.0 type = %s, want Float", got)
	}
}

// `"a" + "b"` is E0401 — strings are not numeric.
func TestArithNonNumeric(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	d := b.letd(xp, nil, b.bin(syntax.BinAdd, b.slit("a"), b.slit("b")))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0401")
}

// `%` on floats is E0401 — the operands must be integral.
func TestModFloat(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	d := b.letd(xp, nil, b.bin(syntax.BinMod, b.flit("1.0"), b.flit("2.0")))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0401")
}

// `==` on a function type is E0409.
func TestEqFunction(t *testing.T) {
	b := newTB()
	fp, fs := b.param("f", "", b.tfn(b.tn("Int"), b.tp(b.tn("Int"))))
	gp, gs := b.param("g", "", b.tfn(b.tn("Int"), b.tp(b.tn("Int"))))
	eq, _ := b.fn("eq", []syntax.Param{fp, gp}, b.tn("Bool"),
		b.bin(syntax.BinEq, b.ref(fs), b.ref(gs)))

	_, diags := b.check(nil, eq)
	requireCodes(t, diags, "E0409")
}

// `==` on ints is fine.
func TestEqInt(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Bool"),
		b.bin(syntax.BinEq, b.ilit("1"), b.ilit("2")))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// A comparison is Bool.
func TestCompare(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Bool"),
		b.bin(syntax.BinLt, b.ilit("1"), b.ilit("2")))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// `x |> f` applies f to x.
func TestPipeFwd(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	fp, fs := b.param("f", "", b.tfn(b.tn("String"), b.tp(b.tn("Int"))))
	pipe, _ := b.fn("pipe", []syntax.Param{xp, fp}, b.tn("String"),
		b.bin(syntax.BinPipeFwd, b.ref(xs), b.ref(fs)))

	f, diags := b.check(nil, pipe)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "String" {
		t.Errorf("pipe type = %s, want String", got)
	}
}

// `f <| x` applies f to x.
func TestPipeBack(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	fp, fs := b.param("f", "", b.tfn(b.tn("String"), b.tp(b.tn("Int"))))
	pipe, _ := b.fn("pipe", []syntax.Param{xp, fp}, b.tn("String"),
		b.bin(syntax.BinPipeBack, b.ref(fs), b.ref(xs)))

	_, diags := b.check(nil, pipe)
	requireCodes(t, diags)
}

// `f >> g` composes.
func TestComposeFwd(t *testing.T) {
	b := newTB()
	fp, fs := b.param("f", "", b.tfn(b.tn("Int"), b.tp(b.tn("Int"))))
	gp, gs := b.param("g", "", b.tfn(b.tn("String"), b.tp(b.tn("Int"))))
	comp, _ := b.fn("comp", []syntax.Param{fp, gp},
		b.tfn(b.tn("String"), b.tp(b.tn("Int"))),
		b.bin(syntax.BinComposeFwd, b.ref(fs), b.ref(gs)))

	_, diags := b.check(nil, comp)
	requireCodes(t, diags)
}

// `!b` and `-n` are unary.
func TestUnary(t *testing.T) {
	b := newTB()
	bp, bs := b.param("b", "", b.tn("Bool"))
	np, ns := b.param("n", "", b.tn("Int"))
	f1, _ := b.fn("f", []syntax.Param{bp, np},
		b.tn("Bool"),
		b.block(
			b.letStmt(func() syntax.Pat { p, _ := b.vp("r"); return p }(),
				b.unary(syntax.UnNeg, b.ref(ns))),
			b.unary(syntax.UnNot, b.ref(bs))))

	f, diags := b.check(nil, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	neg := applyAt(d.Body.(*Block).Stmts[0].(*Let).Value)
	_ = neg
}

// ---- match and patterns -----------------------------------------------

// `match o { Some x -> x; None -> 0 }` — constructor patterns bind
// the payload type, and arm bodies unify on the result.
func TestMatchCtor(t *testing.T) {
	b := newTB()
	someSym := b.ctorSym("Some")
	noneSym := b.ctorSym("None")
	op, os := b.param("o", "", b.tapp(b.tn("Option"), b.tn("Int")))
	xp, xs := b.vp("x")
	m := b.matchx(b.ref(os),
		b.arm(b.ctorPatSym(someSym, b.ctorArg("", xp)), nil, b.ref(xs)),
		b.arm(b.ctorPatSym(noneSym), nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{op}, b.tn("Int"), m)

	f, diags := b.check(nil, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	m2 := d.Body.(*Match)
	cp := m2.Arms[0].Pat.(*CtorPat)
	if cp.Ctor != someSym {
		t.Errorf("arm ctor = %v, want Some", cp.Ctor)
	}
	if got := cp.Args[0].Pat.Type().String(); got != "Int" {
		t.Errorf("payload pat type = %s, want Int", got)
	}
}

// An arm body that disagrees with the others is E0401.
func TestMatchArmMismatch(t *testing.T) {
	b := newTB()
	someSym := b.ctorSym("Some")
	noneSym := b.ctorSym("None")
	op, os := b.param("o", "", b.tapp(b.tn("Option"), b.tn("Int")))
	xp, xs := b.vp("x")
	m := b.matchx(b.ref(os),
		b.arm(b.ctorPatSym(someSym, b.ctorArg("", xp)), nil, b.ref(xs)),
		b.arm(b.ctorPatSym(noneSym), nil, b.slit("no")))
	f1, _ := b.fn("f", []syntax.Param{op}, b.tn("Int"), m)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// A wildcard arm binds nothing and a guard must be Bool.
func TestMatchGuard(t *testing.T) {
	b := newTB()
	ip, is := b.param("i", "", b.tn("Int"))
	m := b.matchx(b.ref(is),
		b.arm(b.wp(), b.bin(syntax.BinLt, b.ref(is), b.ilit("0")), b.ilit("1")),
		b.arm(b.wp(), nil, b.ilit("2")))
	f1, _ := b.fn("f", []syntax.Param{ip}, b.tn("Int"), m)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// A non-Bool guard is E0401.
func TestMatchGuardNonBool(t *testing.T) {
	b := newTB()
	ip, is := b.param("i", "", b.tn("Int"))
	m := b.matchx(b.ref(is),
		b.arm(b.wp(), b.ilit("1"), b.ilit("1")),
		b.arm(b.wp(), nil, b.ilit("2")))
	f1, _ := b.fn("f", []syntax.Param{ip}, b.tn("Int"), m)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// A tuple pattern destructures a tuple subject.
func TestTuplePat(t *testing.T) {
	b := newTB()
	pp, ps := b.param("p", "", b.ttup(b.tn("Int"), b.tn("String")))
	xp, xs := b.vp("x")
	yp, ys := b.vp("y")
	m := b.matchx(b.ref(ps),
		b.arm(b.tupPat(xp, yp), nil, b.ref(xs)))
	f1, _ := b.fn("f", []syntax.Param{pp}, b.tn("Int"), m)
	_ = ys

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// A list pattern binds the element type.
func TestListPat(t *testing.T) {
	b := newTB()
	lp, ls := b.param("l", "", b.tapp(b.tn("List"), b.tn("Int")))
	hp, hs := b.vp("h")
	rp, _ := b.vp("rest")
	m := b.matchx(b.ref(ls),
		b.arm(b.listPat(rp, hp), nil, b.ref(hs)))
	f1, _ := b.fn("f", []syntax.Param{lp}, b.tn("Int"), m)

	f, diags := b.check(nil, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	l := d.Body.(*Match).Arms[0].Pat.(*ListPat)
	if got := l.Elems[0].Type().String(); got != "Int" {
		t.Errorf("list elem pat type = %s, want Int", got)
	}
}

// An or-pattern binds every alternative at the same type.
func TestOrPat(t *testing.T) {
	b := newTB()
	circleV, circleSym := b.variant("Circle", b.fieldPos(b.tn("Float")))
	emptyV, _ := b.variant("Empty")
	shapeD, shape := b.typeDecl("Shape", nil, b.sumBody(circleV, emptyV))
	sp, ss := b.param("s", "", b.tnBound("Shape", shape))
	wp, _ := b.vp("w")
	m := b.matchx(b.ref(ss),
		b.arm(b.orPat(
			b.ctorPatSym(circleSym, b.ctorArg("", wp)),
			b.litp(b.ilit("0"))),
			nil, b.ilit("1")),
		b.arm(b.wp(), nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{sp}, b.tn("Int"), m)

	_, diags := b.check(nil, shapeD, f1)
	requireCodes(t, diags, "E0401") // Shape vs Int literal — mismatch arm
}

// An as-pattern binds the whole matched value.
func TestAsPat(t *testing.T) {
	b := newTB()
	someSym := b.ctorSym("Some")
	op, os := b.param("o", "", b.tapp(b.tn("Option"), b.tn("Int")))
	xp, _ := b.vp("x")
	m := b.matchx(b.ref(os),
		b.arm(b.asPat(b.ctorPatSym(someSym, b.ctorArg("", xp)), "whole"),
			nil, b.ilit("1")),
		b.arm(b.wp(), nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{op}, b.tn("Int"), m)

	f, diags := b.check(nil, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	ap := d.Body.(*Match).Arms[0].Pat.(*AsPat)
	if got := ap.Pat.Type().String(); got != "Option Int" {
		t.Errorf("as-pat type = %s, want `Option Int`", got)
	}
}

// A literal pattern must match the subject's type.
func TestLitPatMismatch(t *testing.T) {
	b := newTB()
	sp, ss := b.param("s", "", b.tn("String"))
	m := b.matchx(b.ref(ss),
		b.arm(b.litp(b.ilit("1")), nil, b.ilit("1")),
		b.arm(b.wp(), nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{sp}, b.tn("Int"), m)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// A constructor-pattern argument over the payload arity is a
// mismatch; a labeled pattern argument resolves by name.
func TestCtorPatLabeled(t *testing.T) {
	b := newTB()
	transferV, transferSym := b.variant("Transfer",
		b.fieldPos(b.tn("Int")),
		b.fieldLabeled("to", b.tn("String")))
	otherV, _ := b.variant("Other")
	cmdD, cmd := b.typeDecl("Cmd", nil, b.sumBody(transferV, otherV))
	cp, cs := b.param("c", "", b.tnBound("Cmd", cmd))
	n, _ := b.vp("n")
	to, toS := b.vp("to")
	m := b.matchx(b.ref(cs),
		b.arm(b.ctorPatSym(transferSym,
			b.ctorArg("", n),
			b.ctorArg("to", to)), nil, b.ref(toS)),
		b.arm(b.wp(), nil, b.slit("?")))
	f1, _ := b.fn("f", []syntax.Param{cp}, b.tn("String"), m)

	_, diags := b.check(nil, cmdD, f1)
	requireCodes(t, diags)
}

// ---- try and the Result pipe -------------------------------------------

// `try e` unwraps `Result a e` to `a` when the enclosing function
// returns a matching Result.
func TestTry(t *testing.T) {
	b := newTB()
	resP, resSym := b.vp("res")
	lr := b.letd(resP, b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")), nil)
	okSym := b.ctorSym("Ok")
	vp, vs := b.vp("v")
	up := b.unitParam()
	body := b.block(
		b.letStmt(vp, b.try(b.ref(resSym))),
		b.app(b.ref(okSym), b.arg(b.ref(vs))))
	main, _ := b.fn("main", []syntax.Param{up},
		b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")), body)

	_, diags := b.check(nil, lr, main)
	requireCodes(t, diags)
}

// `try` with no enclosing function is E0410.
func TestTryNoBoundary(t *testing.T) {
	b := newTB()
	resP, resSym := b.vp("res")
	lr := b.letd(resP, b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")), nil)
	xp, _ := b.vp("x")
	lx := b.letd(xp, nil, b.try(b.ref(resSym)))

	_, diags := b.check(nil, lr, lx)
	requireCodes(t, diags, "E0410")
}

// `try` in a function returning a non-Result is E0410.
func TestTryNonResultBoundary(t *testing.T) {
	b := newTB()
	resP, resSym := b.vp("res")
	lr := b.letd(resP, b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")), nil)
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.try(b.ref(resSym)))

	_, diags := b.check(nil, lr, main)
	requireCodes(t, diags, "E0410")
}

// The tried error channel must equal the function's — E0411.
func TestTryErrMismatch(t *testing.T) {
	b := newTB()
	errD, myErr := b.typeDecl("MyErr", nil, nil)
	resP, resSym := b.vp("res")
	lr := b.letd(resP, b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")), nil)
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up},
		b.tapp(b.tn("Result"), b.tn("Int"), b.tnBound("MyErr", myErr)),
		b.block(b.letStmt(func() syntax.Pat { p, _ := b.vp("v"); return p }(),
			b.try(b.ref(resSym))), b.ilit("0")))

	_, diags := b.check(nil, errD, lr, main)
	requireCodes(t, diags, "E0411", "E0401")
}

// `try` inside `go {}` cannot escape — E0702.
func TestTryAcrossGo(t *testing.T) {
	b := newTB()
	resP, resSym := b.vp("res")
	lr := b.letd(resP, b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")), nil)
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up},
		b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")),
		b.block(
			b.goexpr(b.block(b.try(b.ref(resSym)))),
			b.ilit("0")))

	_, diags := b.check(nil, lr, main)
	// E0702: try cannot cross the go boundary; E0701: the recovered
	// go body is Int, not Unit; E0401: the block's Int is not Result.
	requireCodes(t, diags, "E0702", "E0701", "E0401")
}

// `x |?> f` is `Result b e` when `f : a -> Result b e`.
func TestPipeTry(t *testing.T) {
	b := newTB()
	rp, rs := b.param("r", "", b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")))
	fp, fs := b.param("f", "",
		b.tfn(b.tapp(b.tn("Result"), b.tn("String"), b.tn("GoError")), b.tp(b.tn("Int"))))
	pipe, _ := b.fn("pipe", []syntax.Param{rp, fp},
		b.tapp(b.tn("Result"), b.tn("String"), b.tn("GoError")),
		b.bin(syntax.BinPipeTry, b.ref(rs), b.ref(fs)))

	f, diags := b.check(nil, pipe)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "Result String GoError" {
		t.Errorf("|?> type = %s, want `Result String GoError`", got)
	}
}

// ---- go, for, select ----------------------------------------------------

// A non-Unit `go` body is E0701.
func TestGoExprNonUnit(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(b.goexpr(b.block(b.ilit("1"))), b.ulit()))

	_, diags := b.check(nil, main)
	requireCodes(t, diags, "E0701")
}

// `for x in list` binds the element; `for x in chan` too.
func TestForList(t *testing.T) {
	b := newTB()
	lp, ls := b.param("l", "", b.tapp(b.tn("List"), b.tn("Int")))
	xp, xs := b.vp("x")
	body := b.block(b.bin(syntax.BinAdd, b.ref(xs), b.ilit("1")))
	_ = body
	f1, _ := b.fn("f", []syntax.Param{lp}, b.tn("Unit"),
		b.block(b.forx(xp, b.ref(ls), b.block(b.ulit())), b.ulit()))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// A non-iterable `for` is E0401.
func TestForNonIterable(t *testing.T) {
	b := newTB()
	ip, is := b.param("i", "", b.tn("Int"))
	xp, _ := b.vp("x")
	f1, _ := b.fn("f", []syntax.Param{ip}, b.tn("Unit"),
		b.block(b.forx(xp, b.ref(is), b.block(b.ulit())), b.ulit()))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// A channel `for` binds the element type.
func TestForChan(t *testing.T) {
	b := newTB()
	cp, cs := b.param("ch", "", b.tapp(b.tn("Chan"), b.tn("String")))
	xp, xs := b.vp("x")
	f1, _ := b.fn("f", []syntax.Param{cp}, b.tn("Unit"),
		b.block(b.forx(xp, b.ref(cs), b.block(b.letStmt(
			func() syntax.Pat { p, _ := b.vp("s2"); return p }(),
			b.bin(syntax.BinAdd, b.ref(xs), b.slit("!"))))),
			b.ulit()))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401") // String + String — not numeric
}

// `select` arms unify on one result type; a second default is E0703.
func TestSelectExpr(t *testing.T) {
	b := newTB()
	cp, cs := b.param("ch", "", b.tapp(b.tn("Chan"), b.tn("Int")))
	noneSym := b.ctorSym("None")
	bp, bs := b.vp("b")
	sel := b.selectx(
		b.recvArm(bp, b.ref(cs), b.ref(bs)),
		b.defArm(b.app(b.ref(noneSym))))
	f1, _ := b.fn("f", []syntax.Param{cp},
		b.tapp(b.tn("Option"), b.tn("Int")), sel)
	_ = bs

	f, diags := b.check(nil, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	s := d.Body.(*SelectExpr)
	if got := s.Arms[0].(*RecvArm).Bind.Type().String(); got != "Option Int" {
		t.Errorf("recv bind type = %s, want `Option Int`", got)
	}
}

// Two defaults are E0703.
func TestSelectTwoDefaults(t *testing.T) {
	b := newTB()
	cp, cs := b.param("ch", "", b.tapp(b.tn("Chan"), b.tn("Int")))
	bp, _ := b.vp("b")
	sel := b.selectx(
		b.recvArm(bp, b.ref(cs), b.ilit("0")),
		b.defArm(b.ilit("0")),
		b.defArm(b.ilit("1")))
	f1, _ := b.fn("f", []syntax.Param{cp}, b.tn("Int"), sel)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0703")
}

// A send arm on a receive-only channel is E0401.
func TestSelectSendOnRecv(t *testing.T) {
	b := newTB()
	cp, cs := b.param("ch", "", b.tapp(b.tn("RecvChan"), b.tn("Int")))
	sel := b.selectx(
		b.sendArm(b.ref(cs), b.ilit("1"), b.ilit("0")),
		b.defArm(b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{cp}, b.tn("Int"), sel)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// ---- lists, tuples, records, interpolation -------------------------------

// A list literal is `List a` over one element type; a spread tail
// splices a List.
func TestListLit(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	d := b.letd(xp, b.tapp(b.tn("List"), b.tn("Int")),
		b.list(b.ilit("1"), b.ilit("2")))

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	if got := ld.Value.Type().String(); got != "List Int" {
		t.Errorf("list type = %s, want `List Int`", got)
	}
}

// A spread tail must be a List (or Go.Slice) of the element type.
func TestListLitSpread(t *testing.T) {
	b := newTB()
	xsP, xsSym := b.vp("xs")
	lxs := b.letd(xsP, b.tapp(b.tn("List"), b.tn("Int")), nil)
	yp, _ := b.vp("y")
	d := b.letd(yp, b.tapp(b.tn("List"), b.tn("Int")),
		b.list(b.ilit("1"), b.spread(b.ref(xsSym))))

	_, diags := b.check(nil, lxs, d)
	requireCodes(t, diags)
}

// A mid-list spread is E0505.
func TestListLitSpreadNotLast(t *testing.T) {
	b := newTB()
	xsP, xsSym := b.vp("xs")
	lxs := b.letd(xsP, b.tapp(b.tn("List"), b.tn("Int")), nil)
	yp, _ := b.vp("y")
	d := b.letd(yp, b.tapp(b.tn("List"), b.tn("Int")),
		b.list(b.spread(b.ref(xsSym)), b.ilit("1")))

	_, diags := b.check(nil, lxs, d)
	requireCodes(t, diags, "E0505")
}

// `(a, b)` is a Tuple.
func TestTupleLit(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	d := b.letd(xp, b.ttup(b.tn("Int"), b.tn("String")),
		b.tuple(b.ilit("1"), b.slit("s")))

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	if got := ld.Value.Type().String(); got != "(Int, String)" {
		t.Errorf("tuple type = %s, want `(Int, String)`", got)
	}
}

// A record literal checked against its record type gets field
// symbols; a missing field is E0401.
func TestRecordLit(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("P", nil,
		b.recBody(b.fieldNamed("x", b.tn("Int")), b.fieldNamed("y", b.tn("Int"))))
	pp, _ := b.vp("p")
	d := b.letd(pp, b.tnBound("P", rec),
		b.recLit([]syntax.FieldInit{
			b.fieldInit("x", b.ilit("1")),
			b.fieldInit("y", b.ilit("2")),
		}, nil))

	f, diags := b.check(nil, recD, d)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	rl := ld.Value.(*RecordLit)
	if len(rl.Fields) != 2 || rl.Fields[0].Sym == nil {
		t.Fatalf("record fields not resolved: %+v", rl.Fields)
	}
	if got := rl.Type().String(); got != "P" {
		t.Errorf("record type = %s, want P", got)
	}
}

func TestRecordLitMissingField(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("P", nil,
		b.recBody(b.fieldNamed("x", b.tn("Int")), b.fieldNamed("y", b.tn("Int"))))
	pp, _ := b.vp("p")
	d := b.letd(pp, b.tnBound("P", rec),
		b.recLit([]syntax.FieldInit{b.fieldInit("x", b.ilit("1"))}, nil))

	_, diags := b.check(nil, recD, d)
	requireCodes(t, diags, "E0401")
}

// `{ base with f = e }` updates a record.
func TestRecordWith(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("P", nil,
		b.recBody(b.fieldNamed("x", b.tn("Int")), b.fieldNamed("y", b.tn("Int"))))
	pp, ps := b.vp("p")
	lp := b.letd(pp, b.tnBound("P", rec), nil)
	qp, _ := b.vp("q")
	d := b.letd(qp, b.tnBound("P", rec),
		b.recLit([]syntax.FieldInit{b.fieldInit("y", b.ilit("9"))}, b.ref(ps)))

	_, diags := b.check(nil, recD, lp, d)
	requireCodes(t, diags)
}

// `p.x` resolves the field symbol and index.
func TestSelectRecordField(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("P", nil,
		b.recBody(b.fieldNamed("x", b.tn("Int")), b.fieldNamed("y", b.tn("Int"))))
	pp, ps := b.vp("p")
	lp := b.letd(pp, b.tnBound("P", rec), nil)
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Int"),
		b.sel(b.ref(ps), nil, "x"))

	f, diags := b.check(nil, recD, lp, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	me := d.Body.(*MemberExpr)
	if me.Index != 0 {
		t.Errorf("field index = %d, want 0", me.Index)
	}
	if me.Sym == nil || me.Sym.Name != "x" {
		t.Errorf("field sym = %v, want x", me.Sym)
	}
	if got := me.Type().String(); got != "Int" {
		t.Errorf("field type = %s, want Int", got)
	}
}

// Selecting a non-existent field is E0401.
func TestSelectMissingField(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("P", nil,
		b.recBody(b.fieldNamed("x", b.tn("Int"))))
	pp, ps := b.vp("p")
	lp := b.letd(pp, b.tnBound("P", rec), nil)
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Int"),
		b.sel(b.ref(ps), nil, "z"))

	_, diags := b.check(nil, recD, lp, f1)
	requireCodes(t, diags, "E0401")
}

// `"hello ${x}"` interpolates to String.
func TestStrInterp(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	d := b.letd(xp, b.tn("String"),
		b.strInterp(b.strText("hi "), b.strExpr(b.ilit("1"))))

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	if got := ld.Value.Type().String(); got != "String" {
		t.Errorf("interp type = %s, want String", got)
	}
}

// ---- test declarations ---------------------------------------------------

// `test "x" { () }` is a Unit test.
func TestTestDeclUnit(t *testing.T) {
	b := newTB()
	d := b.testd("x", b.block(b.ulit()))

	_, diags := b.check(nil, d)
	requireCodes(t, diags)
}

// `test "x" { 1 }` is E0801 — a test body is Unit or Result Unit e.
func TestTestDeclNonUnit(t *testing.T) {
	b := newTB()
	d := b.testd("x", b.block(b.ilit("1")))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0801")
}

// `test "x" { Ok () }` is a Result-returning test — allowed.
func TestTestDeclResult(t *testing.T) {
	b := newTB()
	okSym := b.ctorSym("Ok")
	d := b.testd("x", b.block(b.app(b.ref(okSym), b.arg(b.ulit()))))

	_, diags := b.check(nil, d)
	requireCodes(t, diags)
}

// `try` inside a test escapes to the test itself.
func TestTestDeclTry(t *testing.T) {
	b := newTB()
	resP, resSym := b.vp("res")
	lr := b.letd(resP, b.tapp(b.tn("Result"), b.tn("Int"), b.tn("GoError")), nil)
	vp, _ := b.vp("v")
	d := b.testd("t", b.block(b.letStmt(vp, b.try(b.ref(resSym))), b.ulit()))

	_, diags := b.check(nil, lr, d)
	requireCodes(t, diags)
}

// assert_eq works in tests; its operands follow `==`'s rules.
func TestAssertEq(t *testing.T) {
	b := newTB()
	ae := b.builtinSym("assert_eq", "")
	d := b.testd("t", b.block(
		b.app(b.ref(ae), b.arg(b.ilit("1")), b.arg(b.ilit("2")))))

	_, diags := b.check(nil, d)
	requireCodes(t, diags)
}

// A record pattern binds each named field, punned or not.
func TestRecordPat(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("P", nil,
		b.recBody(b.fieldNamed("x", b.tn("Int")), b.fieldNamed("y", b.tn("Int"))))
	pp, ps := b.param("p", "", b.tnBound("P", rec))
	xp, xs := b.vp("x")
	m := b.matchx(b.ref(ps),
		b.arm(b.recPat(false,
			b.fieldPat("x", xp),
			b.fieldPatPunned("y")), nil, b.ref(xs)),
		b.arm(b.wp(), nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{pp}, b.tn("Int"), m)

	f, diags := b.check(nil, recD, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	rp := d.Body.(*Match).Arms[0].Pat.(*RecordPat)
	if rp.Fields[0].Sym == nil || rp.Fields[0].Sym.Name != "x" {
		t.Errorf("record field pat sym = %v, want x", rp.Fields[0].Sym)
	}
	if rp.Fields[0].Index != 0 || rp.Fields[1].Index != 1 {
		t.Errorf("field indices = %d,%d, want 0,1",
			rp.Fields[0].Index, rp.Fields[1].Index)
	}
	if got := rp.Fields[1].Pat.Type().String(); got != "Int" {
		t.Errorf("punned field type = %s, want Int", got)
	}
}

// An unknown field in a record pattern is E0401.
func TestRecordPatBadField(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("P", nil,
		b.recBody(b.fieldNamed("x", b.tn("Int"))))
	pp, ps := b.param("p", "", b.tnBound("P", rec))
	m := b.matchx(b.ref(ps),
		b.arm(b.recPat(false, b.fieldPatPunned("z")), nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{pp}, b.tn("Int"), m)

	_, diags := b.check(nil, recD, f1)
	requireCodes(t, diags, "E0401")
}

// ---- aliases, generics, annotations --------------------------------------

// `type M = Int` is transparent: `let x: M = 1` is an Int.
func TestAliasDecl(t *testing.T) {
	b := newTB()
	mD, mSym := b.typeDecl("M", nil, b.aliasBody(b.tn("Int")))
	xp, xs := b.vp("x")
	lx := b.letd(xp, b.tnBound("M", mSym), b.ilit("1"))

	f, diags := b.check(nil, mD, lx)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	if got := ld.Pat.Type().String(); got != "Int" {
		t.Errorf("aliased let type = %s, want Int", got)
	}
	_ = xs
}

// An unannotated record literal resolves its record by field set and
// instantiates the type parameters.
func TestRecordGeneric(t *testing.T) {
	b := newTB()
	pairD, _ := b.typeDecl("Pair", []string{"a", "b"},
		b.recBody(
			b.fieldNamed("fst", b.tvar("a")),
			b.fieldNamed("snd", b.tvar("b"))))
	pp, _ := b.vp("p")
	d := b.letd(pp, nil,
		b.recLit([]syntax.FieldInit{
			b.fieldInit("fst", b.ilit("1")),
			b.fieldInit("snd", b.slit("s")),
		}, nil))

	f, diags := b.check(nil, pairD, d)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	if got := ld.Value.Type().String(); got != "Pair Int String" {
		t.Errorf("generic record type = %s, want `Pair Int String`", got)
	}
}

// `f ~x` with `x` in scope passes x by label (punned).
func TestPunnedArg(t *testing.T) {
	b := newTB()
	fp, fs := b.param("x", "x", b.tn("Int"))
	fDecl, fSym := b.fn("f", []syntax.Param{fp}, b.tn("Int"), b.ref(fs))
	xp, xs := b.vp("x")
	lx := b.letd(xp, b.tn("Int"), nil)
	yp, _ := b.vp("y")
	d := b.letd(yp, b.tn("Int"), b.app(b.ref(fSym), b.parg("x", xs)))

	f, diags := b.check(nil, fDecl, lx, d)
	requireCodes(t, diags)
	app := applyAt(findDecl[*LetDecl](f, 1).Value)
	if app.Call.Kind != CallDirect {
		t.Errorf("kind = %s, want CallDirect", app.Call.Kind)
	}
	// `~x` lands on parameter 0: the identity mapping is stored as nil.
	if app.Call.ArgToParam != nil {
		t.Errorf("ArgToParam = %v, want nil (identity)", app.Call.ArgToParam)
	}
	if app.Args[0].Label != "x" {
		t.Errorf("arg label = %q, want x", app.Args[0].Label)
	}
}

// `let y: Int = 1` inside a block unifies the annotation.
func TestLetAnnotated(t *testing.T) {
	b := newTB()
	yp, _ := b.vp("y")
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Unit"),
		b.block(b.letx(yp, b.tn("Int"), b.ilit("1")), b.ulit()))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}
