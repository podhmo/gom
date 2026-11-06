package check

// Deferred member applications (pendingSels + pendingApplies): a
// select applied while its receiver's type is still a meta defers
// the calling-convention decision until revisitSelects settles the
// member — a Go method replays under CallGo, anything else under
// the Gom rule. These tests build the deferral by hand: an
// unannotated lambda parameter carries the meta, and a later `let
// buf` application is what binds it.

import (
	"testing"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// methodStub is a SignatureSource + MethodSource for tests: fixed
// signatures plus a method table consulted for a single receiver
// type (the way internal/goimport answers for named Go types).
// ifaces lists the Go interface types recv is declared assignable
// to — the InterfaceSource half of the stub.
type methodStub struct {
	MapSource
	recv    types.Type
	methods map[string]*resolve.Symbol
	ifaces  []types.Type
}

// MethodOf answers for methods registered on recv.
func (m methodStub) MethodOf(recv types.Type, name string) (*resolve.Symbol, Signature, bool) {
	if !types.Equal(types.Chase(recv), m.recv) {
		return nil, Signature{}, false
	}
	sym, ok := m.methods[name]
	if !ok {
		return nil, Signature{}, false
	}
	return sym, m.MapSource[sym], true
}

// InterfaceAssignable answers reflexively plus recv → each ifaces
// entry — a stand-in for go/types' Implements lookup.
func (m methodStub) InterfaceAssignable(from, to types.Type) bool {
	if types.Equal(from, to) {
		return true
	}
	if !types.Equal(types.Chase(from), m.recv) {
		return false
	}
	for _, i := range m.ifaces {
		if types.Equal(to, i) {
			return true
		}
	}
	return false
}

// bufType is the receiver the stub's method table answers for —
// `bytes.Buffer` in spirit.
func bufType() *types.Go {
	return &types.Go{Ref: types.NamedRef{Path: "bytes", Name: "Buffer"}}
}

// `let later = fn x -> x.M(1); later buf` — the member call is
// checked while x's type is still a meta; once `later buf` binds
// `x = bytes.Buffer`, revisitSelects resolves the method and the
// apply replays as CallGo.
func TestDeferredMethodCall(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer") // type-name symbol
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: sig(types.Unit, types.P(types.Int))},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
	}

	// let buf : bytes.Buffer = <hole of the right type>
	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	// fn x -> x.M(1) applied to buf, inside main's block.
	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "M"), b.arg(b.ilit("1"))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	f, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	// firstApply walks the let's lambda value before the outer
	// `later buf` call, so this is the `x.M(1)` application.
	a := firstApply(d)
	if a == nil {
		t.Fatal("no apply in main")
	}
	expectCall(t, a, CallGo, 1, 1, 1, 0, nil)
	if a.Call.Callee != m {
		t.Errorf("callee = %v, want method M", a.Call.Callee)
	}
}

// `let m = x.Error; m ()` — the apply's callee is a Var standing
// on the deferred member's standing meta, so once `x.Error`
// settles to a Go method no CallGo can name its receiver: the
// pendingApply's alwaysGom pin forces the Gom replay, where
// `m ()` over the settled `-> Unit` is the zero-argument call
// marker — CallUnknown with no arguments — not a tuple-convention
// method call.
func TestDeferredAlwaysGomUnitMarker(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "Error")
	src := methodStub{
		MapSource: MapSource{m: sig(types.Unit)}, // func()
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"Error": m},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	mp, mSym := b.vp("m")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		// { let m = x.Error; m () }
		b.block(
			b.letStmt(mp, b.sel(b.ref(xs), nil, "Error")),
			b.app(b.ref(mSym), b.arg(b.ulit()))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	f, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	requireCodes(t, diags)
	a := firstApply(findDecl[*FnDecl](f, 0))
	if a == nil {
		t.Fatal("no apply in main")
	}
	// The `-> Unit` value applied as `m ()` saturates into the
	// zero-argument call — no Go convention may claim the Var.
	expectCall(t, a, CallUnknown, 0, 0, 0, 0, nil)
	if a.Call.Callee != nil {
		t.Errorf("callee = %v, want nil — `m` is a value, not the method", a.Call.Callee)
	}
	if len(a.Args) != 0 {
		t.Errorf("args = %d, want 0 — the unit literal is the call marker", len(a.Args))
	}
	if got := a.Type().String(); got != "Unit" {
		t.Errorf("apply type = %s, want Unit", got)
	}
}

