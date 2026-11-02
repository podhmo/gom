package check

// Hindley–Milner inference tests (P2-1): generalization of top-level
// let bindings under the value restriction, per-use instantiation,
// and the diagnostics that keep inference failures out of lowering.
// The emission shape is recorded in
// docs/decisions/2026-11-02-let-polymorphism-emission.md.

import (
	"testing"

	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// `let id = fn x -> x` generalizes; `id` applies at Int and String in
// the same file — each use instantiates fresh type arguments.
func TestPolyIdentityUsedTwice(t *testing.T) {
	b := newTB()
	xp, xs := b.vp("x")
	idp, idSym := b.vp("id")
	letD := b.letd(idp, nil, b.lambda([]syntax.Param{{Sp: xp.Sp, Pat: xp}}, b.ref(xs)))

	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("String"),
		b.block(
			b.letStmt(b.wp(), b.app(b.ref(idSym), b.arg(b.ilit("1")))),
			b.app(b.ref(idSym), b.arg(b.slit("s"))),
		))

	f, diags := b.check(nil, letD, main)
	requireCodes(t, diags)

	// The let flipped to a function declaration with one type param.
	fd := findDecl[*FnDecl](f, 0)
	if fd == nil {
		t.Fatal("let id = fn x -> x did not become a FnDecl")
	}
	if got := fd.Sig.String(); got != "a -> a" {
		t.Errorf("id sig = %s, want `a -> a`", got)
	}
	if len(fd.TypeParams) != 1 {
		t.Fatalf("TypeParams = %d, want 1", len(fd.TypeParams))
	}

	md := findDecl[*FnDecl](f, 1)
	var targs [][]types.Type
	for _, a := range applys(md) {
		if a.Call.Callee == idSym {
			targs = append(targs, a.Call.TypeArgs)
		}
	}
	if len(targs) != 2 {
		t.Fatalf("found %d id calls, want 2", len(targs))
	}
	if got := targs[0][0].String(); got != "Int" {
		t.Errorf("id 1 targs = %s, want Int", got)
	}
	if got := targs[1][0].String(); got != "String" {
		t.Errorf("id \"s\" targs = %s, want String", got)
	}
}

// `let w = fn x -> x x` — self-application forces `?x := ?x -> ?r`,
// an occurs failure reported as one E0401, not a cascade.
func TestSelfApplyOccurs(t *testing.T) {
	b := newTB()
	xp, xs := b.vp("x")
	wp, _ := b.vp("w")
	d := b.letd(wp, nil,
		b.lambda([]syntax.Param{{Sp: xp.Sp, Pat: xp}},
			b.app(b.ref(xs), b.arg(b.ref(xs)))))

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0401")
}

// A top-level `let` whose RHS is an application does not generalize:
// `let y = f 1` is monomorphic, so using y at String is E0401.
func TestValueRestrictionApply(t *testing.T) {
	b := newTB()
	xp, xs := b.param("x", "", b.tn("Int"))
	fDecl, fSym := b.fn("f", []syntax.Param{xp}, b.tn("Int"), b.ref(xs))
	yp, ySym := b.vp("y")
	letD := b.letd(yp, nil, b.app(b.ref(fSym), b.arg(b.ilit("1"))))

	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("String"), b.ref(ySym))

	_, diags := b.check(nil, fDecl, letD, main)
	requireCodes(t, diags, "E0401")
}

// A generalized empty-list value emits a thunk: a niladic FnDecl
// whose references instantiate the element type.
func TestLetValueThunk(t *testing.T) {
	b := newTB()
	ep, _ := b.vp("e")
	d := b.letd(ep, nil, b.list())

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	fd := findDecl[*FnDecl](f, 0)
	if fd == nil {
		t.Fatal("let e = [] did not become a thunk FnDecl")
	}
	if len(fd.Params) != 0 {
		t.Errorf("thunk params = %d, want 0", len(fd.Params))
	}
	if got := fd.Sig.String(); got != " -> List a" {
		t.Errorf("thunk sig = %s, want ` -> List a`", got)
	}
	if len(fd.TypeParams) != 1 {
		t.Fatalf("TypeParams = %d, want 1", len(fd.TypeParams))
	}
}

