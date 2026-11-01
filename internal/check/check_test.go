package check

import (
	"testing"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// ---- call classification ---------------------------------------------

// A saturated call of a declared function is CallDirect with the
// declared arity; the identity ArgToParam stays nil.
func TestCallDirect(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	yp, ys := b.param("y", "", b.tn("Int"))
	add, addSym := b.fn("add", []syntax.Param{xp, yp}, b.tn("Int"),
		b.bin(syntax.BinAdd, b.ref(xs), b.ref(ys)))
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(addSym), b.arg(b.ilit("1")), b.arg(b.ilit("2"))))

	f, diags := b.check(nil, add, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	expectCall(t, a, CallDirect, 2, 2, 0, nil)
	if a.Call.Callee != addSym {
		t.Errorf("callee = %v, want the add symbol", a.Call.Callee)
	}
	if got := a.Type().String(); got != "Int" {
		t.Errorf("apply type = %s, want Int", got)
	}
}

// Recursion: the function's own name resolves inside its body.
func TestCallDirectRecursive(t *testing.T) {
	b := newTB()
	np, ns := b.param("n", "", b.tn("Int"))
	var factSym *resolve.Symbol
	cond := b.bin(syntax.BinLt, b.ref(ns), b.ilit("1"))
	rec := b.bin(syntax.BinMul, b.ref(ns), nil) // placeholder; set below
	fact := b.app(nil, b.arg(b.bin(syntax.BinSub, b.ref(ns), b.ilit("1"))))
	rec.Y = fact
	factDecl, s := b.fn("fact", []syntax.Param{np}, b.tn("Int"),
		b.ifx(cond, b.ilit("1"), rec))
	factSym = s
	fact.Fn = b.ref(factSym)

	f, diags := b.check(nil, factDecl)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	a := firstApply(d)
	expectCall(t, a, CallDirect, 1, 1, 0, nil)
	if a.Call.Callee != factSym {
		t.Errorf("callee = %v, want fact", a.Call.Callee)
	}
}

// Under-application of a known callee is CallPartial; the remaining
// parameters become the result function type.
func TestCallPartial(t *testing.T) {
	b := newTB()
	accountD, account := b.typeDecl("Account", nil, nil)
	fp, _ := b.param("from", "", b.tnBound("Account", account))
	tp, _ := b.param("to", "to", b.tnBound("Account", account))
	ap, _ := b.param("amount", "amount", b.tn("Int"))
	transfer, transferSym := b.fn("transfer",
		[]syntax.Param{fp, tp, ap}, b.tn("Unit"), b.block(b.ulit()))

	// let alice : Account; let bob : Account
	aliceP, aliceSym := b.vp("alice")
	bobP, bobSym := b.vp("bob")
	la := b.letd(aliceP, b.tnBound("Account", account), nil)
	lb := b.letd(bobP, b.tnBound("Account", account), nil)

	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up},
		b.tfn(b.tn("Unit"), b.tpL("amount", b.tn("Int"))),
		b.app(b.ref(transferSym),
			b.arg(b.ref(aliceSym)),
			b.larg("to", b.ref(bobSym))))

	f, diags := b.check(nil, accountD, la, lb, transfer, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	expectCall(t, a, CallPartial, 3, 2, 1, nil)
	// The result type keeps the missing labeled parameter.
	if got := a.Type().String(); got != "~amount: Int -> Unit" {
		t.Errorf("partial type = %s, want `~amount: Int -> Unit`", got)
	}
	// Args stay in source (evaluation) order.
	if len(a.Args) != 2 {
		t.Fatalf("args = %d, want 2", len(a.Args))
	}
	if a.Args[1].Label != "to" {
		t.Errorf("arg[1].Label = %q, want to", a.Args[1].Label)
	}
}