// `fn x -> x.M(_, 3)` — the `_` survives the provisional pass and
// the replayed CallGo reports HoleIndex 0: the partial function's
// parameter type is the hole's declared slot.
func TestDeferredMethodHole(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: sig(types.Unit, types.P(types.String), types.P(types.Int))},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "M"), b.arg(b.hole()), b.arg(b.ilit("3"))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			// The cute partial — `later buf : String -> Unit` —
			// applies once so the block's value is the call.
			b.app(b.ref(lSym), b.arg(b.ref(bs)), b.arg(b.slit("s")))))

	f, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	inner := firstApply(d)
	if inner == nil {
		t.Fatal("no inner apply")
	}
	expectCall(t, inner, CallGo, 2, 2, 2, 0, nil)
	if inner.Call.HoleIndex != 0 {
		t.Fatalf("HoleIndex = %d, want 0", inner.Call.HoleIndex)
	}
	if _, ok := inner.Args[0].Value.(*Hole); !ok {
		t.Fatalf("arg 0 = %T, want *Hole", inner.Args[0].Value)
	}
	// `x.M(_, 3)` : String -> Unit — one parameter at the hole's slot.
	if got := inner.Type().String(); got != "String -> Unit" {
		t.Errorf("hole call type = %s, want `String -> Unit`", got)
	}
}

// The surface argument tuple survives the provisional pass so the
// replay unpacks it the way the eager Go path does.
func TestDeferredMethodTupleArgs(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: sig(types.Unit, types.P(types.String), types.P(types.Int))},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		// x.M(("s", 3)) — a single tuple argument.
		b.app(b.sel(b.ref(xs), nil, "M"),
			b.arg(b.tuple(b.slit("s"), b.ilit("3")))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	f, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	requireCodes(t, diags)
	d := findDecl[*FnDecl](f, 0)
	inner := firstApply(d)
	if inner == nil {
		t.Fatal("no inner apply")
	}
	expectCall(t, inner, CallGo, 2, 2, 2, 0, nil)
	if len(inner.Args) != 2 {
		t.Fatalf("go args = %d, want the tuple unpacked to 2", len(inner.Args))
	}
}

// `fn r -> r.n(_)` where `r` binds to a record: the member resolves
// as a field, so the apply replays under the Gom convention and the
// hole reports E0504 — the diagnostic the provisional pass withheld.
func TestDeferredHoleOnGomField(t *testing.T) {
	b := newTB()
	recD, rec := b.typeDecl("R", nil, b.recBody(b.fieldNamed("n", b.tn("Int"))))

	rp, rs := b.vp("r")
	lr := b.letd(rp, b.tnBound("R", rec), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "n"), b.arg(b.hole())))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(rs)))))

	f, diags := b.check(nil, recD, lr, main)
	// E0504 for the hole (emitted as the argument replays); the
	// curried over-application of `Int` contributes the ordinary
	// mismatch diagnostics — same emission order the eager path
	// produces (`os.Args _`).
	requireCodes(t, diags, "E0504", "E0401", "E0407")
	inner := firstApply(findDecl[*FnDecl](f, 0))
	if inner == nil {
		t.Fatal("no inner apply")
	}
	// The recovered unit literal keeps the tree valid for lowering.
	if _, ok := inner.Args[0].Value.(*Hole); ok {
		t.Error("a Hole argument must not survive the Gom replay")
	}
}

