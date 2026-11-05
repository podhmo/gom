package check

// Go calling convention: tuple-style argument unpacking, variadic
// calls, `_` placeholders, methods, and Go.conv.

import (
	"testing"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// goCall builds `mod.name(args...)` where args are the unpacked Go
// arguments — written in surface form as one tuple argument.
func (t *tb) goCall(mod, fn *resolve.Symbol, selName string, args ...syntax.Expr) *syntax.Apply {
	var a syntax.Arg
	if len(args) == 0 {
		a = t.arg(t.ulit())
	} else if len(args) == 1 {
		a = t.arg(args[0])
	} else {
		a = t.arg(t.tuple(args...))
	}
	return t.app(t.member(mod, fn, selName), a)
}

// `strings.Repeat("x", 2)` — a Go call is CallGo with the surface
// tuple unpacked into the argument list.
func TestCallGo(t *testing.T) {
	b := newTB()
	strings := b.modSym("strings", true)
	repeat := b.gofnSym("strings", "Repeat")
	src := MapSource{
		repeat: sig(types.String, types.P(types.String), types.P(types.Int)),
	}
	up := b.unitParam()
	call := b.goCall(strings, repeat, "Repeat", b.slit("x"), b.ilit("2"))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("String"), call)

	f, diags := b.check(src, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	expectCall(t, a, CallGo, 2, 2, 0, nil)
	if a.Call.Variadic {
		t.Error("Repeat must not be variadic")
	}
	if got := a.Type().String(); got != "String" {
		t.Errorf("go call type = %s, want String", got)
	}
	if len(a.Args) != 2 {
		t.Fatalf("go args = %d, want 2 unpacked arguments", len(a.Args))
	}
}

// A single non-tuple argument stays one argument (no unpacking);
// `f()` passes none.
func TestCallGoArgForms(t *testing.T) {
	b := newTB()
	strconv := b.modSym("strconv", true)
	atoi := b.gofnSym("strconv", "Atoi")
	itoa := b.gofnSym("strconv", "Itoa")
	src := MapSource{
		atoi: sig(types.Int, types.P(types.String)),
		itoa: sig(types.String), // no parameters
	}
	up := b.unitParam()
	atoiCall := b.goCall(strconv, atoi, "Atoi", b.slit("42"))
	itoaCall := b.goCall(strconv, itoa, "Itoa")
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("String"),
		b.block(b.letStmt(func() syntax.Pat { p, _ := b.vp("n"); return p }(), atoiCall),
			itoaCall))

	f, diags := b.check(src, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	blk := d.Body.(*Block)
	a1 := applyAt(blk.Stmts[0].(*Let).Value)
	expectCall(t, a1, CallGo, 1, 1, 0, nil)
	a2 := applyAt(blk.Stmts[1])
	expectCall(t, a2, CallGo, 0, 0, 0, nil)
}

// A variadic Go callee accepts any number of trailing arguments; a
// `..spread` expands a list.
func TestCallGoVariadic(t *testing.T) {
	b := newTB()
	fmt := b.modSym("fmt", true)
	println := b.gofnSym("fmt", "Println")
	src := MapSource{
		println: vsig(types.Unit, types.P(types.GoSlice(types.String))),
	}
	up := b.unitParam()
	call := b.goCall(fmt, println, "Println",
		b.slit("a"), b.slit("b"), b.slit("c"))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"), call)

	f, diags := b.check(src, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	// Arity counts the declared parameter list (the one slice).
	expectCall(t, a, CallGo, 1, 3, 0, nil)
	if !a.Call.Variadic {
		t.Error("Println must be variadic")
	}
}

// The variadic tail may be empty.
func TestCallGoVariadicEmpty(t *testing.T) {
	b := newTB()
	fmt := b.modSym("fmt", true)
	println := b.gofnSym("fmt", "Println")
	src := MapSource{
		println: vsig(types.Unit, types.P(types.GoSlice(types.String))),
	}
	up := b.unitParam()
	call := b.goCall(fmt, println, "Println")
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"), call)

	_, diags := b.check(src, main)
	requireCodes(t, diags)
}

// `..xs` as the last argument spreads a list into the variadic tail.
func TestCallGoSpread(t *testing.T) {
	b := newTB()
	fmt := b.modSym("fmt", true)
	println := b.gofnSym("fmt", "Println")
	src := MapSource{
		println: vsig(types.Unit, types.P(types.GoSlice(types.String))),
	}
	xsP, xsSym := b.vp("xs")
	xs := b.letd(xsP, b.tapp(b.tn("List"), b.tn("String")), nil)
	up := b.unitParam()
	call := b.goCall(fmt, println, "Println", b.spread(b.ref(xsSym)))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"), call)

	f, diags := b.check(src, xs, main)
	// The stub's Unit result plus `..xs` spread into a slice
	// parameter is exactly the copied-List shape W0501 names —
	// the spread mechanics it verifies are unaffected.
	requireCodes(t, diags, "W0501")
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	expectCall(t, a, CallGo, 1, 1, 0, nil)
	if _, ok := a.Args[0].Value.(*Spread); !ok {
		t.Fatalf("arg 0 = %T, want *Spread", a.Args[0].Value)
	}
}

// The arity diagnostic compares the callee's declared parameter
// count against the supplied positional arguments — never against
// itself. `strings.Compare("x")` used to report a self-contradicting
// "takes 1 argument(s), got 1" (fuzz finding): the expected count
// subtracted the missing parameters instead of staying at the
// declaration's arity.
func TestCallGoArityMessage(t *testing.T) {
	b := newTB()
	strconv := b.modSym("strconv", true)
	os := b.modSym("os", true)
	fmt := b.modSym("fmt", true)
	format := b.gofnSym("strconv", "FormatInt")
	getpid := b.gofnSym("os", "Getpid")
	sprintf := b.gofnSym("fmt", "Sprintf")
	src := MapSource{
		format:  sig(types.String, types.P(types.Int), types.P(types.Int)),
		getpid:  sig(types.Int),
		sprintf: vsig(types.String, types.P(types.String), types.P(types.GoSlice(types.GoAny))),
	}

	for _, tt := range []struct {
		name string
		call *syntax.Apply
		ret  syntax.TypeExpr
		want string
	}{
		// Under-application of a fixed-arity callee.
		{"under", b.goCall(strconv, format, "FormatInt", b.ilit("255")), b.tn("String"),
			"the Go callee takes 2 argument(s), got 1"},
		// Over-application of the same callee.
		{"over", b.goCall(strconv, format, "FormatInt", b.ilit("1"), b.ilit("2"), b.ilit("3")), b.tn("String"),
			"the Go callee takes 2 argument(s), got 3"},
		// A niladic callee with a stray argument.
		{"niladic", b.goCall(os, getpid, "Getpid", b.ilit("5")), b.tn("Int"),
			"the Go callee takes 0 argument(s), got 1"},
		// A variadic callee missing its fixed prefix.
		{"variadic", b.goCall(fmt, sprintf, "Sprintf"), b.tn("String"),
			"the Go callee takes at least 1 argument(s), got 0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			up := b.unitParam()
			main, _ := b.fn("main", []syntax.Param{up}, tt.ret, tt.call)
			_, diags := b.check(src, main)
			requireCodes(t, diags, "E0401")
			if got := diags[0].Message; got != tt.want {
				t.Errorf("message = %q, want %q", got, tt.want)
			}
		})
	}
}

// A spread on a non-variadic callee is E0505.
func TestCallGoSpreadNonVariadic(t *testing.T) {
	b := newTB()
	strconv := b.modSym("strconv", true)
	atoi := b.gofnSym("strconv", "Atoi")
	src := MapSource{atoi: sig(types.Int, types.P(types.String))}
	xsP, xsSym := b.vp("xs")
	xs := b.letd(xsP, b.tapp(b.tn("List"), b.tn("String")), nil)
	up := b.unitParam()
	call := b.goCall(strconv, atoi, "Atoi", b.spread(b.ref(xsSym)))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"), call)

	_, diags := b.check(src, xs, main)
	requireCodes(t, diags, "E0505")
}

// `f(_, x)` — the cute placeholder turns the call into a
// one-argument function and records HoleIndex.
func TestCallGoHole(t *testing.T) {
	b := newTB()
	strings := b.modSym("strings", true)
	repeat := b.gofnSym("strings", "Repeat")
	src := MapSource{
		repeat: sig(types.String, types.P(types.String), types.P(types.Int)),
	}
	up := b.unitParam()
	call := b.goCall(strings, repeat, "Repeat", b.hole(), b.ilit("3"))
	main, _ := b.fn("main", []syntax.Param{up},
		b.tfn(b.tn("String"), b.tp(b.tn("String"))), call)

	f, diags := b.check(src, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	expectCall(t, a, CallGo, 2, 2, 0, nil)
	if a.Call.HoleIndex != 0 {
		t.Fatalf("HoleIndex = %d, want 0", a.Call.HoleIndex)
	}
	if _, ok := a.Args[0].Value.(*Hole); !ok {
		t.Fatalf("arg 0 = %T, want *Hole", a.Args[0].Value)
	}
	// The call is a one-argument function at the hole's type.
	if got := a.Type().String(); got != "String -> String" {
		t.Errorf("hole call type = %s, want `String -> String`", got)
	}
}

// Two holes are E0504.
func TestCallGoTwoHoles(t *testing.T) {
	b := newTB()
	strings := b.modSym("strings", true)
	repeat := b.gofnSym("strings", "Repeat")
	src := MapSource{
		repeat: sig(types.String, types.P(types.String), types.P(types.Int)),
	}
	up := b.unitParam()
	call := b.goCall(strings, repeat, "Repeat", b.hole(), b.hole())
	main, _ := b.fn("main", []syntax.Param{up},
		b.tfn(b.tn("String"), b.tp(b.tn("String"))), call)

	_, diags := b.check(src, main)
	requireCodes(t, diags, "E0504")
}

// A `_` argument of a Gom call is E0504.
func TestCallGomHole(t *testing.T) {
	b := newTB()
	xp, _ := b.param("x", "", b.tn("Int"))
	f1, f1Sym := b.fn("f", []syntax.Param{xp}, b.tn("Int"), b.ilit("0"))
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(f1Sym), b.arg(b.hole())))

	_, diags := b.check(nil, f1, main)
	requireCodes(t, diags, "E0504")
}

