package lower

import (
	"fmt"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// --- builders ---

var sp = diag.SpanIn("t.gom", 1, 2)

func b(t types.Type) check.Base { return check.Base{Sp: sp, Typ: t} }

func lit(k syntax.LitKind, text string, t types.Type) *check.Lit {
	return &check.Lit{Base: b(t), Kind: k, Text: text}
}
func intLit(n string) *check.Lit { return lit(syntax.LitInt, n, types.Int) }
func strLit(s string) *check.Lit { return lit(syntax.LitStr, `"`+s+`"`, types.String) }
func unitLit() *check.Lit        { return lit(syntax.LitUnit, "()", types.Unit) }

func vsym(name string, t types.Type) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindVar}
}
func fsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindFunc}
}
func csym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindCtor}
}
func gsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindFunc, IsGo: true}
}

func vr(sym *resolve.Symbol, t types.Type) *check.Var {
	return &check.Var{Base: b(t), Sym: sym}
}
func vpat(sym *resolve.Symbol, t types.Type) *check.VarPat {
	return &check.VarPat{Base: b(t), Sym: sym}
}
func wpat(t types.Type) *check.WildPat { return &check.WildPat{Base: b(t)} }

func app(fn check.Expr, args []check.Expr, ci check.CallInfo, t types.Type) *check.Apply {
	as := make([]check.Arg, len(args))
	for i, a := range args {
		as[i] = check.Arg{Value: a}
	}
	return &check.Apply{Base: b(t), Fn: fn, Args: as, Call: ci}
}

func decl(name string, params []check.Param, body check.Expr, sig *types.Func) *check.FnDecl {
	if sig == nil {
		var ps []types.FuncParam
		for _, p := range params {
			ps = append(ps, types.FuncParam{Type: p.Pat.Type()})
		}
		sig = types.Fn(body.Type(), ps...)
	}
	return &check.FnDecl{Sp: sp, Sym: fsym(name), Sig: sig, Params: params, Body: body}
}

func param(pat check.Pat) check.Param { return check.Param{Pat: pat} }

func lower1(t *testing.T, decls ...check.Decl) (*core.File, []diag.Diagnostic) {
	t.Helper()
	f := &check.File{Sp: sp, Package: "p", PkgSp: sp, Decls: decls}
	out, diags := File(f, nil)
	if out == nil {
		t.Fatal("File returned nil")
	}
	return out, diags
}

// mustLower lowers and fails on any diagnostic — including the
// validator violations File reports, so a green test means the
// output is validator-clean.
func mustLower(t *testing.T, decls ...check.Decl) *core.File {
	t.Helper()
	out, diags := lower1(t, decls...)
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %v", d)
	}
	return out
}

func fnDeclOf(t *testing.T, f *core.File, i int) *core.FuncDecl {
	t.Helper()
	d, ok := f.Decls[i].(*core.FuncDecl)
	if !ok {
		t.Fatalf("decl %d is %T, want FuncDecl", i, f.Decls[i])
	}
	return d
}

// --- tests ---

// TestEvalOrder: `fn f (x) = { let a = g x; h (g a) a }` — every
// complex operand binds in source order before its consumer.
func TestEvalOrder(t *testing.T) {
	x := vsym("x", types.Int)
	g := fsym("g")
	h := fsym("h")
	gCall := app(vr(g, types.Fn(types.Int, types.P(types.Int))), []check.Expr{vr(x, types.Int)},
		check.CallInfo{Kind: check.CallDirect, Callee: g, Arity: 1, Applied: 1, HoleIndex: -1}, types.Int)
	aSym := vsym("a", types.Int)
	body := &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.Let{Base: b(types.Unit), Pat: vpat(aSym, types.Int), Value: gCall},
		app(vr(h, types.Fn(types.Int, types.P(types.Int), types.P(types.Int))),
			[]check.Expr{
				app(vr(g, types.Fn(types.Int, types.P(types.Int))), []check.Expr{vr(aSym, types.Int)},
					check.CallInfo{Kind: check.CallDirect, Callee: g, Arity: 1, Applied: 1, HoleIndex: -1}, types.Int),
				vr(aSym, types.Int),
			},
			check.CallInfo{Kind: check.CallDirect, Callee: h, Arity: 2, Applied: 2, HoleIndex: -1}, types.Int),
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(x, types.Int))}, body, nil))
	fn := fnDeclOf(t, out, 0)
	// lets: a = g(x) ; tmp = g(a) ; tail = h(tmp, a)
	if len(fn.Fn.Body.Lets) != 2 {
		t.Fatalf("lets = %d, want 2", len(fn.Fn.Body.Lets))
	}
	if _, ok := fn.Fn.Body.Lets[0].X.(*core.CallDirect); !ok {
		t.Fatalf("let0 is %T, want CallDirect", fn.Fn.Body.Lets[0].X)
	}
	if fn.Fn.Body.Lets[0].Name.Sym != aSym {
		t.Errorf("let0 binds %v, want a's symbol", fn.Fn.Body.Lets[0].Name.Sym)
	}
	call, ok := fn.Fn.Body.Tail.(*core.CallDirect)
	if !ok {
		t.Fatalf("tail is %T, want CallDirect", fn.Fn.Body.Tail)
	}
	if _, ok := call.Args[0].(*core.Var); !ok {
		t.Errorf("arg0 is %T, want Var (ANF)", call.Args[0])
	}
	if call.Args[1].(*core.Var).Sym != aSym {
		t.Errorf("arg1 is not the a reference")
	}
}

// TestTopLevelTupleDestructure: `let (a, b) = pair` — the deep-pure
// path: one hidden VarDecl, then one VarDecl per bound var, each a
// pure projection.
func TestTopLevelTupleDestructure(t *testing.T) {
	a, bSym := vsym("a", types.Int), vsym("b", types.Int)
	tupT := types.Tup(types.Int, types.Int)
	pat := &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
		vpat(a, types.Int), vpat(bSym, types.Int),
	}}
	d := &check.LetDecl{Sp: sp, Pat: pat, Value: &check.TupleLit{
		Base: b(tupT), Elems: []check.Expr{intLit("1"), intLit("2")},
	}}
	out := mustLower(t, d)
	if len(out.Decls) != 3 {
		t.Fatalf("decls = %d, want 3 (tmp + a + b)", len(out.Decls))
	}
	tmp, ok := out.Decls[0].(*core.VarDecl)
	if !ok {
		t.Fatalf("decl0 is %T, want VarDecl", out.Decls[0])
	}
	if tmp.Sym.Name == "a" || tmp.Sym.Name == "b" {
		t.Errorf("tmp decl should hide a fresh symbol, got %q", tmp.Sym.Name)
	}
	va, ok := out.Decls[1].(*core.VarDecl)
	if !ok || va.Sym != a {
		t.Fatalf("decl1 = %v (sym %v), want VarDecl a", out.Decls[1], va)
	}
	fg, ok := va.Body.Tail.(*core.FieldGet)
	if !ok {
		t.Fatalf("a's body is %T, want FieldGet", va.Body.Tail)
	}
	if fg.Index != 0 || fg.Recv.(*core.Var).Sym != tmp.Sym {
		t.Errorf("a projects wrong: %+v", fg)
	}
	vb := out.Decls[2].(*core.VarDecl)
	if vb.Body.Tail.(*core.FieldGet).Index != 1 {
		t.Errorf("b projects index %d, want 1", vb.Body.Tail.(*core.FieldGet).Index)
	}
}

// TestTopLevelCtorDestructure: `let MkPair x y = p` where MkPair is
// the single variant of a local type — the tested path compiles to a
// match on the hidden temporary; malformed values panic, not silently
// project.
func TestTopLevelCtorDestructure(t *testing.T) {
	pair := &resolve.Symbol{Name: "Pair", Kind: resolve.KindType}
	mk := csym("MkPair")
	pairT := &types.Con{Name: "Pair"}
	td := &check.TypeDecl{Sp: sp, Sym: pair, Body: &check.SumType{
		Sp: sp,
		Variants: []check.Variant{{
			Sp: sp, Sym: mk,
			Fields: []check.TField{
				{Index: 0, Type: types.Int},
				{Index: 1, Type: types.Int},
			},
		}},
	}}
	x, y := vsym("x", types.Int), vsym("y", types.Int)
	pSym := vsym("p", pairT)
	ctorApp := app(vr(mk, types.Fn(pairT, types.P(types.Int), types.P(types.Int))),
		[]check.Expr{intLit("1"), intLit("2")},
		check.CallInfo{Kind: check.CallDirect, Callee: mk, Arity: 2, Applied: 2, HoleIndex: -1}, pairT)
	pd := &check.LetDecl{Sp: sp, Pat: vpat(pSym, pairT), Value: ctorApp}
	pat := &check.CtorPat{Base: b(pairT), Ctor: mk, Args: []check.CtorArg{
		{Pat: vpat(x, types.Int)}, {Pat: vpat(y, types.Int)},
	}}
	d := &check.LetDecl{Sp: sp, Pat: pat, Value: vr(pSym, pairT)}
	out := mustLower(t, td, pd, d)
	// decls: TypeDecl, p, tmp VarDecl, bound-tuple VarDecl, x, y
	if len(out.Decls) != 6 {
		t.Fatalf("decls = %d, want 6", len(out.Decls))
	}
	bound, ok := out.Decls[3].(*core.VarDecl)
	if !ok {
		t.Fatalf("decl3 is %T, want VarDecl (bound tuple)", out.Decls[3])
	}
	m, ok := bound.Body.Tail.(*core.Match)
	if !ok {
		t.Fatalf("bound body is %T, want Match", bound.Body.Tail)
	}
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok || sw.Kind != core.SwitchType {
		t.Fatalf("tree is %T, want DSwitch/SwitchType", m.Tree)
	}
	if len(sw.Cases) != 1 || sw.Cases[0].Test.(*core.DCtorTest).Ctor != mk {
		t.Fatalf("cases = %+v, want [MkPair]", sw.Cases)
	}
	if _, ok := sw.Default.(*core.DPanic); !ok {
		t.Errorf("default is %T, want DPanic", sw.Default)
	}
	vx := out.Decls[4].(*core.VarDecl)
	if vx.Sym != x {
		t.Errorf("decl4 sym = %v, want x", vx.Sym)
	}
}

// TestDirectOverApplied: a CallDirect with Applied>Arity splits into
// a saturated CallDirect bound to a temp plus a CallFunc.
func TestDirectOverApplied(t *testing.T) {
	mk := fsym("mk_add") // arity 1, returns Int->Int
	fsig := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	x := vsym("x", types.Int)
	y := vsym("y", types.Int)
	call := app(vr(mk, fsig), []check.Expr{vr(x, types.Int), vr(y, types.Int)},
		check.CallInfo{Kind: check.CallDirect, Callee: mk, Arity: 1, Applied: 2, HoleIndex: -1}, types.Int)
	out := mustLower(t, decl("f", []check.Param{param(vpat(x, types.Int)), param(vpat(y, types.Int))},
		call, types.Fn(types.Int, types.P(types.Int), types.P(types.Int))))
	fn := fnDeclOf(t, out, 0)
	if len(fn.Fn.Body.Lets) != 1 {
		t.Fatalf("lets = %d, want 1 (saturated prefix)", len(fn.Fn.Body.Lets))
	}
	prefix, ok := fn.Fn.Body.Lets[0].X.(*core.CallDirect)
	if !ok {
		t.Fatalf("let0 is %T, want CallDirect", fn.Fn.Body.Lets[0].X)
	}
	if len(prefix.Args) != 1 || prefix.Arity != 1 {
		t.Errorf("prefix = %+v, want arity-1 saturated", prefix)
	}
	cf, ok := fn.Fn.Body.Tail.(*core.CallFunc)
	if !ok {
		t.Fatalf("tail is %T, want CallFunc", fn.Fn.Body.Tail)
	}
	if len(cf.Args) != 1 || cf.Args[0].(*core.Var).Sym != y {
		t.Errorf("CallFunc args = %+v, want [y]", cf.Args)
	}
}

