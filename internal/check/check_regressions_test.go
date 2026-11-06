package check

// Regression tests for the P1-5 review: function-type unification
// with tail absorption, rigid type variables on value bindings,
// record literals against non-record expectations, and the select
// receive-arm pattern restriction.

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// ---- unifyFunc tail absorption ---------------------------------------

// `let twice = fn f -> fn x -> f (f x)` applied to a unary function
// typechecks: f's inferred `?x -> ?r` unifies with the lambda's
// `?n -> ?n` by prefix; the same mechanism binds `?r` when the
// argument is curried (the flattened tail folds into the result).
func TestUnifyFuncTailAbsorb(t *testing.T) {
	b := newTB()
	fp, fs := b.param("f", "", nil)
	xp, xs := b.param("x", "", nil)
	// fn f -> fn x -> f (f x)
	inner := b.lambda([]syntax.Param{xp},
		b.app(b.ref(fs), b.arg(b.app(b.ref(fs), b.arg(b.ref(xs))))))
	outer := b.lambda([]syntax.Param{fp}, inner)
	tp, twiceSym := b.vp("twice")
	twice := b.letd(tp, nil, outer)

	// twice (fn n -> n + 1) 0
	np, ns := b.param("n", "", nil)
	inc := b.lambda([]syntax.Param{np},
		b.bin(syntax.BinAdd, b.ref(ns), b.ilit("1")))
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(twiceSym), b.arg(inc), b.arg(b.ilit("0"))))

	f, diags := b.check(nil, twice, main)
	requireCodes(t, diags)
	// FnDecl 0 is the generalized `twice`; `main` is the second.
	d := findDecl[*FnDecl](f, 1)
	if got := d.Body.Type().String(); got != "Int" {
		t.Errorf("main body type = %s, want Int", got)
	}
}

// The same call shape against a binary lambda is a genuine occurs
// error — `twice (fn a b -> a)` needs `?x := ?b -> ?x` — reported as
// one E0401, not an arity mismatch plus a meta cascade.
func TestUnifyFuncTailOccurs(t *testing.T) {
	b := newTB()
	fp, fs := b.param("f", "", nil)
	xp, xs := b.param("x", "", nil)
	inner := b.lambda([]syntax.Param{xp},
		b.app(b.ref(fs), b.arg(b.app(b.ref(fs), b.arg(b.ref(xs))))))
	outer := b.lambda([]syntax.Param{fp}, inner)
	tp, twiceSym := b.vp("twice")
	twice := b.letd(tp, nil, outer)

	ap, as := b.param("a", "", nil)
	bp, _ := b.param("b", "", nil)
	konst := b.lambda([]syntax.Param{ap, bp}, b.ref(as))
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(twiceSym), b.arg(konst), b.arg(b.ilit("3"))))

	_, diags := b.check(nil, twice, main)
	requireCodes(t, diags, "E0401", "E0407")
	if !strings.Contains(diags[0].Message, "occurs") {
		t.Errorf("diag[0] = %q, want an occurs-check failure", diags[0].Message)
	}
}

// A meta result absorbs a labeled tail too: `x |> f` with
// `f : Int -> ~l: Int -> Int` has type `~l: Int -> Int` — the tail is
// the same type once flattened (spec section 2.1).
func TestUnifyFuncTailAbsorbLabeled(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	fp, fs := b.param("f", "",
		b.tfn(b.tn("Int"), b.tp(b.tn("Int")), b.tpL("l", b.tn("Int"))))
	pipe, _ := b.fn("pipe", []syntax.Param{xp, fp},
		b.tfn(b.tn("Int"), b.tpL("l", b.tn("Int"))),
		b.bin(syntax.BinPipeFwd, b.ref(xs), b.ref(fs)))

	f, diags := b.check(nil, pipe)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "~l: Int -> Int" {
		t.Errorf("pipe type = %s, want `~l: Int -> Int`", got)
	}
}