// Labeled arguments may arrive out of declaration order; ArgToParam
// records the permutation while Args keep evaluation order.
func TestCallLabeledReorder(t *testing.T) {
	b := newTB()
	accountD, account := b.typeDecl("Account", nil, nil)
	fp, _ := b.param("from", "", b.tnBound("Account", account))
	tp, _ := b.param("to", "to", b.tnBound("Account", account))
	ap, _ := b.param("amount", "amount", b.tn("Int"))
	transfer, transferSym := b.fn("transfer",
		[]syntax.Param{fp, tp, ap}, b.tn("Unit"), b.block(b.ulit()))
	bobP, bobSym := b.vp("bob")
	lb := b.letd(bobP, b.tnBound("Account", account), nil)

	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.app(b.ref(transferSym),
			b.larg("amount", b.ilit("100")),
			b.larg("to", b.ref(bobSym)),
			b.arg(b.ref(bobSym))))

	f, diags := b.check(nil, accountD, lb, transfer, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	expectCall(t, a, CallDirect, 3, 3, 0, []int{2, 1, 0})
	if a.Args[0].Label != "amount" || a.Args[1].Label != "to" || a.Args[2].Label != "" {
		t.Errorf("args labels = %q,%q,%q — source order must be kept",
			a.Args[0].Label, a.Args[1].Label, a.Args[2].Label)
	}
}

// An unknown label is E0406; recovery still emits a valid CallInfo —
// the regression shape for an under-applied call with an
// unresolvable argument (which must not panic Apply.Validate).
func TestCallUnknownLabel(t *testing.T) {
	b := newTB()
	xp, _ := b.param("x", "", b.tn("Int"))
	yp, _ := b.param("y", "", b.tn("Int"))
	add, addSym := b.fn("add", []syntax.Param{xp, yp}, b.tn("Int"),
		b.ilit("0"))
	up := b.unitParam()
	bad := b.larg("wut", b.ilit("1"))
	main, _ := b.fn("main", []syntax.Param{up},
		b.tfn(b.tn("Int"), b.tp(b.tn("Int")), b.tp(b.tn("Int"))),
		b.app(b.ref(addSym), bad))

	f, diags := b.check(nil, add, main)
	requireCodes(t, diags, "E0406")
	if diags[0].Span != bad.Label.Sp {
		t.Errorf("E0406 span = %v, want the label span %v", diags[0].Span, bad.Label.Sp)
	}
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	// One applied arg on a two-parameter function: under-applied.
	expectCall(t, a, CallPartial, 2, 1, 1, nil)
}

// The same recovery in a saturated call yields CallDirect.
func TestCallUnknownLabelSaturated(t *testing.T) {
	b := newTB()
	xp, _ := b.param("x", "", b.tn("Int"))
	yp, _ := b.param("y", "", b.tn("Int"))
	add, addSym := b.fn("add", []syntax.Param{xp, yp}, b.tn("Int"),
		b.ilit("0"))
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(addSym),
			b.arg(b.ilit("1")), b.arg(b.ilit("2")), b.larg("wut", b.ilit("3"))))

	f, diags := b.check(nil, add, main)
	requireCodes(t, diags, "E0406")
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	expectCall(t, a, CallDirect, 2, 3, 0, nil)
}

// A label supplied twice is E0405 at the second label.
func TestCallDuplicateLabel(t *testing.T) {
	b := newTB()
	xp, _ := b.param("x", "", b.tn("Int"))
	lp, _ := b.param("l", "l", b.tn("Int"))
	f1, f1Sym := b.fn("f", []syntax.Param{xp, lp}, b.tn("Int"), b.ilit("0"))
	up := b.unitParam()
	dup := b.larg("l", b.ilit("2"))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(f1Sym), b.arg(b.ilit("1")), b.larg("l", b.ilit("1")), dup))

	_, diags := b.check(nil, f1, main)
	requireCodes(t, diags, "E0405")
	if diags[0].Span != dup.Label.Sp {
		t.Errorf("E0405 span = %v, want %v", diags[0].Span, dup.Label.Sp)
	}
}

// A labeled argument on a callee of unknown type is E0408.
func TestCallLabelOnUnknownFn(t *testing.T) {
	b := newTB()
	fp, fs := b.param("f", "", nil) // unannotated: meta
	apply, _ := b.fn("apply", []syntax.Param{fp}, b.tn("Int"),
		b.app(b.ref(fs), b.larg("to", b.ilit("1"))))
	_, diags := b.check(nil, apply)
	requireCodes(t, diags, "E0408")
}