// TestPartialToClosure: CallPartial becomes an explicit Closure over
// the unfilled parameters — no half-filled call node.
func TestPartialToClosure(t *testing.T) {
	add := fsym("add")
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	rem := types.Fn(types.Int, types.P(types.Int))
	call := app(vr(add, sig), []check.Expr{intLit("1")},
		check.CallInfo{Kind: check.CallPartial, Callee: add, Arity: 2, Applied: 1, Unapplied: 1, HoleIndex: -1}, rem)
	out := mustLower(t, decl("f", nil, call, types.Fn(rem)))
	cl, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Closure)
	if !ok {
		t.Fatalf("tail is %T, want Closure", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	if len(cl.Fn.Params) != 1 {
		t.Fatalf("closure params = %d, want 1", len(cl.Fn.Params))
	}
	cd, ok := cl.Fn.Body.Tail.(*core.CallDirect)
	if !ok {
		t.Fatalf("closure body is %T, want CallDirect", cl.Fn.Body.Tail)
	}
	if len(cd.Args) != 2 || cd.Args[0].(*core.Lit).Text != "1" {
		t.Fatalf("closure call args = %+v", cd.Args)
	}
	if cd.Args[1].(*core.Var).Sym != cl.Fn.Params[0].Sym {
		t.Errorf("unfilled arg should be the closure param")
	}
}

// TestLabeledArgsOrder: labeled args evaluate in source order but
// land in declaration order.
func TestLabeledArgsOrder(t *testing.T) {
	f := fsym("f")
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	g := fsym("g")
	mkcall := func() *check.Apply {
		return app(vr(g, types.Fn(types.Int, types.P(types.Int))), []check.Expr{intLit("0")},
			check.CallInfo{Kind: check.CallDirect, Callee: g, Arity: 1, Applied: 1, HoleIndex: -1}, types.Int)
	}
	call := app(vr(f, sig), []check.Expr{mkcall(), mkcall()},
		check.CallInfo{Kind: check.CallDirect, Callee: f, Arity: 2, Applied: 2,
			ArgToParam: []int{1, 0}, HoleIndex: -1}, types.Int)
	out := mustLower(t, decl("main", nil, call, types.Fn(types.Int)))
	fn := fnDeclOf(t, out, 0)
	if len(fn.Fn.Body.Lets) != 2 {
		t.Fatalf("lets = %d, want 2 (two arg evals in source order)", len(fn.Fn.Body.Lets))
	}
	cd, ok := fn.Fn.Body.Tail.(*core.CallDirect)
	if !ok {
		t.Fatalf("tail is %T, want CallDirect", fn.Fn.Body.Tail)
	}
	// decl order: [param0, param1] = [second-eval, first-eval]
	if cd.Args[0].(*core.Var).Sym != fn.Fn.Body.Lets[1].Name.Sym ||
		cd.Args[1].(*core.Var).Sym != fn.Fn.Body.Lets[0].Name.Sym {
		t.Errorf("declaration order wrong: %v, %v", cd.Args[0], cd.Args[1])
	}
}

// TestCuteHole: `strconv.Atoi(_, 10)` becomes a one-param Closure
// whose body is the saturated CallGo — the captured arg evaluated
// once before the closure is built.
func TestCuteHole(t *testing.T) {
	g := gsym("strconv.Atoi")
	resT := types.Result(types.Int, types.GoError)
	call := app(vr(g, types.Fn(resT, types.P(types.String), types.P(types.Int))),
		[]check.Expr{
			&check.Hole{Base: b(types.String)},
			intLit("10"),
		},
		check.CallInfo{Kind: check.CallGo, Callee: g, Arity: 2, Applied: 2, HoleIndex: 0},
		types.Fn(resT, types.P(types.String)))
	out := mustLower(t, decl("f", nil, call, types.Fn(types.Fn(resT, types.P(types.String)))))
	cl, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Closure)
	if !ok {
		t.Fatalf("tail is %T, want Closure", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	if len(cl.Fn.Params) != 1 {
		t.Fatalf("params = %d, want 1 (the hole)", len(cl.Fn.Params))
	}
	rfg, ok := cl.Fn.Body.Tail.(*core.ResultFromGo)
	if !ok {
		t.Fatalf("closure tail is %T, want ResultFromGo", cl.Fn.Body.Tail)
	}
	call_ := rfg.Call
	if call_.Callee != g || len(call_.Args) != 2 {
		t.Fatalf("call = %+v", call_)
	}
	if call_.Args[0].(*core.Var).Sym != cl.Fn.Params[0].Sym {
		t.Errorf("hole arg should be the closure param")
	}
	if call_.Args[1].(*core.Lit).Text != "10" {
		t.Errorf("captured arg = %v", call_.Args[1])
	}
}

// TestGoMultiResultWrap: each multi-result shape gets exactly one
// consumer node.
func TestGoMultiResultWrap(t *testing.T) {
	g := gsym("read") // (T, error)
	resT := types.Result(types.Int, types.GoError)
	call := app(vr(g, types.Fn(resT, types.P(types.String))), []check.Expr{strLit("x")},
		check.CallInfo{Kind: check.CallGo, Callee: g, Arity: 1, Applied: 1, HoleIndex: -1}, resT)
	out := mustLower(t, decl("f", nil, call, types.Fn(resT)))
	tail := fnDeclOf(t, out, 0).Fn.Body.Tail
	rfg, ok := tail.(*core.ResultFromGo)
	if !ok {
		t.Fatalf("tail is %T, want ResultFromGo", tail)
	}
	if rfg.Call.Results != core.GoResultsErr {
		t.Errorf("results = %s, want err", rfg.Call.Results)
	}

	// (T, bool) kept as tuple → TupleFromGo
	g2 := gsym("lookup")
	tupT := types.Tup(types.Int, types.Bool)
	call2 := app(vr(g2, types.Fn(tupT, types.P(types.String))), []check.Expr{strLit("k")},
		check.CallInfo{Kind: check.CallGo, Callee: g2, Arity: 1, Applied: 1, HoleIndex: -1}, tupT)
	out2 := mustLower(t, decl("f2", nil, call2, types.Fn(tupT)))
	tail2 := fnDeclOf(t, out2, 0).Fn.Body.Tail
	if _, ok := tail2.(*core.TupleFromGo); !ok {
		t.Fatalf("tail2 is %T, want TupleFromGo", tail2)
	}

	// (T, bool) declared comma-ok → OptionFromGo
	g3 := gsym("get")
	optT := types.Option(types.Int)
	call3 := app(vr(g3, types.Fn(optT, types.P(types.String))), []check.Expr{strLit("k")},
		check.CallInfo{Kind: check.CallGo, Callee: g3, Arity: 1, Applied: 1, HoleIndex: -1}, optT)
	env := &Env{GoSig: func(sym *resolve.Symbol) (GoSig, bool) {
		return GoSig{Arity: 1, Results: core.GoResultsOK}, true
	}}
	out3, diags := func() (*core.File, []diag.Diagnostic) {
		f := &check.File{Sp: sp, Package: "p", PkgSp: sp,
			Decls: []check.Decl{decl("f3", nil, call3, types.Fn(optT))}}
		return File(f, env)
	}()
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %v", d)
	}
	tail3 := fnDeclOf(t, out3, 0).Fn.Body.Tail
	if _, ok := tail3.(*core.OptionFromGo); !ok {
		t.Fatalf("tail3 is %T, want OptionFromGo", tail3)
	}
}

// TestMatchCtorSwitch: ctor arms compile to SwitchType with a DPanic
// default; a wildcard arm expands to the remaining variants' cases.
func TestMatchCtorSwitch(t *testing.T) {
	shape := &resolve.Symbol{Name: "Shape", Kind: resolve.KindType}
	circ, rect := csym("Circle"), csym("Rect")
	shapeT := &types.Con{Name: "Shape"}
	td := &check.TypeDecl{Sp: sp, Sym: shape, Body: &check.SumType{
		Sp: sp,
		Variants: []check.Variant{
			{Sp: sp, Sym: circ, Fields: []check.TField{{Index: 0, Type: types.Float}}},
			{Sp: sp, Sym: rect, Fields: []check.TField{{Index: 0, Type: types.Float}, {Index: 1, Type: types.Float}}},
		},
	}}
	r := vsym("r", types.Float)
	subj := vsym("s", shapeT)
	body := &check.Match{Base: b(types.Float), Subj: vr(subj, shapeT), Arms: []check.MatchArm{
		{Sp: sp, Pat: &check.CtorPat{Base: b(shapeT), Ctor: circ,
			Args: []check.CtorArg{{Pat: vpat(r, types.Float)}}},
			Body: vr(r, types.Float)},
		{Sp: sp, Pat: wpat(shapeT), Body: lit(syntax.LitFloat, "0", types.Float)},
	}}
	out := mustLower(t, td, decl("area", []check.Param{param(vpat(subj, shapeT))}, body, nil))
	fn := fnDeclOf(t, out, 1)
	m, ok := fn.Fn.Body.Tail.(*core.Match)
	if !ok {
		t.Fatalf("tail is %T, want Match", fn.Fn.Body.Tail)
	}
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok || sw.Kind != core.SwitchType {
		t.Fatalf("tree = %T kind %v", m.Tree, sw)
	}
	if len(sw.Cases) != 2 {
		t.Fatalf("cases = %d, want 2 (wildcard expands to Rect)", len(sw.Cases))
	}
	if _, ok := sw.Default.(*core.DPanic); !ok {
		t.Errorf("default = %T, want DPanic", sw.Default)
	}
	// Circle case binds the narrowed subject.
	c0 := sw.Cases[0]
	if len(c0.Binds) != 1 {
		t.Fatalf("Circle case binds = %d, want 1", len(c0.Binds))
	}
	// The r bind is a let projecting V0 of the narrowed subject.
	var foundFG bool
	for _, lt := range c0.Body.Lets {
		if fg, ok := lt.X.(*core.FieldGet); ok && fg.Index == 0 {
			foundFG = true
		}
	}
	if !foundFG {
		t.Errorf("no V0 projection found in Circle case")
	}
}

// TestMatchLitSwitch: literal arms become SwitchValue cases; the
// wildcard arm is the default continuation.
func TestMatchLitSwitch(t *testing.T) {
	subj := vsym("n", types.Int)
	x := vsym("x", types.Int)
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, types.Int), Arms: []check.MatchArm{
		{Sp: sp, Pat: &check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "0"},
			Body: intLit("100")},
		{Sp: sp, Pat: vpat(x, types.Int), Body: vr(x, types.Int)},
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(subj, types.Int))}, body, nil))
	m, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Match)
	if !ok {
		t.Fatalf("tail is %T", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok || sw.Kind != core.SwitchValue {
		t.Fatalf("tree = %T", m.Tree)
	}
	if len(sw.Cases) != 1 || sw.Cases[0].Test.(*core.DLitTest).Text != "0" {
		t.Fatalf("cases = %+v", sw.Cases)
	}
	if _, ok := sw.Default.(*core.DLeaf); !ok {
		t.Fatalf("default = %T, want DLeaf (the x arm)", sw.Default)
	}
}

// TestMatchGuard: a guarded arm emits DGuard; failure continues to
// the remaining arms.
func TestMatchGuard(t *testing.T) {
	subj := vsym("n", types.Int)
	x := vsym("x", types.Int)
	pred := fsym("pred")
	guard := app(vr(pred, types.Fn(types.Bool, types.P(types.Int))), []check.Expr{vr(x, types.Int)},
		check.CallInfo{Kind: check.CallDirect, Callee: pred, Arity: 1, Applied: 1, HoleIndex: -1}, types.Bool)
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, types.Int), Arms: []check.MatchArm{
		{Sp: sp, Pat: vpat(x, types.Int), Guard: guard, Body: vr(x, types.Int)},
		{Sp: sp, Pat: wpat(types.Int), Body: intLit("0")},
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(subj, types.Int))}, body, nil))
	m := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Match)
	g, ok := m.Tree.(*core.DGuard)
	if !ok {
		t.Fatalf("tree = %T, want DGuard", m.Tree)
	}
	if _, ok := g.Then.(*core.DLeaf); !ok {
		t.Errorf("then = %T", g.Then)
	}
	if _, ok := g.Else.(*core.DLeaf); !ok {
		t.Errorf("else = %T, want DLeaf (wildcard)", g.Else)
	}
}

// TestListSwitch: `[x, ..rest]` compiles to SwitchList with head/tail
// binds and a DPanic default.
func TestListSwitch(t *testing.T) {
	subj := vsym("xs", types.List(types.Int))
	h, rest := vsym("h", types.Int), vsym("rest", types.List(types.Int))
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, types.List(types.Int)), Arms: []check.MatchArm{
		{Sp: sp, Pat: &check.ListPat{Base: b(types.List(types.Int)),
			Elems: []check.Pat{vpat(h, types.Int)}, Rest: vpat(rest, types.List(types.Int))},
			Body: vr(h, types.Int)},
		{Sp: sp, Pat: &check.ListPat{Base: b(types.List(types.Int))}, Body: intLit("0")},
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(subj, types.List(types.Int)))}, body, nil))
	m := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Match)
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok || sw.Kind != core.SwitchList {
		t.Fatalf("tree = %T", m.Tree)
	}
	if len(sw.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(sw.Cases))
	}
	var cons *core.DCase
	for i := range sw.Cases {
		if sw.Cases[i].Test.(*core.DListTest).Cons {
			cons = &sw.Cases[i]
		}
	}
	if cons == nil || len(cons.Binds) != 2 {
		t.Fatalf("cons case = %+v", cons)
	}
	if _, ok := sw.Default.(*core.DPanic); !ok {
		t.Errorf("default = %T, want DPanic", sw.Default)
	}
}