// A receiver that never binds replays the provisional apply under
// the Gom convention: the hole still reports E0504, and the
// parameter's unresolved meta reports E0407 — no phantom method
// classification is invented.
func TestDeferredUnresolvedReceiver(t *testing.T) {
	b := newTB()
	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "M"), b.arg(b.hole())))
	lp, _ := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(b.letStmt(lp, lambda), b.ulit()))

	_, diags := b.check(nil, main)
	codes := diagCodes(diags)
	var sawHole, sawAnno bool
	for _, c := range codes {
		sawHole = sawHole || c == "E0504"
		sawAnno = sawAnno || c == "E0407"
	}
	if !sawHole {
		t.Errorf("codes = %v, want an E0504 for the `_`", codes)
	}
	if !sawAnno {
		t.Errorf("codes = %v, want an E0407 for the unresolved receiver", codes)
	}
}

// A `_` nested inside the argument tuple — not an argument of the
// Go call itself — still reports E0504 after the method resolves.
func TestDeferredNestedHole(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: sig(types.Unit, types.P(types.Int))},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		// x.M((_, 1)) — the tuple is the single argument; the hole
		// sits an element deep, where no Go convention admits it.
		b.app(b.sel(b.ref(xs), nil, "M"),
			b.arg(b.tuple(b.tuple(b.hole(), b.ilit("1"))))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	_, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	codes := diagCodes(diags)
	var sawHole bool
	for _, c := range codes {
		sawHole = sawHole || c == "E0504"
	}
	if !sawHole {
		t.Errorf("codes = %v, want an E0504 for the nested `_`", codes)
	}
}

// A method that does not exist on the bound receiver reports the
// ordinary E0401 — the provisional apply then replays under Gom.
func TestDeferredNoSuchMethod(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	src := methodStub{
		MapSource: MapSource{},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "NoSuch"), b.arg(b.ilit("1"))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	_, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	codes := diagCodes(diags)
	var sawMember bool
	for _, c := range codes {
		sawMember = sawMember || c == "E0401"
	}
	if !sawMember {
		t.Errorf("codes = %v, want an E0401 for `bytes.Buffer has no field or method NoSuch`", codes)
	}
}

// `let s : Iface = x.M()` inside the unannotated lambda: the
// conformance's interface want must wait for the replay — the
// deferred application's result meta defers through
// deferInterfaceSource just like a pendingSels member meta, so the
// provisional consumer routes through pendingIfaces instead of
// binding the meta to the interface. Once `later buf` settles
// `x = bytes.Buffer`, the replay unifies the result with the real
// method type and revisitInterfaces accepts the assignment.
func TestDeferredApplyResultInterface(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	ifaceSym := b.gotypeSym("fmt", "Stringer")
	bufT := bufType()
	ifaceT := &types.Go{Ref: types.NamedRef{Path: "fmt", Name: "Stringer"}}
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: sig(bufT)},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
		ifaces:    []types.Type{ifaceT},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	spat, _ := b.vp("s")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		// { let s : fmt.Stringer = x.M(); () }
		b.block(
			b.letx(spat, b.tnBound("Stringer", ifaceSym),
				b.app(b.sel(b.ref(xs), nil, "M"), b.arg(b.ulit()))),
			b.ulit()))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	_, diags := b.check(Chain(src,
		MapSource{bufSym: {Type: bufT}, ifaceSym: {Type: ifaceT}}), lb, main)
	// Without the deferral the provisional `fmt.Stringer` want would
	// bind the result meta and the replay's unify would report
	// `expected fmt.Stringer, got bytes.Buffer` (E0401).
	requireCodes(t, diags)
}