// Different label names in the same run position still mismatch —
// runs are paired by label, not by position.
func TestUnifyFuncLabelMismatch(t *testing.T) {
	b := newTB()
	gp, gs := b.param("g", "",
		b.tfn(b.tn("Int"), b.tp(b.tn("Int")), b.tpL("y", b.tn("Int"))))
	fp, _ := b.vp("f")
	f1, _ := b.fn("f1", []syntax.Param{gp}, b.tn("Unit"),
		b.block(
			// let f : Int -> ~x: Int -> Int = g   (g : ~y: ...)
			b.letx(fp,
				b.tfn(b.tn("Int"), b.tp(b.tn("Int")), b.tpL("x", b.tn("Int"))),
				b.ref(gs)),
			b.ulit()))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// ---- rigid variables on local bindings -------------------------------

// A constraint on a parameter annotated `a` reaches the rigid `a`
// itself: `x + 1` where `x : a` is a numeric mismatch on `a`, not a
// fresh meta that silently defaults. Previously the fn compiled and
// `f "s"` passed too — a soundness hole.
func TestRigidVarConstraint(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tvar("a"))
	f1, _ := b.fn("f", []syntax.Param{xp}, b.tn("Int"),
		b.bin(syntax.BinAdd, b.ref(xs), b.ilit("1")))

	_, diags := b.check(nil, f1)
	// The unify of `a` against the literal's numeric meta and the
	// numeric constraint on `a` both report the same root cause.
	for _, d := range diags {
		if d.Code != "E0401" {
			t.Fatalf("diags = %v, want only E0401", diagCodes(diags))
		}
	}
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for `x + 1` where x : a")
	}
}

// `==` on a rigid `a` is E0409 — the eq constraint lands on the
// variable directly, in a single diagnostic.
func TestRigidVarEq(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tvar("a"))
	f1, _ := b.fn("f", []syntax.Param{xp}, b.tn("Bool"),
		b.bin(syntax.BinEq, b.ref(xs), b.ref(xs)))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0409")
}

// A polymorphic-looking signature still instantiates at call sites:
// `id` is a fn, so its `a` quantifies.
func TestFnSignatureStillInstantiates(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tvar("a"))
	id, idSym := b.fn("id", []syntax.Param{xp}, b.tvar("a"), b.ref(xs))
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(idSym), b.arg(b.ilit("3"))))

	_, diags := b.check(nil, id, main)
	requireCodes(t, diags)
}