// TestOrPat: or-pattern alternatives splice in order; each preserves
// its bindings.
func TestOrPat(t *testing.T) {
	subj := vsym("n", types.Int)
	x := vsym("x", types.Int)
	or := &check.OrPat{Base: b(types.Int), Alts: []check.Pat{
		&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "0"},
		vpat(x, types.Int),
	}}
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, types.Int), Arms: []check.MatchArm{
		{Sp: sp, Pat: or, Body: intLit("1")},
		{Sp: sp, Pat: wpat(types.Int), Body: intLit("2")},
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(subj, types.Int))}, body, nil))
	m := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Match)
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok || sw.Kind != core.SwitchValue {
		t.Fatalf("tree = %T", m.Tree)
	}
	if _, ok := sw.Default.(*core.DLeaf); !ok {
		t.Fatalf("default = %T, want DLeaf (or-alt x, then _)", sw.Default)
	}
}

// TestTry: `try e` on a bound Result is EscapeResult; on a direct Go
// (T,error) call it fuses to EscapeCall.
func TestTry(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	okSym := csym("Ok")
	okApp := func(v *resolve.Symbol) *check.Apply {
		return app(vr(okSym, types.Fn(resT, types.P(types.Int))), []check.Expr{vr(v, types.Int)},
			check.CallInfo{Kind: check.CallDirect, Callee: okSym, Arity: 1, Applied: 1, HoleIndex: -1}, resT)
	}
	r := vsym("r", resT)
	v := vsym("v", types.Int)
	fn1 := decl("f", []check.Param{param(vpat(r, resT))},
		&check.Block{Base: b(resT), Stmts: []check.Expr{
			&check.Let{Base: b(types.Unit), Pat: vpat(v, types.Int),
				Value: &check.Try{Base: b(types.Int), X: vr(r, resT)}},
			okApp(v),
		}},
		types.Fn(resT, types.P(resT)))
	out := mustLower(t, fn1)
	er, ok := fnDeclOf(t, out, 0).Fn.Body.Lets[0].X.(*core.EscapeResult)
	if !ok {
		t.Fatalf("let0 is %T, want EscapeResult", fnDeclOf(t, out, 0).Fn.Body.Lets[0].X)
	}
	if er.To != fnDeclOf(t, out, 0).Fn || er.Wrap != "" {
		t.Errorf("escape = %+v", er)
	}
	if _, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CtorApp); !ok {
		t.Fatalf("tail = %T, want Ok CtorApp", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}

	g := gsym("atoi")
	call := app(vr(g, types.Fn(resT, types.P(types.String))), []check.Expr{strLit("1")},
		check.CallInfo{Kind: check.CallGo, Callee: g, Arity: 1, Applied: 1, HoleIndex: -1}, resT)
	v2 := vsym("v2", types.Int)
	fn2 := decl("g2", nil,
		&check.Block{Base: b(resT), Stmts: []check.Expr{
			&check.Let{Base: b(types.Unit), Pat: vpat(v2, types.Int),
				Value: &check.Try{Base: b(types.Int), X: call}},
			okApp(v2),
		}}, types.Fn(resT))
	out2 := mustLower(t, fn2)
	ec, ok := fnDeclOf(t, out2, 0).Fn.Body.Lets[0].X.(*core.EscapeCall)
	if !ok {
		t.Fatalf("let0 is %T, want EscapeCall", fnDeclOf(t, out2, 0).Fn.Body.Lets[0].X)
	}
	if ec.Call.Callee != g || ec.Wrap != "" {
		t.Errorf("escape call = %+v", ec.Call)
	}
}

// TestPipeTry: `Ok x |?> f` unwraps the inflow (Wrap=""), applies the
// step, escapes on its Result with the step name, and re-wraps in Ok.
func TestPipeTry(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	f := fsym("f")
	fsig := types.Fn(resT, types.P(types.Int))
	subj := vsym("r", resT)
	pipe := &check.Binary{Base: b(resT), Op: syntax.BinPipeTry,
		X: vr(subj, resT), Y: vr(f, fsig)}
	d := decl("g", []check.Param{param(vpat(subj, resT))}, pipe, types.Fn(resT, types.P(resT)))
	out := mustLower(t, d)
	body := fnDeclOf(t, out, 0).Fn.Body
	// lets: in = escape(r) ; step = f(in) ; payload = escape-wrap(step)
	if len(body.Lets) != 3 {
		t.Fatalf("lets = %d, want 3", len(body.Lets))
	}
	escIn, ok := body.Lets[0].X.(*core.EscapeResult)
	if !ok || escIn.Wrap != "" {
		t.Fatalf("inflow escape = %+v", body.Lets[0].X)
	}
	if _, ok := body.Lets[1].X.(*core.CallDirect); !ok {
		t.Fatalf("step call = %T", body.Lets[1].X)
	}
	escStep, ok := body.Lets[2].X.(*core.EscapeResult)
	if !ok || escStep.Wrap != "f" {
		t.Fatalf("step escape = %+v, want Wrap \"f\"", body.Lets[2].X)
	}
	okApp, ok := body.Tail.(*core.CtorApp)
	if !ok || okApp.Ctor.Name != "Ok" {
		t.Fatalf("tail = %T, want Ok CtorApp", body.Tail)
	}
}

// TestPipeTryBoundMeta: `let r = inc(1); r |?> inc` where the
// let-bound `r` still carries a bound `?n` — the checker does not
// always zonk a let-inferred binding's type. pipeTry must chase the
// meta before extracting the payload: before the fix the whole
// Result atom fed `inc` (`inc(r)`), now `inc(payload)`.
func TestPipeTryBoundMeta(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	mt := &types.Meta{ID: 41}
	mt.Bind(resT)
	f := fsym("inc")
	fsig := types.Fn(resT, types.P(types.Int))
	subj := vsym("r", resT)
	pipe := &check.Binary{Base: b(resT), Op: syntax.BinPipeTry,
		X: vr(subj, mt), Y: vr(f, fsig)}
	d := decl("run", []check.Param{param(vpat(subj, resT))}, pipe, types.Fn(resT, types.P(resT)))
	out := mustLower(t, d)
	body := fnDeclOf(t, out, 0).Fn.Body
	// Same three-let shape as TestPipeTry: the inflow escape unwraps
	// the Result, the step consumes its payload.
	if len(body.Lets) != 3 {
		t.Fatalf("lets = %d, want 3 (inflow escape, step, step escape)", len(body.Lets))
	}
	escIn, ok := body.Lets[0].X.(*core.EscapeResult)
	if !ok || escIn.Wrap != "" {
		t.Fatalf("inflow escape = %+v — the bound meta hid the Result shape", body.Lets[0].X)
	}
	if !types.Equal(escIn.Typ, types.Int) {
		t.Errorf("escape payload type = %s, want Int", escIn.Typ)
	}
	cd, ok := body.Lets[1].X.(*core.CallDirect)
	if !ok {
		t.Fatalf("step call = %T, want CallDirect", body.Lets[1].X)
	}
	if arg, ok := cd.Args[0].(*core.Var); !ok || arg.Sym != body.Lets[0].Name.Sym {
		t.Errorf("step arg = %+v, want the escape's payload bind", cd.Args[0])
	}
}

// TestMatchBoundMetaSubject: `let r = inc(1); match r { ... }` where
// the subject's type is a bound `?n` — ctorSwitch must chase before
// enumerating variants. Before the fix this reported E0901 "cannot
// enumerate variants of Result Int GoError" and emitted DPanic.
func TestMatchBoundMetaSubject(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	mt := &types.Meta{ID: 42}
	mt.Bind(resT)
	subj := vsym("r", resT)
	n := vsym("n", types.Int)
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, mt), Arms: []check.MatchArm{
		mArm(&check.CtorPat{Base: b(resT), Ctor: csym("Ok"),
			Args: []check.CtorArg{{Pat: vpat(n, types.Int)}}}, vr(n, types.Int)),
		mArm(wpat(resT), intLit("0")),
	}}
	out := mustLower(t, decl("run", []check.Param{param(vpat(subj, resT))}, body,
		types.Fn(types.Int, types.P(resT))))
	m, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Match)
	if !ok {
		t.Fatalf("tail is %T, want Match", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok || sw.Kind != core.SwitchTag {
		t.Fatalf("tree = %T, want DSwitch SwitchTag", m.Tree)
	}
	if len(sw.Cases) != 2 {
		t.Fatalf("cases = %d, want 2 (Ok, Err)", len(sw.Cases))
	}
}

// TestTailCall: a self call in tail position becomes TailCall and
// marks the function TailLoop; a non-tail self call does not.
func TestTailCall(t *testing.T) {
	self := fsym("loop")
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Bool))
	x := vsym("x", types.Int)
	cond := vsym("c", types.Bool)
	selfCall := func() *check.Apply {
		return app(vr(self, sig), []check.Expr{vr(x, types.Int), vr(cond, types.Bool)},
			check.CallInfo{Kind: check.CallDirect, Callee: self, Arity: 2, Applied: 2, HoleIndex: -1}, types.Int)
	}
	body := &check.If{Base: b(types.Int), Cond: vr(cond, types.Bool),
		Then: selfCall(), Else: intLit("0")}
	d := decl("loop", []check.Param{param(vpat(x, types.Int)), param(vpat(cond, types.Bool))},
		body, types.Fn(types.Int, types.P(types.Int), types.P(types.Bool)))
	d.Sym = self
	out := mustLower(t, d)
	fn := fnDeclOf(t, out, 0)
	if !fn.Fn.TailLoop {
		t.Fatal("TailLoop not set")
	}
	ife := fn.Fn.Body.Tail.(*core.If)
	tc, ok := ife.Then.Tail.(*core.TailCall)
	if !ok {
		t.Fatalf("then tail = %T, want TailCall", ife.Then.Tail)
	}
	if tc.To != fn.Fn {
		t.Errorf("TailCall.To is not the enclosing Func")
	}
	if _, ok := ife.Else.Tail.(*core.Lit); !ok {
		t.Errorf("else tail = %T", ife.Else.Tail)
	}

	// Negative: self call bound to a let is not tail position.
	d2 := decl("loop2", []check.Param{param(vpat(x, types.Int))}, unitLit(),
		types.Fn(types.Int, types.P(types.Int)))
	selfCall2 := func() *check.Apply {
		return app(vr(d2.Sym, d2.Sig), []check.Expr{vr(x, types.Int)},
			check.CallInfo{Kind: check.CallDirect, Callee: d2.Sym, Arity: 1, Applied: 1, HoleIndex: -1}, types.Int)
	}
	body2 := &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.Let{Base: b(types.Unit), Pat: vpat(vsym("v", types.Int), types.Int), Value: selfCall2()},
		intLit("0"),
	}}
	d2.Body = body2
	out2 := mustLower(t, d2)
	fn2 := fnDeclOf(t, out2, 0)
	if fn2.Fn.TailLoop {
		t.Fatal("TailLoop set on a non-tail self call")
	}
	if _, ok := fn2.Fn.Body.Lets[0].X.(*core.CallDirect); !ok {
		t.Fatalf("let0 = %T, want CallDirect (not rewritten)", fn2.Fn.Body.Lets[0].X)
	}
}