// The declared arity is the parameter count of the signature, not of
// the flattened curried type: `mk () 2` over-applies into the
// returned function.
func TestCallDeclaredArity(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	mk, mkSym := b.fn("mk", []syntax.Param{up},
		b.tfn(b.tn("Int"), b.tp(b.tn("Int"))),
		b.lambda([]syntax.Param{
			func() syntax.Param { p, _ := b.param("x", "", b.tn("Int")); return p }(),
		}, b.ilit("0")))
	up2 := b.unitParam()
	// mk () 2 — applies () then an Int into the returned function.
	main, _ := b.fn("main", []syntax.Param{up2}, b.tn("Int"),
		b.app(b.ref(mkSym), b.arg(b.ulit()), b.arg(b.ilit("2"))))

	f, diags := b.check(nil, mk, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	expectCall(t, a, CallDirect, 1, 2, 0, nil)
	if got := a.Type().String(); got != "Int" {
		t.Errorf("over-applied type = %s, want Int", got)
	}
}

// `mk ()` alone is a saturated call returning a function.
func TestCallDeclaredAritySaturated(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	mk, mkSym := b.fn("mk", []syntax.Param{up},
		b.tfn(b.tn("Int"), b.tp(b.tn("Int"))),
		b.lambda([]syntax.Param{
			func() syntax.Param { p, _ := b.param("x", "", b.tn("Int")); return p }(),
		}, b.ilit("0")))
	up2 := b.unitParam()
	apply, _ := b.fn("apply", []syntax.Param{up2},
		b.tfn(b.tn("Int"), b.tp(b.tn("Int"))),
		b.app(b.ref(mkSym), b.arg(b.ulit())))

	f, diags := b.check(nil, mk, apply)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 1)
	a := applyAt(d.Body)
	expectCall(t, a, CallDirect, 1, 1, 0, nil)
	if got := a.Type().String(); got != "Int -> Int" {
		t.Errorf("type = %s, want `Int -> Int`", got)
	}
}

// An application of a function-*valued* variable is CallUnknown:
// curried application with no callee symbol. Extra arguments apply
// to the returned function one at a time.
func TestCallUnknown(t *testing.T) {
	b := newTB()
	fp, fs := b.param("f", "",
		b.tfn(b.tn("Int"), b.tp(b.tn("Int")), b.tp(b.tn("Int"))))
	xp, _ := b.param("x", "", b.tn("Int"))
	apply, _ := b.fn("apply", []syntax.Param{fp, xp}, b.tn("Int"),
		b.app(b.ref(fs), b.arg(b.ilit("1")), b.arg(b.ilit("2"))))

	f, diags := b.check(nil, apply)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	expectCall(t, a, CallUnknown, 2, 2, 0, nil)
	if a.Call.Callee != nil {
		t.Errorf("CallUnknown with callee %v", a.Call.Callee)
	}
}

// A constructor application is CallDirect, labeled payload slots
// included.
func TestCallCtor(t *testing.T) {
	b := newTB()
	circleV, circleSym := b.variant("Circle", b.fieldLabeled("r", b.tn("Float")))
	emptyV, _ := b.variant("Empty")
	shapeD, shape := b.typeDecl("Shape", nil, b.sumBody(circleV, emptyV))
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tnBound("Shape", shape),
		b.app(b.ref(circleSym), b.larg("r", b.flit("1.0"))))

	f, diags := b.check(nil, shapeD, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	expectCall(t, a, CallDirect, 1, 1, 0, nil)
	if a.Call.Callee != circleSym {
		t.Errorf("callee = %v, want Circle", a.Call.Callee)
	}
	if got := a.Type().String(); got != "Shape" {
		t.Errorf("ctor type = %s, want Shape", got)
	}
}

// The builtin-scope constructor `Some` classifies the same way.
func TestCallBuiltinCtor(t *testing.T) {
	b := newTB()
	someSym := b.ctorSym("Some")
	vp, vSym := b.vp("v")
	d := b.letd(vp, b.tapp(b.tn("Option"), b.tn("Int")),
		b.app(b.ref(someSym), b.arg(b.ilit("1"))))
	_ = vSym

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 0)
	a := applyAt(ld.Value)
	expectCall(t, a, CallDirect, 1, 1, 0, nil)
	if got := a.Type().String(); got != "Option Int" {
		t.Errorf("ctor type = %s, want `Option Int`", got)
	}
}