// A reference to a generalized value types at the *result*: `e` in
// `match e` is `List ?`, not `() -> List ?`.
func TestThunkRefTypesAsValue(t *testing.T) {
	b := newTB()
	ep, eSym := b.vp("e")
	letD := b.letd(ep, nil, b.list())
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.matchx(b.ref(eSym),
			b.arm(b.listPat(nil), nil, b.ilit("0")),
			b.arm(b.wp(), nil, b.ilit("1"))))

	f, diags := b.check(nil, letD, main)
	requireCodes(t, diags, "E0407")
	// The match's subject must be a List — the thunk's result —
	// or the arm check never sees the element meta.
	md := findDecl[*FnDecl](f, 1)
	m, ok := md.Body.(*Match)
	if !ok {
		t.Fatalf("main body = %T, want *Match", md.Body)
	}
	v, ok := m.Subj.(*Var)
	if !ok {
		t.Fatalf("match subject = %T, want *Var", m.Subj)
	}
	if _, ok := types.Chase(v.Type()).(*types.Func); ok {
		t.Errorf("match subject type = %s, want a List, not the thunk function", v.Type())
	}
}

// A bodiless `let f : () -> List Int` signature is a *declared*
// niladic function, not a thunk: a reference types at the function
// type itself (the `() -> T` shape alone must not unwrap).
func TestNiladicSigIsNotThunk(t *testing.T) {
	b := newTB()
	fp, fSym := b.vp("f")
	gp, _ := b.vp("g")
	listInt := func() *syntax.TypeApp { return b.tapp(b.tn("List"), b.tn("Int")) }
	letF := b.letd(fp, b.tfn(listInt()), nil) // no body — .gomi-style `() -> List Int`
	letG := b.letd(gp, nil, b.ref(fSym))

	f, diags := b.check(nil, letF, letG)
	requireCodes(t, diags)
	ld := findDecl[*LetDecl](f, 1)
	if ld == nil {
		t.Fatal("let g = f did not stay a LetDecl")
	}
	v, ok := ld.Value.(*Var)
	if !ok {
		t.Fatalf("let g value = %T, want *Var", ld.Value)
	}
	sig, ok := types.Chase(v.Type()).(*types.Func)
	if !ok || len(sig.Params) != 0 {
		t.Fatalf("f reference type = %s, want `() -> List Int`", v.Type())
	}
}

// A local let stays monomorphic: `let id = fn x -> x` inside a body
// used at Int then String is E0401.
func TestLocalLetMonomorphic(t *testing.T) {
	b := newTB()
	xp, xs := b.vp("x")
	idp, idSym := b.vp("id")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("String"),
		b.block(
			b.letStmt(idp, b.lambda([]syntax.Param{{Sp: xp.Sp, Pat: xp}}, b.ref(xs))),
			b.letStmt(b.wp(), b.app(b.ref(idSym), b.arg(b.ilit("1")))),
			b.app(b.ref(idSym), b.arg(b.slit("s"))),
		))

	_, diags := b.check(nil, main)
	found := false
	for _, d := range diags {
		if d.Code != "E0401" {
			t.Fatalf("diags = %v, want only E0401", diagCodes(diags))
		}
		found = true
	}
	if !found {
		t.Fatal("expected E0401 using a local let at two types")
	}
}

// A `let` annotation's type variables bind through generalization:
// `let x : List a = []` quantifies a for the whole binding.
func TestLetAnnotationVarGeneralizes(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	d := b.letd(xp, b.tapp(b.tn("List"), b.tvar("a")), b.list())

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	fd := findDecl[*FnDecl](f, 0)
	if fd == nil {
		t.Fatal("let x : List a = [] did not generalize")
	}
	if len(fd.TypeParams) != 1 || fd.TypeParams[0].Name != "a" {
		t.Fatalf("TypeParams = %v, want [a]", fd.TypeParams)
	}
}

// `let x : a = ()` — the annotation's a never binds: the value is
// Unit, so a is unbound (E0412) and the mismatch is E0401.
func TestLetAnnotationVarUnbound(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	d := b.letd(xp, b.tvar("a"), b.ulit())

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0401", "E0412")
}