// TestTailCallCaptureSnapshot: a TailLoop parameter referenced from a
// nested closure gets a per-iteration copy `n' = n` at the body's
// head, and every later use — the capture, synchronous reads, the
// TailCall argument — reads n' (tail.go;
// docs/problems/2026-11-02-tailloop-param-capture.md).
func TestTailCallCaptureSnapshot(t *testing.T) {
	n := vsym("n", types.Int)
	fnUI := types.Fn(types.Int, types.P(types.Unit))
	self := fsym("loop")
	sig := types.Fn(types.Int, types.P(types.Int))
	d := decl("loop", []check.Param{param(vpat(n, types.Int))},
		&check.Block{Base: b(types.Int), Stmts: []check.Expr{
			&check.Let{Base: b(types.Unit), Pat: vpat(vsym("g", fnUI), fnUI),
				Value: &check.Lambda{Base: b(fnUI),
					Params: []check.Param{param(vpat(vsym("u", types.Unit), types.Unit))},
					Body:   vr(n, types.Int)}},
			app(vr(self, sig), []check.Expr{vr(n, types.Int)},
				check.CallInfo{Kind: check.CallDirect, Callee: self, Arity: 1, Applied: 1, HoleIndex: -1}, types.Int),
		}},
		sig)
	d.Sym = self
	out := mustLower(t, d)
	fn := fnDeclOf(t, out, 0)
	if !fn.Fn.TailLoop {
		t.Fatal("TailLoop not set")
	}
	body := fn.Fn.Body
	// The head let is the per-iteration copy `n' = n`; the param
	// register keeps its symbol.
	cp := body.Lets[0]
	cv, ok := cp.X.(*core.Var)
	if !ok || cv.Sym != n {
		t.Fatalf("Lets[0].X = %#v, want Var(param n)", cp.X)
	}
	snap := cp.Name.Sym
	if snap == nil || snap == n {
		t.Fatalf("snapshot binder = %v, want a fresh symbol", cp.Name.Sym)
	}
	if fn.Fn.Params[0].Sym != n {
		t.Fatalf("param symbol changed: %v", fn.Fn.Params[0].Sym)
	}
	cl, ok := body.Lets[1].X.(*core.Closure)
	if !ok {
		t.Fatalf("Lets[1].X = %T, want Closure", body.Lets[1].X)
	}
	if v, ok := cl.Fn.Body.Tail.(*core.Var); !ok || v.Sym != snap {
		t.Fatalf("closure tail = %#v, want Var(snapshot)", cl.Fn.Body.Tail)
	}
	tc, ok := body.Tail.(*core.TailCall)
	if !ok {
		t.Fatalf("tail = %T, want TailCall", body.Tail)
	}
	if v, ok := tc.Args[0].(*core.Var); !ok || v.Sym != snap {
		t.Fatalf("tail arg = %#v, want Var(snapshot)", tc.Args[0])
	}
}

// TestTailCallNoCaptureNoSnapshot: a TailLoop parameter used only
// synchronously keeps its symbol — no copy let is synthesized.
func TestTailCallNoCaptureNoSnapshot(t *testing.T) {
	n := vsym("n", types.Int)
	self := fsym("loop")
	sig := types.Fn(types.Int, types.P(types.Int))
	d := decl("loop", []check.Param{param(vpat(n, types.Int))},
		app(vr(self, sig), []check.Expr{vr(n, types.Int)},
			check.CallInfo{Kind: check.CallDirect, Callee: self, Arity: 1, Applied: 1, HoleIndex: -1}, types.Int),
		sig)
	d.Sym = self
	out := mustLower(t, d)
	body := fnDeclOf(t, out, 0).Fn.Body
	if len(body.Lets) != 0 {
		t.Fatalf("lets = %v, want none (no snapshot)", body.Lets)
	}
	tc, ok := body.Tail.(*core.TailCall)
	if !ok {
		t.Fatalf("tail = %T, want TailCall", body.Tail)
	}
	if v := tc.Args[0].(*core.Var); v.Sym != n {
		t.Fatalf("tail arg sym = %v, want param n", v.Sym)
	}
}

// TestPipeFwd: `x |> f a` appends x at the lowest unfilled parameter.
func TestPipeFwd(t *testing.T) {
	f := fsym("f")
	sig := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	x := vsym("x", types.Int)
	rhs := app(vr(f, sig), []check.Expr{intLit("7")},
		check.CallInfo{Kind: check.CallPartial, Callee: f, Arity: 2, Applied: 1, Unapplied: 1, HoleIndex: -1},
		types.Fn(types.Int, types.P(types.Int)))
	pipe := &check.Binary{Base: b(types.Int), Op: syntax.BinPipeFwd, X: vr(x, types.Int), Y: rhs}
	out := mustLower(t, decl("g", []check.Param{param(vpat(x, types.Int))}, pipe, types.Fn(types.Int, types.P(types.Int))))
	cd, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallDirect)
	if !ok {
		t.Fatalf("tail = %T, want saturated CallDirect", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	if len(cd.Args) != 2 || cd.Args[0].(*core.Lit).Text != "7" || cd.Args[1].(*core.Var).Sym != x {
		t.Fatalf("pipe args = %+v", cd.Args)
	}
}

// TestSelectEvalOrder: all channel exprs and send values evaluate
// once, in source order, before the select.
func TestSelectEvalOrder(t *testing.T) {
	chT := types.Chan(types.Int)
	ch1 := vsym("ch1", chT)
	ch2 := vsym("ch2", chT)
	g := fsym("g")
	gsig := types.Fn(chT, types.P(types.Int))
	sel := &check.SelectExpr{Base: b(types.Int), Arms: []check.SelectArm{
		&check.RecvArm{Sp: sp, Bind: vpat(vsym("v", types.Option(types.Int)), types.Option(types.Int)),
			Chan: app(vr(g, gsig), []check.Expr{intLit("1")},
				check.CallInfo{Kind: check.CallDirect, Callee: g, Arity: 1, Applied: 1, HoleIndex: -1}, chT),
			Body: intLit("1")},
		&check.SendArm{Sp: sp,
			Chan: app(vr(g, gsig), []check.Expr{intLit("2")},
				check.CallInfo{Kind: check.CallDirect, Callee: g, Arity: 1, Applied: 1, HoleIndex: -1}, chT),
			Value: app(vr(fsym("h"), types.Fn(types.Int, types.P(types.Int))), []check.Expr{intLit("3")},
				check.CallInfo{Kind: check.CallDirect, Callee: fsym("h"), Arity: 1, Applied: 1, HoleIndex: -1}, types.Int),
			Body: intLit("2")},
		&check.DefaultArm{Sp: sp, Body: intLit("0")},
	}}
	_ = ch1
	_ = ch2
	out := mustLower(t, decl("f", nil, sel, types.Fn(types.Int)))
	body := fnDeclOf(t, out, 0).Fn.Body
	// three complex operands bind before the select node.
	if len(body.Lets) != 3 {
		t.Fatalf("lets = %d, want 3 (ch1, ch2, sendval)", len(body.Lets))
	}
	se, ok := body.Tail.(*core.SelectExpr)
	if !ok {
		t.Fatalf("tail = %T, want SelectExpr", body.Tail)
	}
	if len(se.Arms) != 3 {
		t.Fatalf("arms = %d", len(se.Arms))
	}
	if _, ok := se.Arms[0].(*core.SelRecv); !ok {
		t.Errorf("arm0 = %T", se.Arms[0])
	}
	if _, ok := se.Arms[1].(*core.SelSend); !ok {
		t.Errorf("arm1 = %T", se.Arms[1])
	}
	if _, ok := se.Arms[2].(*core.SelDefault); !ok {
		t.Errorf("arm2 = %T", se.Arms[2])
	}
}

// TestForAndGo: `for x in xs { go { f x } }` — Spawn inside a loop.
func TestForAndGo(t *testing.T) {
	xs := vsym("xs", types.List(types.Int))
	x := vsym("x", types.Int)
	f := fsym("f")
	body := &check.For{Base: b(types.Unit), Pat: vpat(x, types.Int),
		Iter: vr(xs, types.List(types.Int)),
		Body: &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
			&check.GoExpr{Base: b(types.Unit), Body: &check.Block{Base: b(types.Unit), Stmts: []check.Expr{
				app(vr(f, types.Fn(types.Unit, types.P(types.Int))), []check.Expr{vr(x, types.Int)},
					check.CallInfo{Kind: check.CallDirect, Callee: f, Arity: 1, Applied: 1, HoleIndex: -1}, types.Unit),
			}}},
		}}}
	out := mustLower(t, decl("main", []check.Param{param(vpat(xs, types.List(types.Int)))}, body, types.Fn(types.Unit, types.P(types.List(types.Int)))))
	fr, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.ForRange)
	if !ok {
		t.Fatalf("tail = %T, want ForRange", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	if fr.Kind != core.ForList {
		t.Errorf("kind = %s, want ForList", fr.Kind)
	}
	sp_, ok := fr.Body.Tail.(*core.Spawn)
	if !ok {
		t.Fatalf("loop body = %T, want Spawn", fr.Body.Tail)
	}
	_ = sp_
}

// TestFuncRefFlatten: a function returning a function, referenced at
// its full flat type, lowers to a flatten Closure (P0-2 rule 3).
func TestFuncRefFlatten(t *testing.T) {
	fsig := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	// declared arity 1, flat type arity 2.
	d := decl("mk_add", []check.Param{param(vpat(vsym("a", types.Int), types.Int))},
		&check.Lambda{Base: b(types.Fn(types.Int, types.P(types.Int))),
			Params: []check.Param{param(vpat(vsym("b", types.Int), types.Int))},
			Body:   intLit("0")},
		types.Fn(types.Fn(types.Int, types.P(types.Int)), types.P(types.Int)))
	d2 := decl("use", nil, vr(d.Sym, fsig), types.Fn(fsig))
	out := mustLower(t, d, d2)
	cl, ok := fnDeclOf(t, out, 1).Fn.Body.Tail.(*core.Closure)
	if !ok {
		t.Fatalf("tail = %T, want flatten Closure", fnDeclOf(t, out, 1).Fn.Body.Tail)
	}
	if len(cl.Fn.Params) != 2 {
		t.Fatalf("flatten params = %d, want 2", len(cl.Fn.Params))
	}
	if len(cl.Fn.Body.Lets) != 1 {
		t.Fatalf("flatten lets = %d, want 1 (saturated prefix)", len(cl.Fn.Body.Lets))
	}
	cd, ok := cl.Fn.Body.Lets[0].X.(*core.CallDirect)
	if !ok || cd.Arity != 1 || len(cd.Args) != 1 {
		t.Fatalf("flatten prefix = %T", cl.Fn.Body.Lets[0].X)
	}
	if _, ok := cl.Fn.Body.Tail.(*core.CallFunc); !ok {
		t.Fatalf("flatten tail = %T, want CallFunc on the remainder", cl.Fn.Body.Tail)
	}
}

// TestStrInterp: interpolation lowers to display/concat builtins.
func TestStrInterp(t *testing.T) {
	x := vsym("x", types.Int)
	e := &check.StrInterp{Base: b(types.String), Parts: []check.StrPart{
		&check.StrText{Sp: sp, Text: "n="},
		&check.StrExpr{Sp: sp, X: vr(x, types.Int)},
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(x, types.Int))}, e, types.Fn(types.String, types.P(types.Int))))
	body := fnDeclOf(t, out, 0).Fn.Body
	cb, ok := body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "str_concat" {
		t.Fatalf("tail = %T, want str_concat CallBuiltin", body.Tail)
	}
	if len(cb.Args) != 2 {
		t.Fatalf("concat args = %d", len(cb.Args))
	}
}