// A Go method call is CallGo with the method as callee.
func TestCallGoMethod(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer") // used as type sym below
	bufT := &types.Go{Ref: types.NamedRef{Path: "bytes", Name: "Buffer"}}
	writeString := b.methodSym("bytes", "WriteString")
	src := MapSource{
		bufSym:      {Type: bufT},
		writeString: sig(types.Tup(types.Int, &types.Con{Name: "GoError"}), types.P(types.String)),
	}
	// let buf : bytes.Buffer
	bufP, bs := b.vp("buf")
	// qualify the type name to the package member symbol
	lb := b.letd(bufP, b.tnBound("Buffer", bufSym), nil)

	up := b.unitParam()
	recv := b.ref(bs)
	call := b.app(b.sel(recv, writeString, "WriteString"), b.arg(b.slit("s")))
	// mid-block: the tuple the method returns is dropped — E0402.
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(call, b.ulit()))

	f, diags := b.check(src, lb, main)
	requireCodes(t, diags, "E0402")
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body.(*Block).Stmts[0])
	expectCall(t, a, CallGo, 1, 1, 0, nil)
	if a.Call.Callee != writeString {
		t.Errorf("callee = %v, want WriteString", a.Call.Callee)
	}
}

// A method on a Gom-typed receiver is E0502.
func TestMethodOnGomValue(t *testing.T) {
	b := newTB()
	mSym := b.methodSym("bytes", "M")
	src := MapSource{mSym: sig(types.Unit)}
	// The receiver's type is a Gom record: methods cannot apply.
	recD, rec := b.typeDecl("P", nil, b.recBody(b.fieldNamed("x", b.tn("Int"))))
	p, ps := b.vp("p")
	lp := b.letd(p, b.tnBound("P", rec), nil)
	up := b.unitParam()
	call := b.app(b.sel(b.ref(ps), mSym, "M"), b.arg(b.ulit()))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"), b.block(call, b.ulit()))

	_, diags := b.check(src, recD, lp, main)
	requireCodes(t, diags, "E0502")
}