// A member registered *before* the deferred apply whose result feeds
// its receiver: `r.N` enters pendingSels while checking `g`, and
// `x.M`'s replay — later in the same table — is what binds `r`'s
// type. revisitSelects loops to a fixpoint, so registration order
// decides only diagnostic order, never whether the member resolves.
func TestDeferredMemberFedByLaterApply(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	n := b.methodSym("bytes", "N")
	src := methodStub{
		MapSource: MapSource{
			m: sig(bufT, types.P(types.Int)),
			n: sig(types.Unit),
		},
		recv:    bufT,
		methods: map[string]*resolve.Symbol{"M": m, "N": n},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	// fn g (r) -> Unit = r.N()
	rp, rs := b.param("r", "", nil)
	g, gs := b.fn("g", []syntax.Param{rp}, b.tn("Unit"),
		b.app(b.sel(b.ref(rs), nil, "N"), b.arg(b.ulit())))

	// let later = fn x -> g (x.M(1)); later buf
	xp, xs := b.vp("x")
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, b.lambda(
				[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
				b.app(b.ref(gs), b.arg(
					b.app(b.sel(b.ref(xs), nil, "M"), b.arg(b.ilit("1"))))))),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	f, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, g, main)
	requireCodes(t, diags)
	// g's `r.N()` — the member entered first but resolved second —
	// still replays under CallGo.
	a := firstApply(findDecl[*FnDecl](f, 0))
	if a == nil {
		t.Fatal("no apply in g")
	}
	expectCall(t, a, CallGo, 0, 0, 0, 0, nil)
	if a.Call.Callee != n {
		t.Errorf("callee = %v, want method N", a.Call.Callee)
	}
}

// A second `_` in a deferred call reports E0504 once the convention
// settles — the same diagnostic the eager path emits.
func TestDeferredDoubleHole(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: sig(types.Unit, types.P(types.Int), types.P(types.Int))},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "M"), b.arg(b.hole()), b.arg(b.hole())))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	_, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	codes := diagCodes(diags)
	var sawHole bool
	for _, c := range codes {
		sawHole = sawHole || c == "E0504"
	}
	if !sawHole {
		t.Errorf("codes = %v, want an E0504 for the second `_`", codes)
	}
}

// `..xs` survives the provisional pass and replays as a legal
// variadic spread — the Go convention admits it as the last
// argument.
func TestDeferredSpread(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: vsig(types.Unit, types.P(types.GoSlice(types.String)))},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)
	xsp, xs2 := b.vp("xs")
	lxs := b.letd(xsp, b.tapp(b.tn("List"), b.tn("String")), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "M"), b.arg(b.spread(b.ref(xs2)))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	f, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, lxs, main)
	// The stub method's Unit result plus `..xs` spread into its
	// slice parameter is the copied-List shape W0501 names; the
	// deferred-spread mechanics it verifies are unaffected.
	requireCodes(t, diags, "W0501")
	inner := firstApply(findDecl[*FnDecl](f, 0))
	if inner == nil {
		t.Fatal("no inner apply")
	}
	expectCall(t, inner, CallGo, 1, 1, 1, 0, nil)
	if _, ok := inner.Args[0].Value.(*Spread); !ok {
		t.Errorf("arg 0 = %T, want *Spread", inner.Args[0].Value)
	}
}

// Labeled Go-call arguments map back to declaration order through
// ArgToParam — `x.M(~b: 1, ~a: 2)` reports the same mapping the
// eager path computes.
func TestDeferredLabeledArgs(t *testing.T) {
	b := newTB()
	bufSym := b.gotypeSym("bytes", "Buffer")
	bufT := bufType()
	m := b.methodSym("bytes", "M")
	src := methodStub{
		MapSource: MapSource{m: sig(types.Unit, types.LP("a", types.Int), types.LP("b", types.Int))},
		recv:      bufT,
		methods:   map[string]*resolve.Symbol{"M": m},
	}

	bp, bs := b.vp("buf")
	lb := b.letd(bp, b.tnBound("Buffer", bufSym), nil)

	xp, xs := b.vp("x")
	lambda := b.lambda(
		[]syntax.Param{{Sp: xp.Sp, Pat: xp}},
		b.app(b.sel(b.ref(xs), nil, "M"),
			b.larg("b", b.ilit("1")), b.larg("a", b.ilit("2"))))
	lp, lSym := b.vp("later")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letStmt(lp, lambda),
			b.app(b.ref(lSym), b.arg(b.ref(bs)))))

	f, diags := b.check(Chain(src, MapSource{bufSym: {Type: bufT}}), lb, main)
	requireCodes(t, diags)
	inner := firstApply(findDecl[*FnDecl](f, 0))
	if inner == nil {
		t.Fatal("no inner apply")
	}
	expectCall(t, inner, CallGo, 2, 2, 2, 0, []int{1, 0})
}