// TestUnknownCall: CallUnknown chunks by the value's own arity.
func TestUnknownCall(t *testing.T) {
	fT := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	f := vsym("f", fT)
	call := app(vr(f, fT), []check.Expr{intLit("1"), intLit("2")},
		check.CallInfo{Kind: check.CallUnknown, Arity: 2, Applied: 2, HoleIndex: -1}, types.Int)
	out := mustLower(t, decl("g", []check.Param{param(vpat(f, fT))}, call,
		types.Fn(types.Int, types.P(fT))))
	cf, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallFunc)
	if !ok {
		t.Fatalf("tail = %T, want CallFunc", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	if len(cf.Args) != 2 {
		t.Fatalf("args = %d", len(cf.Args))
	}
}

// TestUnknownCallCalleeBindsFirst pins spec section 2 rule 4's
// left-to-right order at the apply boundary — `getF () (eff ())`
// must bind the getF temporary before the eff one: the callee
// evaluates before the arguments (eval's evalApply agrees). Before
// the fix the callee's Let was emitted by unknownCall *after*
// argAtoms, so compiled code ran eff first where eval ran getF.
func TestUnknownCallCalleeBindsFirst(t *testing.T) {
	fT := types.Fn(types.Int, types.P(types.Int))
	getF := fsym("getF")
	eff := fsym("eff")
	callee := app(vr(getF, types.Fn(fT)), nil,
		check.CallInfo{Kind: check.CallDirect, Callee: getF, Arity: 0, Applied: 0, HoleIndex: -1}, fT)
	arg := app(vr(eff, types.Fn(types.Int)), nil,
		check.CallInfo{Kind: check.CallDirect, Callee: eff, Arity: 0, Applied: 0, HoleIndex: -1}, types.Int)
	call := app(callee, []check.Expr{arg},
		check.CallInfo{Kind: check.CallUnknown, Arity: 1, Applied: 1, HoleIndex: -1}, types.Int)
	out := mustLower(t, decl("g", nil, call, types.Fn(types.Int)))
	body := fnDeclOf(t, out, 0).Fn.Body
	if len(body.Lets) != 2 {
		t.Fatalf("lets = %d, want 2 — the callee temp, then the arg temp", len(body.Lets))
	}
	if cd, ok := body.Lets[0].X.(*core.CallDirect); !ok || cd.Callee != getF {
		t.Fatalf("lets[0].X = %#v, want the getF CallDirect bound first", body.Lets[0].X)
	}
	if cd, ok := body.Lets[1].X.(*core.CallDirect); !ok || cd.Callee != eff {
		t.Fatalf("lets[1].X = %#v, want the eff CallDirect bound second", body.Lets[1].X)
	}
	cf, ok := body.Tail.(*core.CallFunc)
	if !ok {
		t.Fatalf("tail = %T, want CallFunc", body.Tail)
	}
	if v, ok := cf.Fn.(*core.Var); !ok || v.Sym != body.Lets[0].Name.Sym {
		t.Fatalf("CallFunc.Fn = %#v, want the var lets[0] binds", cf.Fn)
	}
}

// TestCompose: `f >> g` becomes a closure applying f then g.
func TestCompose(t *testing.T) {
	f := fsym("f")
	g := fsym("g")
	is := types.Fn(types.Int, types.P(types.Int))
	compT := types.Fn(types.Int, types.P(types.Int))
	e := &check.Binary{Base: b(compT), Op: syntax.BinComposeFwd,
		X: vr(f, is), Y: vr(g, is)}
	out := mustLower(t, decl("main", nil, e, types.Fn(compT)))
	cl, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Closure)
	if !ok {
		t.Fatalf("tail = %T, want Closure", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	if len(cl.Fn.Body.Lets) != 1 {
		t.Fatalf("closure lets = %d, want 1 (f call)", len(cl.Fn.Body.Lets))
	}
	if _, ok := cl.Fn.Body.Tail.(*core.CallDirect); !ok {
		t.Fatalf("closure tail = %T, want CallDirect g", cl.Fn.Body.Tail)
	}
}

// TestDiagnosticsOnContractGap: an escape with no boundary reports a
// diagnostic instead of panicking.
func TestDiagnosticsOnContractGap(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	r := vsym("r", resT)
	okSym := csym("Ok")
	rd := &check.LetDecl{Sp: sp, Pat: vpat(r, resT),
		Value: app(vr(okSym, types.Fn(resT, types.P(types.Int))), []check.Expr{intLit("1")},
			check.CallInfo{Kind: check.CallDirect, Callee: okSym, Arity: 1, Applied: 1, HoleIndex: -1}, resT)}
	// top-level let has no function boundary.
	d := &check.LetDecl{Sp: sp, Pat: vpat(vsym("v", types.Int), types.Int),
		Value: &check.Try{Base: b(types.Int), X: vr(r, resT)}}
	_, diags := lower1(t, rd, d)
	if len(diags) != 1 {
		t.Fatalf("diags = %d, want exactly 1 (the boundary gap degrades "+
			"to the unescaped operand, not a second validator violation)", len(diags))
	}
}

// --- review regression tests ---

func mArm(pat check.Pat, body check.Expr) check.MatchArm {
	return check.MatchArm{Sp: sp, Pat: pat, Body: body}
}

// shape3 declares a three-variant sum and returns its TypeDecl, the
// type, and the constructor symbols — the workhorse ADT of the
// decision-tree regression tests.
func shape3() (*check.TypeDecl, *types.Con, *resolve.Symbol, *resolve.Symbol, *resolve.Symbol) {
	shape := &resolve.Symbol{Name: "Shape", Kind: resolve.KindType}
	circ, rect, tri := csym("Circle"), csym("Rect"), csym("Tri")
	shapeT := &types.Con{Name: "Shape"}
	td := &check.TypeDecl{Sp: sp, Sym: shape, Body: &check.SumType{Sp: sp,
		Variants: []check.Variant{
			{Sp: sp, Sym: circ, Fields: []check.TField{{Index: 0, Type: types.Float}}},
			{Sp: sp, Sym: rect, Fields: []check.TField{{Index: 0, Type: types.Float}}},
			{Sp: sp, Sym: tri, Fields: []check.TField{{Index: 0, Type: types.Float}}},
		}}}
	return td, shapeT, circ, rect, tri
}

// caseByCtor finds the DSwitch case testing constructor sym.
func caseByCtor(sw *core.DSwitch, sym *resolve.Symbol) *core.DCase {
	for i := range sw.Cases {
		if ct, ok := sw.Cases[i].Test.(*core.DCtorTest); ok && ct.Ctor == sym {
			return &sw.Cases[i]
		}
	}
	return nil
}

// TestMatchArmStateIsolatedPerCase is the F1 regression:
// `match (s, n) { (Circle, _) -> 0; (_, 5) -> 1; _ -> 2 }`. The
// survivor arms must be re-tested under every variant case — before
// the clone fix the Rect case consumed the shared arm state's todo
// list, so the Tri case dropped to the wildcard leaf without ever
// testing `(_, 5)`.
func TestMatchArmStateIsolatedPerCase(t *testing.T) {
	td, shapeT, circ, rect, tri := shape3()
	tupT := types.Tup(shapeT, types.Int)
	subj := vsym("s", tupT)
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, tupT), Arms: []check.MatchArm{
		mArm(&check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			&check.CtorPat{Base: b(shapeT), Ctor: circ}, wpat(types.Int)}}, intLit("0")),
		mArm(&check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			wpat(shapeT), &check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "5"}}}, intLit("1")),
		mArm(wpat(tupT), intLit("2")),
	}}
	out := mustLower(t, td, decl("f", []check.Param{param(vpat(subj, tupT))}, body, nil))
	m, ok := fnDeclOf(t, out, 1).Fn.Body.Tail.(*core.Match)
	if !ok {
		t.Fatalf("tail = %T, want Match", fnDeclOf(t, out, 1).Fn.Body.Tail)
	}
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok {
		t.Fatalf("tree = %T, want DSwitch", m.Tree)
	}
	// Circle's own arm wins — a bare leaf returning 0.
	if lt, ok := caseByCtor(sw, circ).Body.Tail.(*core.Lit); !ok || lt.Text != "0" {
		t.Errorf("Circle tail = %v, want lit 0", caseByCtor(sw, circ).Body.Tail)
	}
	// The (_, 5) arm must still be tested under every other variant.
	for _, sym := range []*resolve.Symbol{rect, tri} {
		c := caseByCtor(sw, sym)
		if c == nil {
			t.Fatalf("no case for %s", sym.Name)
		}
		inner, ok := c.Body.Tail.(*core.Match)
		if !ok {
			t.Fatalf("%s case tail = %T, want nested Match on the (_, 5) test",
				sym.Name, c.Body.Tail)
		}
		isw, ok := inner.Tree.(*core.DSwitch)
		if !ok || isw.Kind != core.SwitchValue {
			t.Fatalf("%s inner tree = %T", sym.Name, inner.Tree)
		}
		if len(isw.Cases) != 1 || isw.Cases[0].Test.(*core.DLitTest).Text != "5" {
			t.Fatalf("%s inner cases = %+v, want [lit 5]", sym.Name, isw.Cases)
		}
		if tail := isw.Cases[0].Body.Tail; tail.(*core.Lit).Text != "1" {
			t.Errorf("%s lit-5 tail = %v, want 1", sym.Name, tail)
		}
		if dl, ok := isw.Default.(*core.DLeaf); !ok {
			t.Errorf("%s inner default = %T, want DLeaf (the wildcard arm)",
				sym.Name, isw.Default)
		} else if lt := dl.Body.Tail.(*core.Lit); lt.Text != "2" {
			t.Errorf("%s wildcard tail = %s, want 2", sym.Name, lt.Text)
		}
	}
}

// TestDupArmBodyLetFreshSyms is the F2 regression — a match arm body
// is lowered at every leaf it reaches. `match s { Circle -> 0; _ ->
// { let v = 9; v } }` expands the wildcard over Rect and Tri, so the
// body's `let v` binds twice. Each leaf must get a fresh Core symbol
// (the leaf's substitution routes references); reusing the checker
// symbol trips the validator's bound-twice check.
func TestDupArmBodyLetFreshSyms(t *testing.T) {
	td, shapeT, circ, rect, tri := shape3()
	subj := vsym("s", shapeT)
	v := vsym("v", types.Int)
	wildBody := &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.Let{Base: b(types.Unit), Pat: vpat(v, types.Int), Value: intLit("9")},
		vr(v, types.Int),
	}}
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, shapeT), Arms: []check.MatchArm{
		mArm(&check.CtorPat{Base: b(shapeT), Ctor: circ}, intLit("0")),
		mArm(wpat(shapeT), wildBody),
	}}
	out := mustLower(t, td, decl("f", []check.Param{param(vpat(subj, shapeT))}, body, nil))
	m := fnDeclOf(t, out, 1).Fn.Body.Tail.(*core.Match)
	sw := m.Tree.(*core.DSwitch)
	for _, sym := range []*resolve.Symbol{rect, tri} {
		c := caseByCtor(sw, sym)
		if c == nil {
			t.Fatalf("no case for %s", sym.Name)
		}
		if len(c.Body.Lets) != 1 {
			t.Fatalf("%s lets = %d, want 1 (the v bind)", sym.Name, len(c.Body.Lets))
		}
		if _, ok := c.Body.Lets[0].X.(*core.Lit); !ok {
			t.Fatalf("%s let X = %T, want lit 9", sym.Name, c.Body.Lets[0].X)
		}
		tail, ok := c.Body.Tail.(*core.Var)
		if !ok || tail.Sym != c.Body.Lets[0].Name.Sym {
			t.Errorf("%s tail = %v, want a Var of the let binder", sym.Name, c.Body.Tail)
		}
	}
	// The two leaves bind different Core symbols.
	if caseByCtor(sw, rect).Body.Lets[0].Name.Sym == caseByCtor(sw, tri).Body.Lets[0].Name.Sym {
		t.Errorf("both leaves share one binder symbol — the validator flags bound-twice")
	}
}

// TestOrPatArmBodyLet is the or-pattern instance of F2:
// `match n { (0 | 1) -> { let v = 9; v }; _ -> 0 }` — the or-pattern
// arm reaches two literal-case leaves; the same bound-twice hazard
// applies.
func TestOrPatArmBodyLet(t *testing.T) {
	subj := vsym("n", types.Int)
	v := vsym("v", types.Int)
	orBody := &check.Block{Base: b(types.Int), Stmts: []check.Expr{
		&check.Let{Base: b(types.Unit), Pat: vpat(v, types.Int), Value: intLit("9")},
		vr(v, types.Int),
	}}
	or := &check.OrPat{Base: b(types.Int), Alts: []check.Pat{
		&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "0"},
		&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "1"},
	}}
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, types.Int), Arms: []check.MatchArm{
		mArm(or, orBody),
		mArm(wpat(types.Int), intLit("7")),
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(subj, types.Int))}, body, nil))
	m := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Match)
	sw := m.Tree.(*core.DSwitch)
	var binders []*resolve.Symbol
	for i := range sw.Cases {
		c := &sw.Cases[i]
		if len(c.Body.Lets) != 1 {
			t.Fatalf("case %d lets = %d, want 1", i, len(c.Body.Lets))
		}
		binders = append(binders, c.Body.Lets[0].Name.Sym)
		if tail, ok := c.Body.Tail.(*core.Var); !ok || tail.Sym != c.Body.Lets[0].Name.Sym {
			t.Errorf("case %d tail = %v, want a Var of the let binder", i, c.Body.Tail)
		}
	}
	if len(binders) != 2 || binders[0] == binders[1] {
		t.Errorf("or-pattern leaves must bind distinct symbols, got %v", binders)
	}
}