// A `let` inside a function body can still *refer to* the enclosing
// signature's variables: `fn f (xs: List a) = { let y : List a =
// xs; y }` resolves the annotation's `a` to f's own variable — no
// E0412, no "expected a, got a" mismatch.
func TestLocalLetSeesEnclosingVar(t *testing.T) {
	b := newTB()
	xsp, xsSym := b.param("xs", "", b.tapp(b.tn("List"), b.tvar("a")))
	yp, ySym := b.vp("y")
	d, _ := b.fn("f", []syntax.Param{xsp}, b.tapp(b.tn("List"), b.tvar("a")),
		b.block(
			b.letx(yp, b.tapp(b.tn("List"), b.tvar("a")), b.ref(xsSym)),
			b.ref(ySym),
		))

	_, diags := b.check(nil, d)
	requireCodes(t, diags)
}

// A `let` annotation's own variables stay visible inside its
// right-hand side: `let f : List a -> List a = fn xs -> { let y :
// List a = xs; y }` binds the lambda body's `a` to the annotation's.
func TestLetAnnotationVarVisibleInRHS(t *testing.T) {
	b := newTB()
	fp, fSym := b.vp("f")
	yp, ySym := b.vp("y")
	xsp, xsSym := b.vp("xs")
	listA := func() *syntax.TypeApp { return b.tapp(b.tn("List"), b.tvar("a")) }
	d := b.letd(fp, b.tfn(listA(), b.tp(listA())),
		b.lambda([]syntax.Param{{Sp: xsp.Sp, Pat: xsp}},
			b.block(
				b.letx(yp, listA(), b.ref(xsSym)),
				b.ref(ySym))))

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	fd := findDecl[*FnDecl](f, 0)
	if fd == nil || fd.Sym != fSym {
		t.Fatal("annotated let-lambda did not become a FnDecl")
	}
	if len(fd.TypeParams) != 1 || fd.TypeParams[0].Name != "a" {
		t.Fatalf("TypeParams = %v, want [a]", fd.TypeParams)
	}
}

// A `let` inside a function body cannot introduce type variables —
// the example from the E0412 code entry.
func TestLocalLetAnnotationVarUnbound(t *testing.T) {
	b := newTB()
	xp, _ := b.vp("x")
	up := b.unitParam()
	main, _ := b.fn("f", []syntax.Param{up}, b.tn("Unit"),
		b.block(
			b.letx(xp, b.tvar("a"), b.ulit()),
			b.ulit(),
		))

	_, diags := b.check(nil, main)
	found := false
	for _, d := range diags {
		if d.Code == "E0412" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diags = %v, want E0412", diagCodes(diags))
	}
}

// A declared signature's type variables are the fn's own type
// parameters — `fn len2 (xs: List a) -> Int` carries [a].
func TestFnDeclTypeParams(t *testing.T) {
	b := newTB()
	xsp, _ := b.param("xs", "", b.tapp(b.tn("List"), b.tvar("a")))
	d, _ := b.fn("len2", []syntax.Param{xsp}, b.tn("Int"), b.ilit("0"))

	f, diags := b.check(nil, d)
	requireCodes(t, diags)
	fd := findDecl[*FnDecl](f, 0)
	if len(fd.TypeParams) != 1 || fd.TypeParams[0].Name != "a" {
		t.Fatalf("TypeParams = %v, want [a]", fd.TypeParams)
	}
}

// `fn a () -> a = {}` — the signature's a binds nothing the body can
// produce: the mismatch is a check-time E0401, never a lower E0901.
func TestFnRetVarBodyMismatch(t *testing.T) {
	b := newTB()
	up := b.unitParam()
	d, _ := b.fn("a", []syntax.Param{up}, b.tvar("a"), b.block())

	_, diags := b.check(nil, d)
	requireCodes(t, diags, "E0401")
}