// A local binding of a rigid-typed value shares the variable: `let l
// = xs` where `xs : List a` must not ask for an annotation.
func TestRigidVarSharedByLocalBinding(t *testing.T) {
	b := newTB()
	xsp, xss := b.param("xs", "", b.tapp(b.tn("List"), b.tvar("a")))
	lp, ls := b.vp("l")
	f1, _ := b.fn("f", []syntax.Param{xsp}, b.tapp(b.tn("List"), b.tvar("a")),
		b.block(b.letStmt(lp, b.ref(xss)), b.ref(ls)))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// ---- record literal against a non-record expectation -----------------

// `let p : (Int, Int) = { x = 1, y = 2 }` is E0401 — the literal used
// to adopt the tuple type silently.
func TestRecordLitNonRecordWant(t *testing.T) {
	b := newTB()
	pp, _ := b.vp("p")
	d := b.letd(pp, b.ttup(b.tn("Int"), b.tn("Int")),
		b.recLit([]syntax.FieldInit{
			b.fieldInit("x", b.ilit("1")),
			b.fieldInit("y", b.ilit("2")),
		}, nil))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0401")
}

// A record literal checked against a real record type still works.
func TestRecordLitRecordWant(t *testing.T) {
	b := newTB()
	pointD, point := b.typeDecl("Point", nil,
		b.recBody(
			b.fieldNamed("x", b.tn("Int")),
			b.fieldNamed("y", b.tn("Int"))))
	pp, _ := b.vp("p")
	d := b.letd(pp, b.tnBound("Point", point),
		b.recLit([]syntax.FieldInit{
			b.fieldInit("x", b.ilit("1")),
			b.fieldInit("y", b.ilit("2")),
		}, nil))

	_, diags := b.check(nil, pointD, d)
	requireCodes(t, diags)
}

// ---- select receive arm patterns -------------------------------------

// `Some x <- ch` is E0704 — a receive arm binds a variable or `_`
// only (spec section 10.5). The typed arm keeps the VarPat/WildPat
// invariant with a wildcard.
func TestSelectRecvArmCtorPat(t *testing.T) {
	b := newTB()
	cp, cs := b.param("ch", "", b.tapp(b.tn("Chan"), b.tn("Int")))
	someSym := b.ctorSym("Some")
	xp, _ := b.vp("x")
	sel := b.selectx(
		b.recvArm(b.ctorPatSym(someSym, b.ctorArg("", xp)), b.ref(cs), b.ilit("1")),
		b.defArm(b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{cp}, b.tn("Int"), sel)

	f, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0704")
	d := findDecl[*FnDecl](f, 0)
	arm := d.Body.(*SelectExpr).Arms[0].(*RecvArm)
	if _, ok := arm.Bind.(*WildPat); !ok {
		t.Errorf("recv bind = %T, want *WildPat for recovery", arm.Bind)
	}
}

// `x <- ch` and `_ <- ch` stay legal.
func TestSelectRecvArmVarAndWild(t *testing.T) {
	b := newTB()
	cp, cs := b.param("ch", "", b.tapp(b.tn("Chan"), b.tn("Int")))
	bp, _ := b.vp("b")
	sel := b.selectx(
		b.recvArm(bp, b.ref(cs), b.ilit("1")),
		b.recvArm(b.wp(), b.ref(cs), b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{cp}, b.tn("Int"), sel)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// ---- smaller review fixes ---------------------------------------------

// `try <non-Result>` inside a lambda reports the mismatch once; the
// boundary's return unification must not mint unsolved metas behind
// it (was E0401 + E0407 twice).
func TestTryNonResultLambdaNoCascade(t *testing.T) {
	b := newTB()
	gp, _ := b.vp("g")
	up := b.unitParam()
	d := b.letd(gp, nil,
		b.lambda([]syntax.Param{up}, b.try(b.slit("s"))))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0401")
}

// `try 1` — the literal's numeric meta can never be a Result — also
// stays off the E0407 path: the numeric constraint and the Result
// expectation report, and nothing dangles.
func TestTryNumericLiteralNoCascade(t *testing.T) {
	b := newTB()
	gp, _ := b.vp("g")
	up := b.unitParam()
	d := b.letd(gp, nil,
		b.lambda([]syntax.Param{up}, b.try(b.ilit("1"))))

	_, diags := b.check(nil, d)
	for _, dg := range diags {
		if dg.Code != "E0401" {
			t.Fatalf("diags = %v, want only E0401", diagCodes(diags))
		}
	}
	if len(diags) == 0 {
		t.Fatal("expected diagnostics for `try 1`")
	}
}

// A missing argument value (recovery hole in hand-built input) must
// not hit `panic("unreachable")` in the Gom apply path.
func TestGomApplyMissingArgValue(t *testing.T) {
	b := newTB()
	xp, _ := b.param("x", "", b.tn("Int"))
	yp, _ := b.param("y", "", b.tn("Int"))
	add, addSym := b.fn("add", []syntax.Param{xp, yp}, b.tn("Int"), b.ilit("0"))
	up := b.unitParam()
	missing := syntax.Arg{Sp: b.sp()}
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(addSym), b.arg(b.ilit("1")), missing))

	f, diags := b.check(nil, add, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	expectCall(t, a, CallDirect, 2, 2, 2, 0, nil)
}

// A builtin-named symbol from a foreign package does not get the
// compiler's builtin signature: `pkg.Some` with no source signature
// is an unknown callee, not `a -> Option a`.
func TestForeignBuiltinName(t *testing.T) {
	b := newTB()
	foreignSome := b.ctorSym("Some")
	foreignSome.Package = "example.com/foreign"
	xp, _ := b.vp("x")
	d := b.letd(xp, nil,
		b.app(b.ref(foreignSome), b.arg(b.ilit("1"))))

	// Without a signature the callee's type is a meta; applying it
	// leaves an unsolved result meta — E0407. Had the builtin table
	// matched by name alone, `x` would be `Option Int` and clean.
	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0407")
}

// An unresolved constructor pattern binds its payload variables to
// Unit for recovery instead of leaving dangling metas.
func TestCtorPatUnresolvedNoDangling(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	yp, _ := b.vp("y")
	m := b.matchx(b.ref(xs),
		b.arm(&syntax.CtorPat{Sp: b.sp(), Ctor: b.id("Foo"),
			Args: []syntax.CtorArg{b.ctorArg("", yp)}},
			nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{xp}, b.tn("Int"), m)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// ---- E0414: division by a constant zero --------------------------------
//
// A `/` or `%` whose right operand is a constant zero emits Go the
// compiler itself rejects, so check reports E0414 — the same call Go
// makes: integer divisions are invalid whenever the divisor is a
// constant zero (`x / 0` included), Float ones only when the dividend
// is constant too (`x / 0.0` is IEEE arithmetic).

// An integer division by a literal zero is reported even with a
// variable dividend: `x / 0` cannot emit compilable Go.
func TestDivZeroIntVar(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	f1, _ := b.fn("f", []syntax.Param{xp}, b.tn("Int"),
		b.bin(syntax.BinDiv, b.ref(xs), b.ilit("0")))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0414")
}

// The full-constant division the fuzz canary found: `10 / 0` reports
// at the divisor.
func TestDivZeroIntLiteral(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Int"),
		b.bin(syntax.BinDiv, b.ilit("10"), b.ilit("0")))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0414")
}

// `%` shares the rule.
func TestDivZeroMod(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	f1, _ := b.fn("f", []syntax.Param{xp}, b.tn("Int"),
		b.bin(syntax.BinMod, b.ref(xs), b.ilit("0")))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0414")
}

// The divisor folds through unary `-` and constant arithmetic:
// `x / -0`, `x / 0x0`, `x / (3 - 3)`, `x / (2 * 0)` and
// `x / (10 % 10)` all reach zero.
func TestDivZeroFolded(t *testing.T) {
	for _, tc := range []struct {
		name string
		div  func(b *tb) syntax.Expr
	}{
		{"neg", func(b *tb) syntax.Expr { return b.unary(syntax.UnNeg, b.ilit("0")) }},
		{"hex", func(b *tb) syntax.Expr { return b.ilit("0x0") }},
		{"sub", func(b *tb) syntax.Expr {
			return b.bin(syntax.BinSub, b.ilit("3"), b.ilit("3"))
		}},
		{"mul", func(b *tb) syntax.Expr {
			return b.bin(syntax.BinMul, b.ilit("2"), b.ilit("0"))
		}},
		{"mod", func(b *tb) syntax.Expr {
			return b.bin(syntax.BinMod, b.ilit("10"), b.ilit("10"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := newTB()
			xp, xs := b.param("x", "", b.tn("Int"))
			f1, _ := b.fn("f", []syntax.Param{xp}, b.tn("Int"),
				b.bin(syntax.BinDiv, b.ref(xs), tc.div(b)))

			_, diags := b.check(nil, f1)
			requireCodes(t, diags, "E0414")
		})
	}
}

// A constant zero divisor under Float reports only when the dividend
// is a constant expression: `10.0 / 0.0` is an invalid Go constant
// but `x / 0.0` is IEEE division to +Inf. Float composite expressions
// fold exactly (`0.5 - 0.5` is a constant zero), so a constant
// dividend over one reports as well.
func TestDivZeroFloat(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	f1, _ := b.fn("f", []syntax.Param{up}, b.tn("Float"),
		b.bin(syntax.BinDiv, b.flit("10.0"), b.flit("0.0")))
	f2, _ := b.fn("g", []syntax.Param{up}, b.tn("Float"),
		b.bin(syntax.BinDiv, b.flit("10.0"),
			b.bin(syntax.BinSub, b.flit("0.5"), b.flit("0.5"))))

	_, diags := b.check(nil, f1, f2)
	requireCodes(t, diags, "E0414", "E0414")
}

func TestDivZeroFloatVarOK(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Float"))
	f1, _ := b.fn("f", []syntax.Param{xp}, b.tn("Float"),
		b.bin(syntax.BinDiv, b.ref(xs), b.flit("0.0")))
	f2, _ := b.fn("g", []syntax.Param{xp}, b.tn("Float"),
		b.bin(syntax.BinDiv, b.ref(xs),
			b.bin(syntax.BinSub, b.flit("0.5"), b.flit("0.5"))))

	_, diags := b.check(nil, f1, f2)
	requireCodes(t, diags)
}

// A variable divisor is never constant — `let q = 0` binds a Go
// variable, so `z / q` is a runtime panic (Go semantics), not a
// compile error.
func TestDivZeroLetBoundOK(t *testing.T) {
	b := newTB()
	zp, zs := b.param("z", "", b.tn("Int"))
	qp, qs := b.vp("q")
	f1, _ := b.fn("f", []syntax.Param{zp}, b.tn("Int"),
		b.block(
			b.letStmt(qp, b.ilit("0")),
			b.bin(syntax.BinDiv, b.ref(zs), b.ref(qs))))

	_, diags := b.check(nil, f1)
	requireCodes(t, diags)
}

// hasUnboundMeta must look through a Go named type's instantiation
// args the same way it does Con args — a `gopkg.Box ?m` element
// type still has unification work pending.
func TestHasUnboundMetaGoArgs(t *testing.T) {
	m := &types.Meta{ID: 7}
	g := &types.Go{Ref: types.NamedRef{Path: "gopkg", Name: "Box"}, Args: []types.Type{m}}
	if !hasUnboundMeta(g) {
		t.Errorf("hasUnboundMeta(%v): expected true — meta inside Go args", g)
	}
	g.Args = []types.Type{&types.Con{Name: "Int"}}
	if hasUnboundMeta(g) {
		t.Errorf("hasUnboundMeta(%v): expected false — all args concrete", g)
	}
}