// TestListRestOnlyNonLeading is the F3 regression: `[..rest]` never
// fails — it survives both the nil and the cons outcome, binding the
// whole subject. Before the fix a non-leading `[..rest]` was dropped
// from the nil case and statesForCons indexed Elems[0] of the empty
// element list.
func TestListRestOnlyNonLeading(t *testing.T) {
	listT := types.List(types.Int)
	xs, h, tl, rest := vsym("xs", listT), vsym("h", types.Int), vsym("t", listT), vsym("rest", listT)
	body := &check.Match{Base: b(types.Int), Subj: vr(xs, listT), Arms: []check.MatchArm{
		mArm(&check.ListPat{Base: b(listT),
			Elems: []check.Pat{vpat(h, types.Int)}, Rest: vpat(tl, listT)}, vr(h, types.Int)),
		mArm(&check.ListPat{Base: b(listT), Rest: vpat(rest, listT)}, intLit("0")),
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(xs, listT))}, body, nil))
	m := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Match)
	sw, ok := m.Tree.(*core.DSwitch)
	if !ok || sw.Kind != core.SwitchList {
		t.Fatalf("tree = %T", m.Tree)
	}
	var nilC, consC *core.DCase
	for i := range sw.Cases {
		if sw.Cases[i].Test.(*core.DListTest).Cons {
			consC = &sw.Cases[i]
		} else {
			nilC = &sw.Cases[i]
		}
	}
	if nilC == nil || consC == nil {
		t.Fatalf("want both nil and cons cases, got %+v", sw.Cases)
	}
	// Under nil, [..rest] binds the whole subject.
	var restBound bool
	for _, lt := range nilC.Body.Lets {
		if x, ok := lt.X.(*core.Var); ok && x.Sym == xs {
			restBound = true
		}
	}
	if !restBound {
		t.Errorf("nil case has no `rest = xs` bind: %+v", nilC.Body.Lets)
	}
	// Under cons the first arm wins; the second arm's rest bind still
	// survives without indexing an empty Elems.
	if len(consC.Binds) != 2 {
		t.Fatalf("cons binds = %d, want head+tail", len(consC.Binds))
	}
}

// TestListRestOnlyAlone: `[..rest]` as the only arm — the structural
// path binds the whole subject directly.
func TestListRestOnlyAlone(t *testing.T) {
	listT := types.List(types.Int)
	xs, rest := vsym("xs", listT), vsym("rest", listT)
	body := &check.Match{Base: b(listT), Subj: vr(xs, listT), Arms: []check.MatchArm{
		mArm(&check.ListPat{Base: b(listT), Rest: vpat(rest, listT)}, vr(rest, listT)),
	}}
	out := mustLower(t, decl("f", []check.Param{param(vpat(xs, listT))}, body, nil))
	fn := fnDeclOf(t, out, 0)
	// The match collapses to `rest = xs` — one let and the var.
	if len(fn.Fn.Body.Lets) != 1 {
		t.Fatalf("lets = %d, want 1 (rest = xs)", len(fn.Fn.Body.Lets))
	}
	if x, ok := fn.Fn.Body.Lets[0].X.(*core.Var); !ok || x.Sym != xs {
		t.Fatalf("let0 X = %T, want the xs var", fn.Fn.Body.Lets[0].X)
	}
	tail, ok := fn.Fn.Body.Tail.(*core.Var)
	if !ok || tail.Sym != fn.Fn.Body.Lets[0].Name.Sym {
		t.Errorf("tail = %v, want the rest binder", fn.Fn.Body.Tail)
	}
}

// TestTryVariadicSpread is the F4 regression: `try` on a variadic Go
// call must set CallGo.Spread from the written `..` marker — a plain
// variadic call is not a spread.
func TestTryVariadicSpread(t *testing.T) {
	g := gsym("g")
	resT := types.Result(types.Int, types.GoError)
	gsig := types.Fn(resT, types.P(types.Int), types.P(types.List(types.Int)))
	xs := vsym("xs", types.List(types.Int))
	okSym := csym("Ok")
	mkfn := func(value check.Expr) *check.FnDecl {
		v := vsym("v", types.Int)
		return decl("f", []check.Param{param(vpat(xs, types.List(types.Int)))},
			&check.Block{Base: b(resT), Stmts: []check.Expr{
				&check.Let{Base: b(types.Unit), Pat: vpat(v, types.Int), Value: value},
				app(vr(okSym, types.Fn(resT, types.P(types.Int))), []check.Expr{vr(v, types.Int)},
					check.CallInfo{Kind: check.CallDirect, Callee: okSym, Arity: 1, Applied: 1, HoleIndex: -1}, resT),
			}}, types.Fn(resT, types.P(types.List(types.Int))))
	}
	// Plain varargs g(1, 2): Args>Arity-1 but no `..` → Spread false.
	plain := &check.Try{Base: b(types.Int), X: app(vr(g, gsig),
		[]check.Expr{intLit("1"), intLit("2")},
		check.CallInfo{Kind: check.CallGo, Callee: g, Arity: 1, Applied: 2,
			Variadic: true, HoleIndex: -1}, resT)}
	out := mustLower(t, mkfn(plain))
	ec, ok := fnDeclOf(t, out, 0).Fn.Body.Lets[0].X.(*core.EscapeCall)
	if !ok {
		t.Fatalf("let0 = %T, want EscapeCall", fnDeclOf(t, out, 0).Fn.Body.Lets[0].X)
	}
	if ec.Call.Spread {
		t.Errorf("plain variadic call got Spread=true — f(1, 2) is not f(xs...)")
	}
	// Spread g(..xs): Spread true.
	spread := &check.Try{Base: b(types.Int), X: app(vr(g, gsig),
		[]check.Expr{&check.Spread{Base: b(types.List(types.Int)), X: vr(xs, types.List(types.Int))}},
		check.CallInfo{Kind: check.CallGo, Callee: g, Arity: 1, Applied: 1,
			Variadic: true, HoleIndex: -1}, resT)}
	out2 := mustLower(t, mkfn(spread))
	ec2, ok := fnDeclOf(t, out2, 0).Fn.Body.Lets[0].X.(*core.EscapeCall)
	if !ok {
		t.Fatalf("let0 = %T, want EscapeCall", fnDeclOf(t, out2, 0).Fn.Body.Lets[0].X)
	}
	if !ec2.Call.Spread {
		t.Errorf("g(..xs) got Spread=false — the written `..` must survive")
	}
}

// TestOverApplyPartialRemainder is the F5 regression: `mk x y` where
// mk's declared arity is 1 but its flat type arity is 3 — the
// one-argument remainder under-applies the returned `Int -> Int ->
// Int` and must become a Closure, not a partial CallFunc the
// validator rejects.
func TestOverApplyPartialRemainder(t *testing.T) {
	mk := fsym("mk")
	fsig := types.Fn(types.Int, types.P(types.Int), types.P(types.Int), types.P(types.Int))
	retFn := types.Fn(types.Int, types.P(types.Int)) // Int -> Int
	x, y := vsym("x", types.Int), vsym("y", types.Int)
	call := app(vr(mk, fsig), []check.Expr{vr(x, types.Int), vr(y, types.Int)},
		check.CallInfo{Kind: check.CallDirect, Callee: mk, Arity: 1, Applied: 2, HoleIndex: -1}, retFn)
	out := mustLower(t, decl("f", []check.Param{param(vpat(x, types.Int)), param(vpat(y, types.Int))},
		call, types.Fn(retFn, types.P(types.Int), types.P(types.Int))))
	fn := fnDeclOf(t, out, 0)
	// let tmp = mk(x) saturated; tail = \p -> tmp(y, p).
	if len(fn.Fn.Body.Lets) != 1 {
		t.Fatalf("lets = %d, want 1 (saturated prefix)", len(fn.Fn.Body.Lets))
	}
	if _, ok := fn.Fn.Body.Lets[0].X.(*core.CallDirect); !ok {
		t.Fatalf("let0 = %T, want CallDirect", fn.Fn.Body.Lets[0].X)
	}
	cl, ok := fn.Fn.Body.Tail.(*core.Closure)
	if !ok {
		t.Fatalf("tail = %T, want Closure for the partial remainder", fn.Fn.Body.Tail)
	}
	if len(cl.Fn.Params) != 1 {
		t.Fatalf("closure params = %d, want 1", len(cl.Fn.Params))
	}
	cf, ok := cl.Fn.Body.Tail.(*core.CallFunc)
	if !ok {
		t.Fatalf("closure tail = %T, want CallFunc", cl.Fn.Body.Tail)
	}
	if len(cf.Args) != 2 ||
		cf.Args[0].(*core.Var).Sym != y ||
		cf.Args[1].(*core.Var).Sym != cl.Fn.Params[0].Sym {
		t.Errorf("closure call args = %+v, want [y, param]", cf.Args)
	}
}