// A let-bound lambda referenced before its declaration keeps the
// pre-registered monomorphic meta — nothing generalizes — but the
// binding still becomes a function declaration: `var f = <func
// literal>` makes every recursive reference a Go initialization
// cycle, while a func decl escapes dependency analysis. The use
// recorded while the symbol was a variable is re-classified to a
// direct call.
func TestLetForwardRefStaysMonomorphic(t *testing.T) {
	b := newTB()
	xp, xs := b.vp("x")
	tp, tSym := b.vp("twice")
	up := b.unitParam()
	main, _ := b.fn("main", []syntax.Param{up}, b.tn("Int"),
		b.app(b.ref(tSym), b.arg(b.ilit("1"))))
	letD := b.letd(tp, nil,
		b.lambda([]syntax.Param{{Sp: xp.Sp, Pat: xp}},
			b.bin(syntax.BinAdd, b.ref(xs), b.ref(xs))))

	f, diags := b.check(nil, main, letD)
	requireCodes(t, diags)
	// `twice` flips to a monomorphic FnDecl — no type parameters,
	// the forward use's `Int -> Int` shape.
	fd := findDecl[*FnDecl](f, 1)
	if fd == nil || fd.Sym != tSym {
		t.Fatal("expected twice to become a FnDecl")
	}
	if len(fd.TypeParams) != 0 {
		t.Errorf("twice TypeParams = %v, want none (monomorphic)", fd.TypeParams)
	}
	if got := fd.Sig.String(); got != "Int -> Int" {
		t.Errorf("twice sig = %s, want `Int -> Int`", got)
	}
	// The forward call, recorded CallUnknown while twice was a
	// variable, is now a direct call.
	md := findDecl[*FnDecl](f, 0)
	var call *Apply
	for _, a := range applys(md) {
		if a.Call.Callee == tSym {
			call = a
		}
	}
	if call == nil {
		t.Fatal("no call to twice recorded in main")
	}
	if call.Call.Kind != CallDirect || call.Call.Arity != 1 || call.Call.Applied != 1 {
		t.Errorf("twice call = %+v, want saturated CallDirect", call.Call)
	}
}

// `let f = fn n -> ... f ...` — a self-recursive let-lambda is
// *referenced* (by its own body) yet monomorphic: it flips to a
// FnDecl, and the recursive call becomes CallDirect.
func TestLetSelfRecursionFlips(t *testing.T) {
	b := newTB()
	np, ns := b.vp("n")
	fp, fSym := b.vp("f")
	letD := b.letd(fp, nil,
		b.lambda([]syntax.Param{{Sp: np.Sp, Pat: np}},
			b.ifx(b.bin(syntax.BinEq, b.ref(ns), b.ilit("0")),
				b.ilit("0"),
				b.app(b.ref(fSym), b.arg(b.bin(syntax.BinSub, b.ref(ns), b.ilit("1")))))))

	f, diags := b.check(nil, letD)
	requireCodes(t, diags)
	fd := findDecl[*FnDecl](f, 0)
	if fd == nil || fd.Sym != fSym {
		t.Fatal("self-recursive let f did not become a FnDecl")
	}
	if len(fd.TypeParams) != 0 {
		t.Errorf("f TypeParams = %v, want none", fd.TypeParams)
	}
	var selfCall *Apply
	for _, a := range applys(fd) {
		if a.Call.Callee == fSym {
			selfCall = a
		}
	}
	if selfCall == nil {
		t.Fatal("no call to f inside its own body")
	}
	if selfCall.Call.Kind != CallDirect || selfCall.Call.Arity != 1 || selfCall.Call.Applied != 1 {
		t.Errorf("self call = %+v, want saturated CallDirect", selfCall.Call)
	}
}