// `os.Args ()` — a Go package variable is a plain value, not a Go
// callee: the application classifies CallUnknown and reports the
// ordinary non-function mismatch (E0401) instead of becoming a
// CallGo that lowering would reject as an E0901 contract gap.
func TestCallGoVarNotCallable(t *testing.T) {
	b := newTB()
	os := b.modSym("os", true)
	args := b.govarSym("os", "Args")
	src := MapSource{args: {Type: types.GoSlice(types.String)}}
	up := b.unitParam()
	call := b.app(b.member(os, args, "Args"), b.arg(b.ulit()))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"), call)

	f, diags := b.check(src, main)
	requireCodes(t, diags, "E0401")
	d := findDecl[*FnDecl](f, 0)
	a := applyAt(d.Body)
	expectCall(t, a, CallUnknown, 0, 1, 0, nil)
}

// `os.Args _` takes the same path — the `_` placeholder is only
// legal in a Go call, so E0504 fires on top of the non-function
// application mismatch (E0401). The recovered unit argument leaves
// its parameter meta unsolved, so E0407 tags along.
func TestCallGoVarHoleArg(t *testing.T) {
	b := newTB()
	os := b.modSym("os", true)
	args := b.govarSym("os", "Args")
	src := MapSource{args: {Type: types.GoSlice(types.String)}}
	up := b.unitParam()
	call := b.app(b.member(os, args, "Args"), b.arg(b.hole()))
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"), call)

	_, diags := b.check(src, main)
	requireCodes(t, diags, "E0504", "E0401", "E0407")
}