// TestCuteHoleLabeledReorder is the F6 regression: `g(~b=1, _)` —
// HoleIndex counts *written* argument order. Filling the hole after
// the declaration-order reorder put the closure param at index 1 and
// left index 0 a nil atom.
func TestCuteHoleLabeledReorder(t *testing.T) {
	g := gsym("g")
	remT := types.Fn(types.Int, types.P(types.Int)) // param0 unfilled
	call := &check.Apply{Base: b(remT),
		Fn: vr(g, types.Fn(types.Int, types.P(types.Int), types.P(types.Int))),
		Args: []check.Arg{
			{Label: "b", LabelSp: sp, Value: intLit("1")},
			{Value: &check.Hole{Base: b(types.Int)}},
		},
		Call: check.CallInfo{Kind: check.CallGo, Callee: g, Arity: 2, Applied: 2,
			ArgToParam: []int{1, 0}, HoleIndex: 1}}
	out := mustLower(t, decl("f", nil, call, types.Fn(remT)))
	cl, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Closure)
	if !ok {
		t.Fatalf("tail = %T, want Closure", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
	cg, ok := cl.Fn.Body.Tail.(*core.CallGo)
	if !ok {
		t.Fatalf("closure tail = %T, want CallGo", cl.Fn.Body.Tail)
	}
	if len(cg.Args) != 2 {
		t.Fatalf("call args = %d, want 2", len(cg.Args))
	}
	if v, ok := cg.Args[0].(*core.Var); !ok || v.Sym != cl.Fn.Params[0].Sym {
		t.Errorf("arg0 (param0, the hole) = %v, want the closure param", cg.Args[0])
	}
	if lt, ok := cg.Args[1].(*core.Lit); !ok || lt.Text != "1" {
		t.Errorf("arg1 (param1, the labeled arg) = %v, want lit 1", cg.Args[1])
	}
}

// TestOrPatDestructureOrder is the F7 regression:
// `let (A x y | B y x) = v` — the leaf packs the bound tuple in the
// declared binder order (x, y), not the matched path's order, or the
// outer index projections swap the values under B.
func TestOrPatDestructureOrder(t *testing.T) {
	ab := &resolve.Symbol{Name: "AB", Kind: resolve.KindType}
	aCtor, bCtor := csym("A"), csym("B")
	abT := &types.Con{Name: "AB"}
	td := &check.TypeDecl{Sp: sp, Sym: ab, Body: &check.SumType{Sp: sp,
		Variants: []check.Variant{
			{Sp: sp, Sym: aCtor, Fields: []check.TField{{Index: 0, Type: types.Int}, {Index: 1, Type: types.Int}}},
			{Sp: sp, Sym: bCtor, Fields: []check.TField{{Index: 0, Type: types.Int}, {Index: 1, Type: types.Int}}},
		}}}
	// Shared checker symbols — the resolver merges or-alts by name.
	xSym := &resolve.Symbol{Name: "x", Kind: resolve.KindVar}
	ySym := &resolve.Symbol{Name: "y", Kind: resolve.KindVar}
	pat := &check.OrPat{Base: b(abT), Alts: []check.Pat{
		&check.CtorPat{Base: b(abT), Ctor: aCtor, Args: []check.CtorArg{
			{Pat: vpat(xSym, types.Int)}, {Pat: vpat(ySym, types.Int)}}},
		&check.CtorPat{Base: b(abT), Ctor: bCtor, Args: []check.CtorArg{
			{Pat: vpat(ySym, types.Int)}, {Pat: vpat(xSym, types.Int)}}},
	}}
	vSym := vsym("v", abT)
	vd := &check.LetDecl{Sp: sp, Pat: vpat(vSym, abT),
		Value: app(vr(aCtor, types.Fn(abT, types.P(types.Int), types.P(types.Int))),
			[]check.Expr{intLit("1"), intLit("2")},
			check.CallInfo{Kind: check.CallDirect, Callee: aCtor, Arity: 2, Applied: 2, HoleIndex: -1}, abT)}
	out := mustLower(t, td, vd, &check.LetDecl{Sp: sp, Pat: pat, Value: vr(vSym, abT)})
	// decls: TypeDecl, v, hidden tmp, bound tuple, x, y.
	if len(out.Decls) != 6 {
		t.Fatalf("decls = %d, want 6", len(out.Decls))
	}
	bound := out.Decls[3].(*core.VarDecl)
	m := bound.Body.Tail.(*core.Match)
	sw := m.Tree.(*core.DSwitch)
	bCase := caseByCtor(sw, bCtor)
	if bCase == nil {
		t.Fatal("no B case")
	}
	// Under B the path earns y from payload 0 and x from payload 1 —
	// but the tuple must pack declared order (x, y) = (payload1, payload0).
	indexOf := func(s *resolve.Symbol) int {
		for _, lt := range bCase.Body.Lets {
			if lt.Name.Sym == s {
				if fg, ok := lt.X.(*core.FieldGet); ok {
					return fg.Index
				}
			}
		}
		return -1
	}
	tup, ok := bCase.Body.Tail.(*core.TupleNew)
	if !ok {
		t.Fatalf("B case tail = %T, want TupleNew", bCase.Body.Tail)
	}
	if len(tup.Elems) != 2 {
		t.Fatalf("tuple elems = %d, want 2", len(tup.Elems))
	}
	e0, e1 := tup.Elems[0].(*core.Var), tup.Elems[1].(*core.Var)
	if got := indexOf(e0.Sym); got != 1 {
		t.Errorf("tuple elem0 (x slot) binds payload index %d, want 1 under B", got)
	}
	if got := indexOf(e1.Sym); got != 0 {
		t.Errorf("tuple elem1 (y slot) binds payload index %d, want 0 under B", got)
	}
	// Outer projections follow declared order: x<-0, y<-1.
	xd := out.Decls[4].(*core.VarDecl)
	yd := out.Decls[5].(*core.VarDecl)
	if xd.Sym != xSym || xd.Body.Tail.(*core.FieldGet).Index != 0 {
		t.Errorf("x decl = %+v", xd)
	}
	if yd.Sym != ySym || yd.Body.Tail.(*core.FieldGet).Index != 1 {
		t.Errorf("y decl = %+v", yd)
	}
}

// TestComposeBackOrder is the F8 regression: `mkG 1 << mkF 2` is
// `fn p -> mkG' (mkF' p)` — f applies first inside the closure, but
// the operands still evaluate left to right: the mkG call, then the
// mkF call.
func TestComposeBackOrder(t *testing.T) {
	is := types.Fn(types.Int, types.P(types.Int)) // Int -> Int
	retT := is                                    // produced function
	gsig := types.Fn(retT, types.P(types.Int))    // Int -> (Int -> Int)
	mkG, mkF := fsym("mk_g"), fsym("mk_f")
	ci := func(callee *resolve.Symbol) check.CallInfo {
		return check.CallInfo{Kind: check.CallDirect, Callee: callee, Arity: 1, Applied: 1, HoleIndex: -1}
	}
	xCall := app(vr(mkG, gsig), []check.Expr{intLit("1")}, ci(mkG), retT)
	yCall := app(vr(mkF, gsig), []check.Expr{intLit("2")}, ci(mkF), retT)
	e := &check.Binary{Base: b(is), Op: syntax.BinComposeBack, X: xCall, Y: yCall}
	out := mustLower(t, decl("f", nil, e, types.Fn(is)))
	fn := fnDeclOf(t, out, 0)
	if len(fn.Fn.Body.Lets) != 2 {
		t.Fatalf("lets = %d, want 2 (both operand calls)", len(fn.Fn.Body.Lets))
	}
	gLet, fLet := fn.Fn.Body.Lets[0], fn.Fn.Body.Lets[1]
	if cd, ok := gLet.X.(*core.CallDirect); !ok || cd.Callee != mkG {
		t.Fatalf("let0 = %T, want the mkG call (left operand first)", gLet.X)
	}
	if cd, ok := fLet.X.(*core.CallDirect); !ok || cd.Callee != mkF {
		t.Fatalf("let1 = %T, want the mkF call", fLet.X)
	}
	cl, ok := fn.Fn.Body.Tail.(*core.Closure)
	if !ok {
		t.Fatalf("tail = %T, want Closure", fn.Fn.Body.Tail)
	}
	// Inside: f applies first — CallFunc on the mkF temp — then g.
	if len(cl.Fn.Body.Lets) != 1 {
		t.Fatalf("closure lets = %d, want 1 (the f application)", len(cl.Fn.Body.Lets))
	}
	fApp, ok := cl.Fn.Body.Lets[0].X.(*core.CallFunc)
	if !ok || fApp.Fn.(*core.Var).Sym != fLet.Name.Sym {
		t.Fatalf("inner first app = %+v, want mkF' applied", cl.Fn.Body.Lets[0].X)
	}
	gApp, ok := cl.Fn.Body.Tail.(*core.CallFunc)
	if !ok || gApp.Fn.(*core.Var).Sym != gLet.Name.Sym {
		t.Fatalf("inner tail = %+v, want mkG' applied to the mid", cl.Fn.Body.Tail)
	}
	if len(gApp.Args) != 1 || gApp.Args[0].(*core.Var).Sym != cl.Fn.Body.Lets[0].Name.Sym {
		t.Errorf("g application args = %+v, want [mid]", gApp.Args)
	}
}

// TestCtorSwitchNonConSubject is the F9 regression: a constructor arm
// over a non-nominal subject cannot enumerate variants — a diagnostic
// now, previously a nil-dereference panic in variantsOf.
func TestCtorSwitchNonConSubject(t *testing.T) {
	tupT := types.Tup(types.Int, types.Int)
	subj := vsym("p", tupT)
	body := &check.Match{Base: b(types.Int), Subj: vr(subj, tupT), Arms: []check.MatchArm{
		mArm(&check.CtorPat{Base: b(tupT), Ctor: csym("Nope")}, intLit("0")),
		mArm(wpat(tupT), intLit("1")),
	}}
	_, diags := lower1(t, decl("f", []check.Param{param(vpat(subj, tupT))}, body, nil))
	if len(diags) == 0 {
		t.Fatal("expected a variant-enumeration diagnostic")
	}
}

// TestForeignVarDiagnostic is the F10 regression: a KindVar reference
// resolved to another package cannot lower to a bare core.Var — the
// checker would never emit one, so it is a contract-gap diagnostic.
func TestForeignVarDiagnostic(t *testing.T) {
	foreign := &resolve.Symbol{Name: "y", Kind: resolve.KindVar, Package: "q"}
	x := vsym("x", types.Int)
	d := &check.LetDecl{Sp: sp, Pat: vpat(x, types.Int), Value: vr(foreign, types.Int)}
	_, diags := lower1(t, d)
	var found bool
	for _, dg := range diags {
		if strings.Contains(dg.Message, "another package") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a cross-package diagnostic, got %v", diags)
	}
}

// TestRecordPatUnknownFieldIndex is the F11 regression: a FieldPat
// whose Index is -1 and whose symbol resolves nowhere gets the
// fieldIndex fallback and a diagnostic — never a silent
// FieldGet{Index: -1} alone.
func TestRecordPatUnknownFieldIndex(t *testing.T) {
	recT := &types.Con{Name: "Rec"}
	fSym := &resolve.Symbol{Name: "zz", Kind: resolve.KindField}
	v := vsym("v", types.Int)
	r := vsym("r", recT)
	pat := &check.RecordPat{Base: b(recT), Fields: []check.FieldPat{
		{Sp: sp, Name: "zz", Sym: fSym, Index: -1, Pat: vpat(v, types.Int)},
	}}
	d := &check.LetDecl{Sp: sp, Pat: pat, Value: vr(r, recT)}
	_, diags := lower1(t, d)
	if len(diags) == 0 {
		t.Fatal("expected a field-index diagnostic")
	}
	for _, dg := range diags {
		if strings.Contains(dg.Message, "no known index") {
			return
		}
	}
	t.Fatalf("expected a no-known-index diagnostic, got %v", diags)
}

// TestSelectNilBind is the F12 regression: a receive arm whose Bind
// is nil (a checker shape the typed AST should not produce) reports a
// diagnostic instead of dereferencing nil.
func TestSelectNilBind(t *testing.T) {
	chT := types.Chan(types.Int)
	ch := vsym("ch", chT)
	sel := &check.SelectExpr{Base: b(types.Int), Arms: []check.SelectArm{
		&check.RecvArm{Sp: sp, Bind: nil, Chan: vr(ch, chT), Body: intLit("1")},
		&check.DefaultArm{Sp: sp, Body: intLit("0")},
	}}
	_, diags := lower1(t, decl("f", []check.Param{param(vpat(ch, chT))}, sel,
		types.Fn(types.Int, types.P(chT))))
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for the nil bind")
	}
}

var _ = fmt.Sprintf // keep fmt imported if unused in a build tag path
var _ = strings.TrimSpace

// --- builtin calls / test gating / structural equality ---