// Mutual recursion through let-lambdas: `even` flips as an
// unreferenced binding; `odd` was referenced by `even`'s body but
// stays monomorphic and flips too — even's `odd` call, recorded
// while odd was a variable, is re-classified CallDirect.
func TestLetMutualRecursionFlips(t *testing.T) {
	b := newTB()
	np, ns := b.vp("n")
	mp, ms := b.vp("m")
	ep, evenSym := b.vp("even")
	op, oddSym := b.vp("odd")
	letEven := b.letd(ep, nil,
		b.lambda([]syntax.Param{{Sp: np.Sp, Pat: np}},
			b.ifx(b.bin(syntax.BinEq, b.ref(ns), b.ilit("0")),
				b.blit(true),
				b.app(b.ref(oddSym), b.arg(b.bin(syntax.BinSub, b.ref(ns), b.ilit("1")))))))
	letOdd := b.letd(op, nil,
		b.lambda([]syntax.Param{{Sp: mp.Sp, Pat: mp}},
			b.ifx(b.bin(syntax.BinEq, b.ref(ms), b.ilit("0")),
				b.blit(false),
				b.app(b.ref(evenSym), b.arg(b.bin(syntax.BinSub, b.ref(ms), b.ilit("1")))))))

	f, diags := b.check(nil, letEven, letOdd)
	requireCodes(t, diags)
	evenD := findDecl[*FnDecl](f, 0)
	oddD := findDecl[*FnDecl](f, 1)
	if evenD == nil || evenD.Sym != evenSym || oddD == nil || oddD.Sym != oddSym {
		t.Fatalf("mutually-recursive lets did not both become FnDecls: %v", f.Decls)
	}
	var oddCall *Apply
	for _, a := range applys(evenD) {
		if a.Call.Callee == oddSym {
			oddCall = a
		}
	}
	if oddCall == nil {
		t.Fatal("no call to odd recorded in even's body")
	}
	if oddCall.Call.Kind != CallDirect || oddCall.Call.Arity != 1 || oddCall.Call.Applied != 1 {
		t.Errorf("odd call = %+v, want saturated CallDirect", oddCall.Call)
	}
}

// An annotated self-recursive let — `let f : Int -> Int = fn n ->
// ... f ...` — flips the same way: the annotation carries no type
// variables, so nothing blocks the monomorphic func decl.
func TestLetAnnotatedRecursionFlips(t *testing.T) {
	b := newTB()
	np, ns := b.vp("n")
	fp, fSym := b.vp("f")
	letD := b.letd(fp, b.tfn(b.tn("Int"), b.tp(b.tn("Int"))),
		b.lambda([]syntax.Param{{Sp: np.Sp, Pat: np}},
			b.ifx(b.bin(syntax.BinEq, b.ref(ns), b.ilit("0")),
				b.ilit("0"),
				b.app(b.ref(fSym), b.arg(b.bin(syntax.BinSub, b.ref(ns), b.ilit("1")))))))

	f, diags := b.check(nil, letD)
	requireCodes(t, diags)
	fd := findDecl[*FnDecl](f, 0)
	if fd == nil || fd.Sym != fSym {
		t.Fatal("annotated recursive let f did not become a FnDecl")
	}
	if len(fd.TypeParams) != 0 {
		t.Errorf("f TypeParams = %v, want none", fd.TypeParams)
	}
	if got := fd.Sig.String(); got != "Int -> Int" {
		t.Errorf("f sig = %s, want `Int -> Int`", got)
	}
}

// Mutual recursion through declared signatures: isEven/isOdd both
// typecheck — declared fns instantiate at every use, including each
// other's bodies.
func TestMutualRecursion(t *testing.T) {
	b := newTB()
	np, ns := b.param("n", "", b.tn("Int"))
	mp, ms := b.param("m", "", b.tn("Int"))
	isEven, isEvenSym := b.fn("isEven", []syntax.Param{np}, b.tn("Bool"), nil)
	isOdd, isOddSym := b.fn("isOdd", []syntax.Param{mp}, b.tn("Bool"), nil)
	// Fill in the bodies once the symbols exist.
	isEven.Body = b.ifx(b.bin(syntax.BinEq, b.ref(ns), b.ilit("0")),
		b.blit(true),
		b.app(b.ref(isOddSym), b.arg(b.bin(syntax.BinSub, b.ref(ns), b.ilit("1")))))
	isOdd.Body = b.ifx(b.bin(syntax.BinEq, b.ref(ms), b.ilit("0")),
		b.blit(false),
		b.app(b.ref(isEvenSym), b.arg(b.bin(syntax.BinSub, b.ref(ms), b.ilit("1")))))

	_, diags := b.check(nil, isEven, isOdd)
	requireCodes(t, diags)
}