// `Go.conv e : T` is an Ascribe with Conv; a bare `Go.conv e` is
// E0507, as is an impossible conversion.
func TestConv(t *testing.T) {
	b := newTB()
	goMod := b.modSym("Go", true)
	conv := b.builtinSym("conv", "Go")
	vp, vs := b.vp("i")
	lv := b.letd(vp, b.tn("Int"), b.ilit("3"))
	up := b.unitParam()
	convSel := b.member(goMod, conv, "conv")
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int32"),
		b.ascribe(b.app(convSel, b.arg(b.ref(vs))), b.tn("Int32")))

	f, diags := b.check(nil, lv, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	if a, ok := d.Body.(*Ascribe); !ok || !a.Conv {
		t.Errorf("body = %T, want *Ascribe with Conv", d.Body)
	}
	if d.Body.Type().String() != "Int32" {
		t.Errorf("conv type = %s, want Int32", d.Body.Type().String())
	}
}

// A bare `Go.conv e` without `: T` is E0507.
func TestConvBare(t *testing.T) {
	b := newTB()
	goMod := b.modSym("Go", true)
	conv := b.builtinSym("conv", "Go")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int32"),
		b.app(b.member(goMod, conv, "conv"), b.arg(b.ilit("1"))))

	_, diags := b.check(nil, main)
	requireCodes(t, diags, "E0507")
}

// `Go.conv` in value position is not a function: the conversion
// operator only exists inside `Go.conv e : T`, so a bare reference
// reports E0507 rather than reaching lowering (which would E0901).
func TestConvValue(t *testing.T) {
	b := newTB()
	goMod := b.modSym("Go", true)
	conv := b.builtinSym("conv", "Go")
	vp, _ := b.vp("c")
	d := b.letd(vp,
		b.tfn(b.tn("Int32"), b.tp(b.tn("Int"))),
		b.member(goMod, conv, "conv"))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0507")
}

// A type member of a module (`Go.Slice`, `Go.Map`, `pkg.SomeRecord`)
// is a namespace, not a value: expression position is E0201. The
// resolver reports it first for parsed input; check reports it too
// so hand-built inputs stay covered.
func TestGoTypeMemberValue(t *testing.T) {
	b := newTB()
	goMod := b.modSym("Go", true)
	slice := &resolve.Symbol{Name: "Slice", Kind: resolve.KindType, Package: "Go"}
	vp, _ := b.vp("s")
	d := b.letd(vp, nil, b.member(goMod, slice, "Slice"))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0201")
	if got := diags[0].Message; got != "`Slice` is a type, not a value" {
		t.Errorf("message = %q", got)
	}
}

// A Go *function* in type position reports its kind, not its
// signature's arity — a `Ptr` bound to a three-parameter Go func
// used to claim "`Ptr` takes 3 type argument(s)" (the P3-5 extern
// diagnostic; resolve keeps non-type members out of the extern
// type table, and this gate covers qualified member names and
// hand-built tables).
func TestGoFuncMemberType(t *testing.T) {
	b := newTB()
	ptrSym := b.gofnSym("pkg", "Ptr")
	src := MapSource{ptrSym: sig(types.Int,
		types.P(types.String), types.P(types.String), types.P(types.String))}
	vp, _ := b.vp("s")
	d := b.letd(vp, b.tapp(b.tnBound("Ptr", ptrSym), b.tn("Int")), nil)

	_, diags := b.check(src, d)
	requireCodes(t, diags, "E0201")
	if got := diags[0].Message; got != "`Ptr` is a func, not a type" {
		t.Errorf("message = %q", got)
	}
}

// An impossible conversion is E0507.
func TestConvImpossible(t *testing.T) {
	b := newTB()
	goMod := b.modSym("Go", true)
	conv := b.builtinSym("conv", "Go")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.ascribe(b.app(b.member(goMod, conv, "conv"), b.arg(b.slit("abc"))), b.tn("Int")))

	_, diags := b.check(nil, main)
	requireCodes(t, diags, "E0507")
}

// `e : T` without Go.conv is rejected — there is no general
// ascription in the surface language yet (docs/problems).
func TestAscribeNonConv(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.ascribe(b.ilit("1"), b.tn("Int")))

	_, diags := b.check(nil, main)
	requireCodes(t, diags, "E0401")
}