// TestBuiltinCallDirect: `todo ()` and `not x` — a statically known
// builtin callee (CallDirect, KindBuiltin) lowers to CallBuiltin
// without the function position ever touching the value path. The
// regression: applyCall used to lower e.Fn first, so a builtin in
// function position reported "builtin X used as a value" (E0901).
func TestBuiltinCallDirect(t *testing.T) {
	todo := &resolve.Symbol{Name: "todo", Kind: resolve.KindBuiltin}
	call := app(vr(todo, types.Fn(types.Int)), nil,
		check.CallInfo{Kind: check.CallDirect, Callee: todo, Arity: 0, Applied: 0, HoleIndex: -1},
		types.Int)
	out := mustLower(t, decl("f", nil, call, types.Fn(types.Int)))
	cb, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "todo" {
		t.Fatalf("tail = %T, want CallBuiltin{todo}", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
}

// TestBuiltinCallNotDirect: same dispatch through `not x` — one
// argument.
func TestBuiltinCallNotDirect(t *testing.T) {
	not := &resolve.Symbol{Name: "not", Kind: resolve.KindBuiltin}
	x := vsym("x", types.Bool)
	call := app(vr(not, types.Fn(types.Bool, types.P(types.Bool))), []check.Expr{vr(x, types.Bool)},
		check.CallInfo{Kind: check.CallDirect, Callee: not, Arity: 1, Applied: 1, HoleIndex: -1},
		types.Bool)
	out := mustLower(t, decl("f", []check.Param{param(vpat(x, types.Bool))}, call,
		types.Fn(types.Bool, types.P(types.Bool))))
	cb, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "not" || len(cb.Args) != 1 {
		t.Fatalf("tail = %#v, want CallBuiltin{not, 1 arg}", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
}

// TestBuiltinPartialCall: an under-applied builtin still lowers
// through the closure path — `assert_eq 1` inside a test block is a
// Closure whose body is the saturated CallBuiltin.
func TestBuiltinPartialCall(t *testing.T) {
	assertEq := &resolve.Symbol{Name: "assert_eq", Kind: resolve.KindBuiltin}
	call := app(vr(assertEq, types.Fn(types.Unit, types.P(types.Int), types.P(types.Int))),
		[]check.Expr{intLit("1")},
		check.CallInfo{Kind: check.CallPartial, Callee: assertEq, Arity: 2, Applied: 1, Unapplied: 1, HoleIndex: -1},
		types.Fn(types.Unit, types.P(types.Int)))
	body := &check.Block{Base: b(types.Unit), Stmts: []check.Expr{call, unitLit()}}
	td := &check.TestDecl{Sp: sp, Name: "t", NameSp: sp, Body: body}
	f := &check.File{Sp: sp, Package: "p", PkgSp: sp, Decls: []check.Decl{td}}
	out, diags := File(f, &Env{Tests: true})
	for _, d := range diags {
		t.Fatalf("unexpected diagnostic: %v", d)
	}
	tdc, ok := out.Decls[0].(*core.TestDecl)
	if !ok {
		t.Fatalf("decl 0 = %T, want TestDecl", out.Decls[0])
	}
	cl, ok := tdc.Fn.Body.Lets[0].X.(*core.Closure)
	if !ok {
		t.Fatalf("let0 = %T, want Closure over the partial call", tdc.Fn.Body.Lets[0].X)
	}
	cb, ok := cl.Fn.Body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "assert_eq" || len(cb.Args) != 2 {
		t.Fatalf("closure tail = %#v, want saturated CallBuiltin{assert_eq}", cl.Fn.Body.Tail)
	}
}

// TestAssertOutsideTest: `assert` in ordinary code must not emit
// rt.Assert(t, ...) — `t` exists only inside generated test
// functions. Until check settles the semantic rule
// (docs/problems/2026-11-02-assert-outside-test.md) lowering reports
// the shape instead of producing uncompilable Go.
func TestAssertOutsideTest(t *testing.T) {
	asrt := &resolve.Symbol{Name: "assert", Kind: resolve.KindBuiltin}
	x := vsym("x", types.Bool)
	call := app(vr(asrt, types.Fn(types.Unit, types.P(types.Bool))), []check.Expr{vr(x, types.Bool)},
		check.CallInfo{Kind: check.CallDirect, Callee: asrt, Arity: 1, Applied: 1, HoleIndex: -1},
		types.Unit)
	_, diags := lower1(t, decl("f", []check.Param{param(vpat(x, types.Bool))}, call,
		types.Fn(types.Unit, types.P(types.Bool))))
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic for assert outside a test block")
	}
	if !strings.Contains(fmt.Sprint(diags[0]), "test block") {
		t.Fatalf("diagnostic = %v, want a test-block message", diags[0])
	}
}

// TestAssertInTest: inside a `test` declaration — the one place the
// generated `t` exists — assert_eq lowers cleanly.
func TestAssertInTest(t *testing.T) {
	assertEq := &resolve.Symbol{Name: "assert_eq", Kind: resolve.KindBuiltin}
	call := app(vr(assertEq, types.Fn(types.Unit, types.P(types.Int), types.P(types.Int))),
		[]check.Expr{intLit("1"), intLit("1")},
		check.CallInfo{Kind: check.CallDirect, Callee: assertEq, Arity: 2, Applied: 2, HoleIndex: -1},
		types.Unit)
	body := &check.Block{Base: b(types.Unit), Stmts: []check.Expr{call, unitLit()}}
	td := &check.TestDecl{Sp: sp, Name: "t", NameSp: sp, Body: body}
	f := &check.File{Sp: sp, Package: "p", PkgSp: sp, Decls: []check.Decl{td}}
	out, diags := File(f, &Env{Tests: true})
	for _, d := range diags {
		t.Fatalf("unexpected diagnostic: %v", d)
	}
	tdc, ok := out.Decls[0].(*core.TestDecl)
	if !ok {
		t.Fatalf("decl 0 = %T, want TestDecl", out.Decls[0])
	}
	cb, ok := tdc.Fn.Body.Lets[0].X.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "assert_eq" {
		t.Fatalf("let0 = %#v, want CallBuiltin{assert_eq}", tdc.Fn.Body.Lets[0].X)
	}
}

// TestTestDeclGating: spec 11.6 — gom build checks test code but
// emits none of it. A `test` declaration lowers only under
// Env.Tests (gom test mode).
func TestTestDeclGating(t *testing.T) {
	body := &check.Block{Base: b(types.Unit), Stmts: []check.Expr{unitLit()}}
	td := &check.TestDecl{Sp: sp, Name: "t", NameSp: sp, Body: body}
	mk := func() *check.File {
		return &check.File{Sp: sp, Package: "p", PkgSp: sp, Decls: []check.Decl{td}}
	}
	out, diags := File(mk(), nil)
	for _, d := range diags {
		t.Fatalf("unexpected diagnostic: %v", d)
	}
	if len(out.Decls) != 0 {
		t.Fatalf("build mode kept %d decls, want the test dropped", len(out.Decls))
	}
	out, diags = File(mk(), &Env{Tests: true})
	for _, d := range diags {
		t.Fatalf("unexpected diagnostic: %v", d)
	}
	if len(out.Decls) != 1 {
		t.Fatalf("test mode emitted %d decls, want 1", len(out.Decls))
	}
	if _, ok := out.Decls[0].(*core.TestDecl); !ok {
		t.Fatalf("decl 0 = %T, want TestDecl", out.Decls[0])
	}
}

// TestTestOnlyDeclGating: @test fns and lets are test-only code —
// dropped under gom build like the test decls they serve.
func TestTestOnlyDeclGating(t *testing.T) {
	tsym := &resolve.Symbol{Name: "helper", Kind: resolve.KindFunc, TestOnly: true}
	tdecl := &check.FnDecl{Sp: sp, Sym: tsym, Sig: types.Fn(types.Int),
		Body: intLit("1")}
	lsym := &resolve.Symbol{Name: "fixture", Kind: resolve.KindVar, TestOnly: true}
	ldecl := &check.LetDecl{Sp: sp, Pat: vpat(lsym, types.Int), Value: intLit("2")}
	mk := func() *check.File {
		return &check.File{Sp: sp, Package: "p", PkgSp: sp, Decls: []check.Decl{tdecl, ldecl}}
	}
	out, diags := File(mk(), nil)
	for _, d := range diags {
		t.Fatalf("unexpected diagnostic: %v", d)
	}
	if len(out.Decls) != 0 {
		t.Fatalf("build mode kept %d decls, want 0", len(out.Decls))
	}
	out, diags = File(mk(), &Env{Tests: true})
	for _, d := range diags {
		t.Fatalf("unexpected diagnostic: %v", d)
	}
	if len(out.Decls) != 2 {
		t.Fatalf("test mode emitted %d decls, want 2", len(out.Decls))
	}
}

// TestEqLowering: `==` on scalars stays a native Binary; on List it
// becomes the `eq` builtin (rt.Equal), and `!=` the `ne` builtin.
func TestEqLowering(t *testing.T) {
	xs := vsym("x", types.Int)
	ys := vsym("y", types.Int)
	intEq := &check.Binary{Base: b(types.Bool), Op: syntax.BinEq,
		X: vr(xs, types.Int), Y: vr(ys, types.Int)}
	out := mustLower(t, decl("f",
		[]check.Param{param(vpat(xs, types.Int)), param(vpat(ys, types.Int))},
		intEq, nil))
	if _, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Binary); !ok {
		t.Fatalf("Int == lowered to %T, want native core.Binary", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}

	lt := types.List(types.Int)
	lx := vsym("x", lt)
	ly := vsym("y", lt)
	listEq := &check.Binary{Base: b(types.Bool), Op: syntax.BinEq,
		X: vr(lx, lt), Y: vr(ly, lt)}
	out = mustLower(t, decl("f",
		[]check.Param{param(vpat(lx, lt)), param(vpat(ly, lt))},
		listEq, nil))
	cb, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "eq" || len(cb.Args) != 2 {
		t.Fatalf("List == lowered to %#v, want CallBuiltin{eq}", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}

	listNe := &check.Binary{Base: b(types.Bool), Op: syntax.BinNe,
		X: vr(lx, lt), Y: vr(ly, lt)}
	out = mustLower(t, decl("f",
		[]check.Param{param(vpat(lx, lt)), param(vpat(ly, lt))},
		listNe, nil))
	cb, ok = fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "ne" {
		t.Fatalf("List != lowered to %#v, want CallBuiltin{ne}", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}

	// A tuple of scalars is a comparable Go struct — native ==.
	tt := &types.Tuple{Elems: []types.Type{types.Int, types.String}}
	tx := vsym("x", tt)
	ty := vsym("y", tt)
	tupEq := &check.Binary{Base: b(types.Bool), Op: syntax.BinEq,
		X: vr(tx, tt), Y: vr(ty, tt)}
	out = mustLower(t, decl("f",
		[]check.Param{param(vpat(tx, tt)), param(vpat(ty, tt))},
		tupEq, nil))
	if _, ok := fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.Binary); !ok {
		t.Fatalf("scalar-tuple == lowered to %T, want core.Binary", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}

	// A tuple containing a List cannot use Go's ==.
	tt2 := &types.Tuple{Elems: []types.Type{types.Int, lt}}
	tx2 := vsym("x", tt2)
	ty2 := vsym("y", tt2)
	tupEq2 := &check.Binary{Base: b(types.Bool), Op: syntax.BinEq,
		X: vr(tx2, tt2), Y: vr(ty2, tt2)}
	out = mustLower(t, decl("f",
		[]check.Param{param(vpat(tx2, tt2)), param(vpat(ty2, tt2))},
		tupEq2, nil))
	cb, ok = fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "eq" {
		t.Fatalf("list-tuple == lowered to %#v, want CallBuiltin{eq}", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}

	// A nominal record type goes through rt.Equal too.
	rt_ := &types.Con{Name: "Point"}
	rx := vsym("x", rt_)
	ry := vsym("y", rt_)
	recEq := &check.Binary{Base: b(types.Bool), Op: syntax.BinEq,
		X: vr(rx, rt_), Y: vr(ry, rt_)}
	out = mustLower(t, decl("f",
		[]check.Param{param(vpat(rx, rt_)), param(vpat(ry, rt_))},
		recEq, nil))
	cb, ok = fnDeclOf(t, out, 0).Fn.Body.Tail.(*core.CallBuiltin)
	if !ok || cb.Sym.Name != "eq" {
		t.Fatalf("record == lowered to %#v, want CallBuiltin{eq}", fnDeclOf(t, out, 0).Fn.Body.Tail)
	}
}

// --- pipe Go-callee conventions --------------------------------------

// lowerWith is lower1 with an Env — used where the Go signature
// oracle decides the call shape (variadicness is an Env fact).
func lowerWith(t *testing.T, env *Env, decls ...check.Decl) (*core.File, []diag.Diagnostic) {
	t.Helper()
	f := &check.File{Sp: sp, Package: "p", PkgSp: sp, Decls: decls}
	out, diags := File(f, env)
	if out == nil {
		t.Fatal("File returned nil")
	}
	return out, diags
}

// findCallGo digs the single CallGo out of a function body — into
// the Let bindings first, then the tail.
func findCallGo(t *testing.T, fn *core.FuncDecl) *core.CallGo {
	t.Helper()
	for _, l := range fn.Fn.Body.Lets {
		if cg, ok := l.X.(*core.CallGo); ok {
			return cg
		}
	}
	if cg, ok := fn.Fn.Body.Tail.(*core.CallGo); ok {
		return cg
	}
	t.Fatalf("no CallGo in body")
	return nil
}

// `x |> Println` for a bare variadic-arity-1 Go callee lowers as the
// one-element CallGo — the checker types the piped value as an
// element, so the value is not (and must not be) re-sliced at
// lowering. Spread stays false: the pipe is not `..` syntax.
func TestPipeFwdGoVariadicElem(t *testing.T) {
	g := gsym("Println")
	sig := types.Fn(types.Int, types.P(types.GoSlice(types.GoAny)))
	x := vsym("x", types.String)
	pipe := &check.Binary{Base: b(types.Int), Op: syntax.BinPipeFwd,
		X: vr(x, types.String), Y: vr(g, sig)}
	env := &Env{GoSig: func(sym *resolve.Symbol) (GoSig, bool) {
		return GoSig{Arity: 1, Variadic: true, Results: core.GoResultsOne}, true
	}}
	out, diags := lowerWith(t, env,
		decl("f", []check.Param{param(vpat(x, types.String))}, pipe,
			types.Fn(types.Int, types.P(types.String))))
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %v", d)
	}
	cg := findCallGo(t, fnDeclOf(t, out, 0))
	if cg.Spread {
		t.Error("piped call is not a spread call")
	}
	if !cg.Variadic || cg.Arity != 1 {
		t.Errorf("CallGo = %+v, want arity-1 variadic", cg)
	}
	if len(cg.Args) != 1 {
		t.Fatalf("CallGo args = %d, want the single piped element", len(cg.Args))
	}
}

// `x |> f(_, "a")` fills the placeholder with the piped value: the
// hole's slot, not a new argument, and no residual closure.
func TestPipeBackGoHoleFill(t *testing.T) {
	g := gsym("G")
	sig := types.Fn(types.Int, types.P(types.String), types.P(types.String))
	x := vsym("x", types.String)
	rhs := app(vr(g, sig),
		[]check.Expr{strLit("a"), &check.Hole{Base: b(types.String)}},
		check.CallInfo{Kind: check.CallGo, Callee: g, Arity: 2, Applied: 2,
			ArgToParam: []int{0, 1}, HoleIndex: 1},
		types.Fn(types.Int, types.P(types.String)))
	pipe := &check.Binary{Base: b(types.Int), Op: syntax.BinPipeBack,
		X: rhs, Y: vr(x, types.String)}
	env := &Env{GoSig: func(sym *resolve.Symbol) (GoSig, bool) {
		return GoSig{Arity: 2, Results: core.GoResultsOne}, true
	}}
	out, diags := lowerWith(t, env,
		decl("f", []check.Param{param(vpat(x, types.String))}, pipe,
			types.Fn(types.Int, types.P(types.String))))
	for _, d := range diags {
		t.Errorf("unexpected diagnostic: %v", d)
	}
	cg := findCallGo(t, fnDeclOf(t, out, 0))
	if len(cg.Args) != 2 {
		t.Fatalf("CallGo args = %d, want 2 (hole filled)", len(cg.Args))
	}
	if _, ok := cg.Args[1].(*core.Var); !ok {
		t.Errorf("arg1 = %T, want the piped var", cg.Args[1])
	}
}
