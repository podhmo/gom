package check

// Regression tests for the checker-recovery work: pipes against Go
// callees (variadic elements, placeholders, arity), GoError's
// synthetic Error method, pattern-variable equality in guards, and
// quiet metas that keep one diagnostic from echoing as E0407.

import (
	"testing"

	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// `x |> f` for a bare variadic Go callee of arity 1 types x as one
// variadic element — checked against the element type of the
// declared slice parameter, not the slice itself — and the pipe has
// the callee's return type.
func TestPipeGoVariadicElem(t *testing.T) {
	b := newTB()
	fmt := b.modSym("fmt", true)
	println := b.gofnSym("fmt", "Println")
	src := MapSource{
		println: vsig(types.Int, types.P(types.GoSlice(types.GoAny))),
	}
	up := b.unitParam()
	pipe := b.bin(syntax.BinPipeFwd, b.slit("hi"),
		b.member(fmt, println, "Println"))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"), pipe)

	f, diags := b.check(src, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "Int" {
		t.Errorf("pipe type = %s, want Int", got)
	}
}

// The back pipe follows the same convention: `Println BACKPIPE "hi"`
// is `Println("hi")`, not a spread call.
func TestPipeBackGoVariadicElem(t *testing.T) {
	b := newTB()
	fmt := b.modSym("fmt", true)
	println := b.gofnSym("fmt", "Println")
	src := MapSource{
		println: vsig(types.Int, types.P(types.GoSlice(types.GoAny))),
	}
	up := b.unitParam()
	pipe := b.bin(syntax.BinPipeBack,
		b.member(fmt, println, "Println"), b.slit("hi"))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"), pipe)

	f, diags := b.check(src, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "Int" {
		t.Errorf("pipe type = %s, want Int", got)
	}
}

// `x |> f(_)` fills the placeholder: the pipe's type is the call's
// result, and x is checked against the hole's parameter type.
func TestPipeGoHoleFill(t *testing.T) {
	b := newTB()
	fmt := b.modSym("fmt", true)
	println := b.gofnSym("fmt", "Println")
	src := MapSource{
		println: vsig(types.Int, types.P(types.GoSlice(types.GoAny))),
	}
	up := b.unitParam()
	call := b.goCall(fmt, println, "Println", b.hole())
	pipe := b.bin(syntax.BinPipeFwd, b.slit("a"), call)
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"), pipe)

	f, diags := b.check(src, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if got := d.Body.Type().String(); got != "Int" {
		t.Errorf("pipe type = %s, want Int", got)
	}
}

// A pipe into a saturated non-variadic Go call is the same
// over-application diagnostic the application form reports.
func TestPipeGoSaturatedOverApply(t *testing.T) {
	b := newTB()
	strconv := b.modSym("strconv", true)
	atoi := b.gofnSym("strconv", "Atoi")
	src := MapSource{
		atoi: sig(types.Int, types.P(types.String)),
	}
	up := b.unitParam()
	call := b.goCall(strconv, atoi, "Atoi", b.slit("42"))
	pipe := b.bin(syntax.BinPipeFwd, b.ilit("5"), call)
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"), pipe)

	_, diags := b.check(src, main)
	requireCodes(t, diags, "E0401")
}

// A pipe into a *partial* non-variadic Go call does not echo the
// arity error the partial application itself already reported —
// one E0401, not a second "takes 2, got 2".
func TestPipeGoPartialQuiet(t *testing.T) {
	b := newTB()
	strconv := b.modSym("strconv", true)
	format := b.gofnSym("strconv", "FormatInt")
	src := MapSource{
		format: sig(types.String, types.P(types.Int), types.P(types.Int)),
	}
	up := b.unitParam()
	call := b.goCall(strconv, format, "FormatInt", b.ilit("255"))
	pipe := b.bin(syntax.BinPipeBack, call, b.ilit("16"))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(b.letStmt(b.wp(), pipe), b.ulit()))

	_, diags := b.check(src, main)
	requireCodes(t, diags, "E0401")
}

// A pipe into a zero-argument Go callee reports the arity error
// rather than a confusing function-type mismatch.
func TestPipeGoZeroArity(t *testing.T) {
	b := newTB()
	os := b.modSym("os", true)
	getpid := b.gofnSym("os", "Getpid")
	src := MapSource{
		getpid: sig(types.Int),
	}
	up := b.unitParam()
	pipe := b.bin(syntax.BinPipeFwd, b.ilit("5"),
		b.member(os, getpid, "Getpid"))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"), pipe)

	_, diags := b.check(src, main)
	requireCodes(t, diags, "E0401")
}

// `e.Error()` on a GoError resolves through the checker's synthetic
// method — the one member the builtin `error` interface carries.
func TestGoErrorErrorMethod(t *testing.T) {
	b := newTB()
	ep, es := b.param("e", "", b.tn("GoError"))
	call := b.app(b.sel(b.ref(es), nil, "Error"), b.arg(b.ulit()))
	f1, _ := b.fn("f", []syntax.Param{ep}, b.tn("String"), call)

	f, diags := b.check(nil, f1)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	expectCall(t, a, CallGo, 0, 0, 0, nil)
	if got := a.Type().String(); got != "String" {
		t.Errorf("e.Error() type = %s, want String", got)
	}
}

// `Pair a b if a == b` — the pattern binds a and b before the guard
// runs, so the equality constrains the shared Int and nothing is
// left for E0407.
func TestMatchGuardPatternVarEq(t *testing.T) {
	b := newTB()
	pairV, pairSym := b.variant("Pair",
		b.fieldPos(b.tn("Int")), b.fieldPos(b.tn("Int")))
	noneV, _ := b.variant("None")
	pDecl, pSym := b.typeDecl("P", nil, b.sumBody(pairV, noneV))
	pp, ps := b.param("p", "", b.tnBound("P", pSym))
	ap, as := b.vp("a")
	bp, bs := b.vp("b")
	m := b.matchx(b.ref(ps),
		b.arm(b.ctorPatSym(pairSym, b.ctorArg("", ap), b.ctorArg("", bp)),
			b.bin(syntax.BinEq, b.ref(as), b.ref(bs)), b.ref(as)),
		b.arm(b.wp(), nil, b.ilit("0")))
	f1, _ := b.fn("f", []syntax.Param{pp}, b.tn("Int"), m)

	_, diags := b.check(nil, pDecl, f1)
	requireCodes(t, diags)
}

// A constructor pattern against a non-variant subject reports the
// mismatch once — the instantiation arguments stay quiet rather than
// surfacing an E0407 each.
func TestCtorPatSubjectMismatchQuiet(t *testing.T) {
	b := newTB()
	someSym := b.ctorSym("Some")
	op, os := b.param("o", "", b.ttup(b.tn("String"), b.tn("Bool")))
	xp, _ := b.vp("x")
	m := b.matchx(b.ref(os),
		b.arm(b.ctorPatSym(someSym, b.ctorArg("", xp)), nil, b.ulit()),
		b.arm(b.wp(), nil, b.ulit()))
	f1, _ := b.fn("f", []syntax.Param{op}, b.tn("Unit"), m)

	_, diags := b.check(nil, f1)
	requireCodes(t, diags, "E0401")
}

// A stray `_` reports E0504 only — the recovery meta is quiet.
func TestStrayHoleQuiet(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(b.letStmt(b.wp(), b.hole()), b.ulit()))

	_, diags := b.check(nil, main)
	requireCodes(t, diags, "E0504")
}
