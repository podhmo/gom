package eval_test

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

var sp = diag.SpanIn("test.gom", 10, 20)

// --- hand-built typed AST helpers ---

func b(t types.Type) check.Base { return check.Base{Sp: sp, Typ: t} }

func sym(name string, kind resolve.Kind) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: kind}
}

func intLit(n int64) *check.Lit {
	return &check.Lit{Base: b(types.Int), Kind: syntax.LitInt, Text: strconv.FormatInt(n, 10)}
}

func floatLit(f float64) *check.Lit {
	return &check.Lit{Base: b(types.Float), Kind: syntax.LitFloat, Text: strconv.FormatFloat(f, 'g', -1, 64)}
}

func strLit(s string) *check.Lit {
	return &check.Lit{Base: b(types.String), Kind: syntax.LitStr, Text: strconv.Quote(s)}
}

func boolLit(v bool) *check.Lit {
	return &check.Lit{Base: b(types.Bool), Kind: syntax.LitBool, Text: strconv.FormatBool(v)}
}

func unitLit() *check.Lit {
	return &check.Lit{Base: b(types.Unit), Kind: syntax.LitUnit, Text: "()"}
}

func vr(s *resolve.Symbol, t types.Type) *check.Var {
	return &check.Var{Base: b(t), Sym: s}
}

func arg(e check.Expr) check.Arg { return check.Arg{Value: e} }

func larg(label string, e check.Expr) check.Arg {
	return check.Arg{Label: label, Value: e}
}

func arityOf(t types.Type) int {
	if f, ok := t.(*types.Func); ok {
		return len(f.Params)
	}
	return 0
}

// directCall builds a saturated positional call `callee a1 a2 ...`.
func directCall(typ types.Type, callee *resolve.Symbol, fnTyp types.Type, args ...check.Expr) *check.Apply {
	as := make([]check.Arg, len(args))
	for i, a := range args {
		as[i] = arg(a)
	}
	return &check.Apply{
		Base: b(typ), Fn: vr(callee, fnTyp), Args: as,
		Call: check.CallInfo{
			Kind: check.CallDirect, Callee: callee,
			Arity: arityOf(fnTyp), Applied: len(args), HoleIndex: -1,
		},
	}
}

// unknownCall applies an arbitrary function expression positionally.
func unknownCall(typ types.Type, fn check.Expr, arity int, args ...check.Expr) *check.Apply {
	as := make([]check.Arg, len(args))
	for i, a := range args {
		as[i] = arg(a)
	}
	return &check.Apply{
		Base: b(typ), Fn: fn, Args: as,
		Call: check.CallInfo{
			Kind: check.CallUnknown, Arity: arity, Applied: len(args), HoleIndex: -1,
		},
	}
}

func block(t types.Type, stmts ...check.Expr) *check.Block {
	return &check.Block{Base: b(t), Stmts: stmts}
}

func let(p check.Pat, val check.Expr) *check.Let {
	return &check.Let{Base: b(types.Unit), Pat: p, Value: val}
}

// vpat returns a VarPat and its symbol.
func vpat(name string, t types.Type) (*check.VarPat, *resolve.Symbol) {
	s := sym(name, resolve.KindVar)
	return &check.VarPat{Base: b(t), Sym: s}, s
}

func wild(t types.Type) *check.WildPat { return &check.WildPat{Base: b(t)} }

func match(t types.Type, subj check.Expr, arms ...check.MatchArm) *check.Match {
	return &check.Match{Base: b(t), Subj: subj, Arms: arms}
}

func arm(p check.Pat, body check.Expr) check.MatchArm {
	return check.MatchArm{Sp: sp, Pat: p, Body: body}
}

func garm(p check.Pat, guard, body check.Expr) check.MatchArm {
	return check.MatchArm{Sp: sp, Pat: p, Guard: guard, Body: body}
}

func bin(op syntax.BinOp, t types.Type, x, y check.Expr) *check.Binary {
	return &check.Binary{Base: b(t), Op: op, X: x, Y: y}
}

func evalOK(t *testing.T, env *eval.Env, x check.Expr) eval.Value {
	t.Helper()
	r := eval.Expr(env, x)
	if !r.OK() {
		t.Fatalf("eval failed: %+v", r.Diags)
	}
	return r.Value
}

func evalFail(t *testing.T, env *eval.Env, x check.Expr) diag.Diagnostic {
	t.Helper()
	r := eval.Expr(env, x)
	if r.OK() {
		t.Fatalf("eval unexpectedly succeeded with %s", r.Value)
	}
	if len(r.Diags) == 0 {
		t.Fatal("eval failed without diagnostics")
	}
	return r.Diags[0]
}

// --- literals & primitive ops ---

func TestLiterals(t *testing.T) {
	env := eval.NewEnv()
	cases := []struct {
		name string
		x    check.Expr
		want string
	}{
		{"int", intLit(42), "42"},
		{"hex int", &check.Lit{Base: b(types.Int), Kind: syntax.LitInt, Text: "0x1F"}, "31"},
		{"int underscores", &check.Lit{Base: b(types.Int), Kind: syntax.LitInt, Text: "1_000"}, "1000"},
		{"float", floatLit(3.14), "3.14"},
		{"bool", boolLit(true), "true"},
		{"unit", unitLit(), "()"},
		{"string", strLit("hi"), `"hi"`},
		{"string escapes", &check.Lit{Base: b(types.String), Kind: syntax.LitStr, Text: `"a\nb"`}, `"a\nb"`},
		{"string unicode", &check.Lit{Base: b(types.String), Kind: syntax.LitStr, Text: `"\u0041"`}, `"A"`},
		{"string unicode brace", &check.Lit{Base: b(types.String), Kind: syntax.LitStr, Text: `"\u{1F600}"`}, `"😀"`},
		{"char", &check.Lit{Base: b(types.Char), Kind: syntax.LitChar, Text: `'a'`}, "'a'"},
		{"char escape", &check.Lit{Base: b(types.Char), Kind: syntax.LitChar, Text: `'\n'`}, "'\\n'"},
		{"dollar escape", &check.Lit{Base: b(types.String), Kind: syntax.LitStr, Text: `"\${x}"`}, `"${x}"`},
		{"bare string text", &check.Lit{Base: b(types.String), Kind: syntax.LitStr, Text: `raw`}, `"raw"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := evalOK(t, env, tc.x)
			if v.String() != tc.want {
				t.Errorf("got %s, want %s", v, tc.want)
			}
		})
	}
}

func TestArithmeticAndComparison(t *testing.T) {
	env := eval.NewEnv()
	charLit := func(s string) *check.Lit {
		return &check.Lit{Base: b(types.Char), Kind: syntax.LitChar, Text: s}
	}
	cases := []struct {
		name string
		x    check.Expr
		want string
	}{
		{"add", bin(syntax.BinAdd, types.Int, intLit(1), intLit(2)), "3"},
		{"sub", bin(syntax.BinSub, types.Int, intLit(10), intLit(3)), "7"},
		{"mul", bin(syntax.BinMul, types.Int, intLit(3), intLit(4)), "12"},
		{"div", bin(syntax.BinDiv, types.Int, intLit(7), intLit(2)), "3"},
		{"mod", bin(syntax.BinMod, types.Int, intLit(7), intLit(2)), "1"},
		{"neg", &check.Unary{Base: b(types.Int), Op: syntax.UnNeg, X: intLit(5)}, "-5"},
		{"float add", bin(syntax.BinAdd, types.Float, floatLit(1.5), floatLit(0.5)), "2"},
		{"float div", bin(syntax.BinDiv, types.Float, floatLit(1), floatLit(4)), "0.25"},
		{"float div by zero", bin(syntax.BinDiv, types.Float, floatLit(1), floatLit(0)), "+Inf"},
		{"lt", bin(syntax.BinLt, types.Bool, intLit(1), intLit(2)), "true"},
		{"le", bin(syntax.BinLe, types.Bool, intLit(2), intLit(2)), "true"},
		{"gt", bin(syntax.BinGt, types.Bool, intLit(1), intLit(2)), "false"},
		{"eq int", bin(syntax.BinEq, types.Bool, intLit(2), intLit(2)), "true"},
		{"ne string", bin(syntax.BinNe, types.Bool, strLit("a"), strLit("b")), "true"},
		{"eq unit", bin(syntax.BinEq, types.Bool, unitLit(), unitLit()), "true"},
		{"not", &check.Unary{Base: b(types.Bool), Op: syntax.UnNot, X: boolLit(true)}, "false"},
		{"char lt", bin(syntax.BinLt, types.Bool, charLit(`'a'`), charLit(`'b'`)), "true"},
		{"string lt", bin(syntax.BinLt, types.Bool, strLit("a"), strLit("b")), "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := evalOK(t, env, tc.x)
			if v.String() != tc.want {
				t.Errorf("got %s, want %s", v, tc.want)
			}
		})
	}
}

func TestIntDivByZero(t *testing.T) {
	d := evalFail(t, eval.NewEnv(),
		bin(syntax.BinDiv, types.Int, intLit(1), intLit(0)))
	if d.Code != eval.CodePanic {
		t.Fatalf("code = %s, want %s", d.Code, eval.CodePanic)
	}
}

// --- evaluation order witnesses ---

// tapper is a host function recording each call argument — the
// witness for evaluation order.
type tapper struct {
	calls []string
	sym   *resolve.Symbol
	fn    *eval.VFunc
}

func newTapper() *tapper {
	tr := &tapper{sym: sym("tap", resolve.KindFunc)}
	tr.fn = eval.NewHostFunc("tap", 1, func(args []eval.Value) (eval.Value, error) {
		s := args[0].(*eval.VStr).S
		tr.calls = append(tr.calls, s)
		return args[0], nil
	})
	return tr
}

func (tr *tapper) call(s string) *check.Apply {
	return &check.Apply{
		Base: b(types.String),
		Fn:   vr(tr.sym, types.Fn(types.String, types.P(types.String))),
		Args: []check.Arg{arg(strLit(s))},
		Call: check.CallInfo{Kind: check.CallDirect, Callee: tr.sym,
			Arity: 1, Applied: 1, HoleIndex: -1},
	}
}

func tapEnv(tr *tapper) *eval.Env {
	env := eval.NewEnv()
	env.Bind(tr.sym, tr.fn)
	return env
}

func assertTrace(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("trace = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("trace = %v, want %v", got, want)
		}
	}
}

func TestEvalOrder(t *testing.T) {
	t.Run("call args in source order", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		fSym := sym("f2", resolve.KindFunc)
		fTyp := types.Fn(types.Unit, types.P(types.String), types.P(types.String))
		env.Bind(fSym, eval.NewHostFunc("f2", 2, func(args []eval.Value) (eval.Value, error) {
			return &eval.VUnit{}, nil
		}))
		x := &check.Apply{Base: b(types.Unit), Fn: vr(fSym, fTyp),
			Args: []check.Arg{arg(tr.call("first")), arg(tr.call("second"))},
			Call: check.CallInfo{Kind: check.CallDirect, Callee: fSym, Arity: 2, Applied: 2, HoleIndex: -1}}
		evalOK(t, env, x)
		assertTrace(t, tr.calls, []string{"first", "second"})
	})

	t.Run("function expression evaluates before arguments", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		p, s := vpat("x", types.String)
		lam := &check.Lambda{Base: b(types.Fn(types.String, types.P(types.String))),
			Params: []check.Param{{Pat: p}}, Body: vr(s, types.String)}
		// { tap "head"; fn x -> x } (tap "arg")
		head := block(types.Fn(types.String, types.P(types.String)), tr.call("head"), lam)
		x := unknownCall(types.String, head, 1, tr.call("arg"))
		evalOK(t, env, x)
		assertTrace(t, tr.calls, []string{"head", "arg"})
	})

	t.Run("labeled args evaluate in source order not declaration order", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		fSym := sym("f3", resolve.KindFunc)
		fTyp := types.Fn(types.Unit, types.LP("a", types.String), types.LP("b", types.String))
		env.Bind(fSym, eval.NewHostFunc("f3", 2, func(args []eval.Value) (eval.Value, error) {
			return &eval.VUnit{}, nil
		}))
		// f ~b: (tap "b-arg") ~a: (tap "a-arg")
		x := &check.Apply{Base: b(types.Unit), Fn: vr(fSym, fTyp),
			Args: []check.Arg{larg("b", tr.call("b-arg")), larg("a", tr.call("a-arg"))},
			Call: check.CallInfo{Kind: check.CallDirect, Callee: fSym, Arity: 2, Applied: 2,
				ArgToParam: []int{1, 0}, HoleIndex: -1}}
		evalOK(t, env, x)
		assertTrace(t, tr.calls, []string{"b-arg", "a-arg"})
	})

	t.Run("list elements left-to-right", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		x := &check.ListLit{Base: b(types.List(types.String)),
			Elems: []check.Expr{tr.call("e1"), tr.call("e2"), tr.call("e3")}}
		evalOK(t, env, x)
		assertTrace(t, tr.calls, []string{"e1", "e2", "e3"})
	})

	t.Run("record fields left-to-right", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		con := &types.Con{Name: "R"}
		x := &check.RecordLit{Base: b(con), Fields: []check.FieldInit{
			{Sp: sp, Name: "x", Sym: sym("x", resolve.KindField), Value: tr.call("f1")},
			{Sp: sp, Name: "y", Sym: sym("y", resolve.KindField), Value: tr.call("f2")},
		}}
		evalOK(t, env, x)
		assertTrace(t, tr.calls, []string{"f1", "f2"})
	})

	t.Run("match subject then only the taken arm", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		x := match(types.String, tr.call("subj"),
			arm(wild(types.String), tr.call("arm1")),
			arm(wild(types.String), tr.call("arm2")))
		v := evalOK(t, env, x)
		assertTrace(t, tr.calls, []string{"subj", "arm1"})
		if v.String() != `"arm1"` {
			t.Errorf("got %s", v)
		}
	})

	t.Run("pipe x |> f evaluates x first", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		idSym := sym("id", resolve.KindFunc)
		env.Bind(idSym, eval.NewHostFunc("id", 1, func(args []eval.Value) (eval.Value, error) {
			return args[0], nil
		}))
		x := bin(syntax.BinPipeFwd, types.String, tr.call("x"),
			vr(idSym, types.Fn(types.String, types.P(types.String))))
		evalOK(t, env, x)
		assertTrace(t, tr.calls, []string{"x"})
	})

	t.Run("and short-circuits", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		x := bin(syntax.BinAnd, types.Bool, boolLit(false), tr.call("unreached"))
		v := evalOK(t, env, x)
		assertTrace(t, tr.calls, nil)
		if v.String() != "false" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("or short-circuits", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		x := bin(syntax.BinOr, types.Bool, boolLit(true), tr.call("unreached"))
		v := evalOK(t, env, x)
		assertTrace(t, tr.calls, nil)
		if v.String() != "true" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("try stops later evaluation in the body", func(t *testing.T) {
		tr := newTapper()
		errV := &eval.VVariant{
			Type: &types.Con{Name: "Result", Args: []types.Type{types.Int, types.GoError}},
			Name: "Err", Fields: []eval.VField{{Index: 0, V: &eval.VGoErr{Text: "boom"}}}}
		resT := types.Result(types.Int, types.GoError)
		rPat, _ := vpat("r", types.Int)
		uPat, _ := vpat("u", types.Unit)
		eSym := sym("e", resolve.KindVar)
		fSym := sym("f", resolve.KindFunc)
		decl := &check.FnDecl{Sp: sp, Sym: fSym,
			Sig:    types.Fn(resT, types.P(types.Unit)),
			Params: []check.Param{{Pat: uPat}},
			Body: block(resT,
				let(rPat, &check.Try{Base: b(types.Int), X: vr(eSym, resT)}),
				&check.Apply{Base: b(resT), Fn: vr(tr.sym, types.Fn(resT, types.P(types.String))),
					Args: []check.Arg{arg(strLit("after-try"))},
					Call: check.CallInfo{Kind: check.CallDirect, Callee: tr.sym, Arity: 1, Applied: 1, HoleIndex: -1}})}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		ev.Env().Bind(eSym, errV)
		ev.Env().Bind(tr.sym, tr.fn)
		r := ev.Expr(directCall(resT, fSym, types.Fn(resT, types.P(types.Unit)), unitLit()))
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		if r.Value.String() != `Err (error("boom"))` {
			t.Fatalf("got %s", r.Value)
		}
		assertTrace(t, tr.calls, nil)
	})
}

// --- closures, partial application, CallInfo ---

var intToInt = types.Fn(types.Int, types.P(types.Int))

func TestClosuresAndPartialApplication(t *testing.T) {
	addTyp := types.Fn(types.Int, types.P(types.Int), types.P(types.Int))
	addSym := sym("add", resolve.KindFunc)
	aSym, bSym := sym("a", resolve.KindVar), sym("b", resolve.KindVar)
	addDecl := &check.FnDecl{
		Sp: sp, Sym: addSym, Sig: addTyp,
		Params: []check.Param{
			{Pat: &check.VarPat{Base: b(types.Int), Sym: aSym}},
			{Pat: &check.VarPat{Base: b(types.Int), Sym: bSym}}},
		Body: bin(syntax.BinAdd, types.Int, vr(aSym, types.Int), vr(bSym, types.Int)),
	}
	ev := eval.New(&check.File{Sp: sp, Package: "p", Decls: []check.Decl{addDecl}})

	t.Run("saturated direct call", func(t *testing.T) {
		r := ev.Expr(directCall(types.Int, addSym, addTyp, intLit(1), intLit(2)))
		if !r.OK() || r.Value.String() != "3" {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("partial application returns a closure", func(t *testing.T) {
		partial := &check.Apply{
			Base: b(types.Fn(types.Int, types.P(types.Int))),
			Fn:   vr(addSym, addTyp),
			Args: []check.Arg{arg(intLit(1))},
			Call: check.CallInfo{Kind: check.CallPartial, Callee: addSym,
				Arity: 2, Applied: 1, Unapplied: 1, HoleIndex: -1},
		}
		r := ev.Expr(partial)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		f, ok := r.Value.(*eval.VFunc)
		if !ok {
			t.Fatalf("partial application produced %s, want fn", r.Value)
		}
		if f.String() != "<fn add/1>" {
			t.Fatalf("partial fn = %s", f)
		}
		done := ev.Apply(f, &eval.VInt{N: 41})
		if !done.OK() || done.Value.String() != "42" {
			t.Fatalf("apply result = %v diags=%v", done.Value, done.Diags)
		}
	})

	t.Run("labeled partial application leaves the rest", func(t *testing.T) {
		// transfer : Account -> ~to: Account -> ~amount: Money -> Money
		account := &types.Con{Name: "Account"}
		money := &types.Con{Name: "Money"}
		sig := types.Fn(money, types.P(account), types.LP("to", account), types.LP("amount", money))
		trSym := sym("transfer", resolve.KindFunc)
		fa, fb, fc := sym("from", resolve.KindVar), sym("to", resolve.KindVar), sym("amount", resolve.KindVar)
		decl := &check.FnDecl{Sp: sp, Sym: trSym, Sig: sig,
			Params: []check.Param{
				{Pat: &check.VarPat{Base: b(account), Sym: fa}},
				{Label: "to", Pat: &check.VarPat{Base: b(account), Sym: fb}},
				{Label: "amount", Pat: &check.VarPat{Base: b(money), Sym: fc}},
			},
			Body: vr(fc, money)}
		ev2 := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		// Stand-in argument values for alice/bob — they just need to be
		// bound so the Var references resolve.
		alice, bob := sym("alice", resolve.KindVar), sym("bob", resolve.KindVar)
		ev2.Env().Bind(alice, &eval.VUnit{})
		ev2.Env().Bind(bob, &eval.VUnit{})
		// transfer alice ~to: bob  -> remaining ~amount
		partial := &check.Apply{
			Base: b(types.Fn(money, types.LP("amount", money))),
			Fn:   vr(trSym, sig),
			Args: []check.Arg{arg(vr(alice, account)), larg("to", vr(bob, account))},
			Call: check.CallInfo{Kind: check.CallPartial, Callee: trSym,
				Arity: 3, Applied: 2, Unapplied: 1, ArgToParam: []int{0, 1}, HoleIndex: -1},
		}
		r := ev2.Expr(partial)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		done := ev2.Apply(r.Value, &eval.VInt{N: 100})
		if !done.OK() || done.Value.String() != "100" {
			t.Fatalf("apply result = %v diags=%v", done.Value, done.Diags)
		}
	})

	t.Run("labeled args reorder to declaration order", func(t *testing.T) {
		subTyp := types.Fn(types.Int, types.LP("x", types.Int), types.LP("y", types.Int))
		subSym := sym("sub", resolve.KindFunc)
		xS, yS := sym("x", resolve.KindVar), sym("y", resolve.KindVar)
		decl := &check.FnDecl{Sp: sp, Sym: subSym, Sig: subTyp,
			Params: []check.Param{
				{Label: "x", Pat: &check.VarPat{Base: b(types.Int), Sym: xS}},
				{Label: "y", Pat: &check.VarPat{Base: b(types.Int), Sym: yS}},
			},
			Body: bin(syntax.BinSub, types.Int, vr(xS, types.Int), vr(yS, types.Int))}
		ev3 := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		// sub ~y: 1 ~x: 10 => 10 - 1 = 9
		app := &check.Apply{Base: b(types.Int), Fn: vr(subSym, subTyp),
			Args: []check.Arg{larg("y", intLit(1)), larg("x", intLit(10))},
			Call: check.CallInfo{Kind: check.CallDirect, Callee: subSym,
				Arity: 2, Applied: 2, ArgToParam: []int{1, 0}, HoleIndex: -1}}
		r := ev3.Expr(app)
		if !r.OK() || r.Value.String() != "9" {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("closure captures env", func(t *testing.T) {
		kPat, kSym := vpat("k", types.Int)
		xPat, xSym := vpat("x", types.Int)
		lam := &check.Lambda{
			Base:   b(intToInt),
			Params: []check.Param{{Pat: xPat}},
			Body:   bin(syntax.BinAdd, types.Int, vr(xSym, types.Int), vr(kSym, types.Int)),
		}
		fPat, fSym := vpat("f", intToInt)
		x := block(types.Int,
			let(kPat, intLit(10)),
			let(fPat, lam),
			unknownCall(types.Int, vr(fSym, intToInt), 1, intLit(1)))
		v := evalOK(t, nil, x)
		if v.String() != "11" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("unknown function value applied via CallUnknown", func(t *testing.T) {
		p1, s1 := vpat("a", types.Int)
		p2, s2 := vpat("b", types.Int)
		lam := &check.Lambda{Base: b(addTyp), Params: []check.Param{{Pat: p1}, {Pat: p2}},
			Body: bin(syntax.BinSub, types.Int, vr(s1, types.Int), vr(s2, types.Int))}
		v := evalOK(t, nil, unknownCall(types.Int, lam, 2, intLit(10), intLit(3)))
		if v.String() != "7" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("under-application of an unknown function value", func(t *testing.T) {
		p1, s1 := vpat("a", types.Int)
		p2, s2 := vpat("b", types.Int)
		lam := &check.Lambda{Base: b(addTyp), Params: []check.Param{{Pat: p1}, {Pat: p2}},
			Body: bin(syntax.BinSub, types.Int, vr(s1, types.Int), vr(s2, types.Int))}
		env := eval.NewEnv()
		r := eval.Expr(env, unknownCall(intToInt, lam, 2, intLit(10)))
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		f := r.Value.(*eval.VFunc)
		if f.String() != "<fn fn/1>" {
			t.Fatalf("partial = %s", f)
		}
	})

	t.Run("CallUnknown with ArgToParam on a partially applied fn", func(t *testing.T) {
		// f a ~b ~c = a*100 + b*10 + c; `f 1` leaves ~b and ~c, which a
		// CallUnknown then fills out of order via ArgToParam.
		sig := types.Fn(types.Int,
			types.P(types.Int), types.LP("b", types.Int), types.LP("c", types.Int))
		fSym := sym("f", resolve.KindFunc)
		aS, bS, cS := sym("a", resolve.KindVar), sym("b", resolve.KindVar), sym("c", resolve.KindVar)
		decl := &check.FnDecl{Sp: sp, Sym: fSym, Sig: sig,
			Params: []check.Param{
				{Pat: &check.VarPat{Base: b(types.Int), Sym: aS}},
				{Label: "b", Pat: &check.VarPat{Base: b(types.Int), Sym: bS}},
				{Label: "c", Pat: &check.VarPat{Base: b(types.Int), Sym: cS}},
			},
			Body: bin(syntax.BinAdd, types.Int,
				bin(syntax.BinAdd, types.Int,
					bin(syntax.BinMul, types.Int, vr(aS, types.Int), intLit(100)),
					bin(syntax.BinMul, types.Int, vr(bS, types.Int), intLit(10))),
				vr(cS, types.Int))}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		partTyp := types.Fn(types.Int, types.LP("b", types.Int), types.LP("c", types.Int))
		partial := &check.Apply{Base: b(partTyp), Fn: vr(fSym, sig),
			Args: []check.Arg{arg(intLit(1))},
			Call: check.CallInfo{Kind: check.CallPartial, Callee: fSym,
				Arity: 3, Applied: 1, Unapplied: 2, HoleIndex: -1}}
		r := ev.Expr(partial)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		pSym := sym("p", resolve.KindVar)
		ev.Env().Bind(pSym, r.Value)
		// p ~c: 30 ~b: 20 — ArgToParam indexes the remaining-parameter
		// list [b, c]: arg0 fills index 1 (c), arg1 fills index 0 (b).
		app := &check.Apply{Base: b(types.Int), Fn: vr(pSym, partTyp),
			Args: []check.Arg{larg("c", intLit(30)), larg("b", intLit(20))},
			Call: check.CallInfo{Kind: check.CallUnknown,
				Arity: 2, Applied: 2, ArgToParam: []int{1, 0}, HoleIndex: -1}}
		r2 := ev.Expr(app)
		if !r2.OK() || r2.Value.String() != "330" {
			t.Fatalf("result = %v diags=%v", r2.Value, r2.Diags)
		}
	})

	t.Run("over-application applies to returned function", func(t *testing.T) {
		// mk : Int -> (Int -> Int), declared arity 1, returns a lambda.
		mkTyp := types.Fn(intToInt, types.P(types.Int))
		mkSym := sym("mk", resolve.KindFunc)
		aS, yS := sym("a", resolve.KindVar), sym("y", resolve.KindVar)
		inner := &check.Lambda{Base: b(intToInt),
			Params: []check.Param{{Pat: &check.VarPat{Base: b(types.Int), Sym: yS}}},
			Body:   bin(syntax.BinAdd, types.Int, vr(aS, types.Int), vr(yS, types.Int))}
		decl := &check.FnDecl{Sp: sp, Sym: mkSym, Sig: mkTyp,
			Params: []check.Param{{Pat: &check.VarPat{Base: b(types.Int), Sym: aS}}},
			Body:   inner}
		ev4 := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		app := &check.Apply{Base: b(types.Int), Fn: vr(mkSym, mkTyp),
			Args: []check.Arg{arg(intLit(1)), arg(intLit(2))},
			Call: check.CallInfo{Kind: check.CallDirect, Callee: mkSym,
				Arity: 1, Applied: 2, HoleIndex: -1}}
		r := ev4.Expr(app)
		if !r.OK() || r.Value.String() != "3" {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("function composition", func(t *testing.T) {
		nPat, nSym := vpat("n", types.Int)
		inc := &check.Lambda{Base: b(intToInt), Params: []check.Param{{Pat: nPat}},
			Body: bin(syntax.BinAdd, types.Int, vr(nSym, types.Int), intLit(1))}
		mPat, mSym := vpat("m", types.Int)
		double := &check.Lambda{Base: b(intToInt), Params: []check.Param{{Pat: mPat}},
			Body: bin(syntax.BinMul, types.Int, vr(mSym, types.Int), intLit(2))}
		// inc >> double = fn x -> double (inc x)
		comp := bin(syntax.BinComposeFwd, intToInt, inc, double)
		v := evalOK(t, nil, unknownCall(types.Int, comp, 1, intLit(3)))
		if v.String() != "8" {
			t.Fatalf("got %s", v)
		}
		// inc << double = fn x -> inc (double x)
		comp2 := bin(syntax.BinComposeBack, intToInt, inc, double)
		v = evalOK(t, nil, unknownCall(types.Int, comp2, 1, intLit(3)))
		if v.String() != "7" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("composition with a multi-parameter second function", func(t *testing.T) {
		// inc >> add = fn x y -> add (inc x) y — g : Int -> Int -> Int
		// composes into an arity-2 function.
		nPat, nSym := vpat("n", types.Int)
		inc := &check.Lambda{Base: b(intToInt), Params: []check.Param{{Pat: nPat}},
			Body: bin(syntax.BinAdd, types.Int, vr(nSym, types.Int), intLit(1))}
		aPat, aSym := vpat("a", types.Int)
		bPat, bSym := vpat("b", types.Int)
		add := &check.Lambda{Base: b(addTyp), Params: []check.Param{{Pat: aPat}, {Pat: bPat}},
			Body: bin(syntax.BinAdd, types.Int, vr(aSym, types.Int), vr(bSym, types.Int))}
		comp := bin(syntax.BinComposeFwd, addTyp, inc, add)
		v := evalOK(t, nil, unknownCall(types.Int, comp, 2, intLit(3), intLit(10)))
		if v.String() != "14" {
			t.Fatalf("got %s", v)
		}
		// Under-applying leaves a one-parameter function that can be
		// completed later.
		ev := eval.New(nil)
		r := ev.Expr(unknownCall(intToInt, comp, 2, intLit(3)))
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		f, ok := r.Value.(*eval.VFunc)
		if !ok || f.String() != "<fn fn/1>" {
			t.Fatalf("partial = %v", r.Value)
		}
		done := ev.Apply(f, &eval.VInt{N: 10})
		if !done.OK() || done.Value.String() != "14" {
			t.Fatalf("apply result = %v diags=%v", done.Value, done.Diags)
		}
	})

	t.Run("apply of a non-function fails", func(t *testing.T) {
		app := unknownCall(types.Int, intLit(1), 1, intLit(2))
		d := evalFail(t, nil, app)
		if d.Code != eval.CodeInternal {
			t.Fatalf("code = %s", d.Code)
		}
	})
}

// --- match ---

func TestMatch(t *testing.T) {
	env := eval.NewEnv()

	t.Run("literal and wildcard dispatch in order", func(t *testing.T) {
		x := match(types.Int, intLit(2),
			arm(&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "1"}, intLit(10)),
			arm(&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "2"}, intLit(20)),
			arm(wild(types.Int), intLit(30)))
		v := evalOK(t, env, x)
		if v.String() != "20" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("binding pattern", func(t *testing.T) {
		p, s := vpat("n", types.Int)
		x := match(types.Int, intLit(5),
			arm(p, bin(syntax.BinMul, types.Int, vr(s, types.Int), intLit(2))))
		v := evalOK(t, env, x)
		if v.String() != "10" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("or pattern", func(t *testing.T) {
		x := match(types.Int, intLit(2),
			arm(&check.OrPat{Base: b(types.Int), Alts: []check.Pat{
				&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "1"},
				&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "2"},
			}}, intLit(99)),
			arm(wild(types.Int), intLit(0)))
		v := evalOK(t, env, x)
		if v.String() != "99" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("or pattern binding chosen alternative", func(t *testing.T) {
		p, s := vpat("n", types.Int)
		x := match(types.Int, intLit(7),
			arm(&check.OrPat{Base: b(types.Int), Alts: []check.Pat{
				&check.LitPat{Base: b(types.Int), Kind: syntax.LitInt, Text: "1"},
				p,
			}}, vr(s, types.Int)))
		v := evalOK(t, env, x)
		if v.String() != "7" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("as pattern binds the whole", func(t *testing.T) {
		inner, nSym := vpat("n", types.Int)
		as := &check.AsPat{Base: b(types.Int), Pat: inner, Sym: sym("whole", resolve.KindVar)}
		x := match(types.Int, intLit(3), arm(as, vr(nSym, types.Int)))
		v := evalOK(t, env, x)
		if v.String() != "3" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("guard decides between otherwise matching arms", func(t *testing.T) {
		p, s := vpat("n", types.Int)
		p2, _ := vpat("m", types.Int)
		x := match(types.Int, intLit(3),
			garm(p, bin(syntax.BinGt, types.Bool, vr(s, types.Int), intLit(10)), intLit(1)),
			arm(p2, intLit(2)))
		v := evalOK(t, env, x)
		if v.String() != "2" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("tuple pattern", func(t *testing.T) {
		p1, s1 := vpat("a", types.Int)
		p2, s2 := vpat("b", types.Int)
		tup := &check.TupleLit{Base: b(types.Tup(types.Int, types.Int)),
			Elems: []check.Expr{intLit(3), intLit(4)}}
		x := match(types.Int, tup,
			arm(&check.TuplePat{Base: b(types.Tup(types.Int, types.Int)), Elems: []check.Pat{p1, p2}},
				bin(syntax.BinAdd, types.Int, vr(s1, types.Int), vr(s2, types.Int))))
		v := evalOK(t, env, x)
		if v.String() != "7" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("cons pattern binds head", func(t *testing.T) {
		hd, hSym := vpat("h", types.Int)
		tl, _ := vpat("t", types.List(types.Int))
		cons := &check.ListPat{Base: b(types.List(types.Int)), Elems: []check.Pat{hd}, Rest: tl}
		x := match(types.Int,
			&check.ListLit{Base: b(types.List(types.Int)), Elems: []check.Expr{intLit(1), intLit(2), intLit(3)}},
			arm(&check.ListPat{Base: b(types.List(types.Int))}, intLit(0)),
			arm(cons, vr(hSym, types.Int)))
		v := evalOK(t, eval.NewEnv(), x)
		if v.String() != "1" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("list pattern rest sharing", func(t *testing.T) {
		tl, tSym := vpat("t", types.List(types.Int))
		cons := &check.ListPat{Base: b(types.List(types.Int)),
			Elems: []check.Pat{wild(types.Int)}, Rest: tl}
		x := match(types.List(types.Int),
			&check.ListLit{Base: b(types.List(types.Int)), Elems: []check.Expr{intLit(1), intLit(2), intLit(3)}},
			arm(cons, vr(tSym, types.List(types.Int))))
		v := evalOK(t, eval.NewEnv(), x)
		if v.String() != "[2, 3]" {
			t.Fatalf("got %s", v)
		}
	})
}

// --- ADTs ---

// shapeFile builds:
//
//	type Shape = | Circle (radius: Float) | Rect (w: Float) (h: Float) | Empty
func shapeFile() (*check.File, map[string]*resolve.Symbol, *types.Con) {
	con := &types.Con{Name: "Shape"}
	shapeSym := sym("Shape", resolve.KindType)
	circle := sym("Circle", resolve.KindCtor)
	rect := sym("Rect", resolve.KindCtor)
	empty := sym("Empty", resolve.KindCtor)
	radiusF := sym("radius", resolve.KindField)
	decl := &check.TypeDecl{
		Sp: sp, Sym: shapeSym,
		Body: &check.SumType{Sp: sp, Variants: []check.Variant{
			{Sp: sp, Sym: circle, Fields: []check.TField{
				{Sp: sp, Name: "radius", Index: 0, Sym: radiusF, Type: types.Float}}},
			{Sp: sp, Sym: rect, Fields: []check.TField{
				{Sp: sp, Name: "w", Index: 0, Sym: sym("w", resolve.KindField), Type: types.Float},
				{Sp: sp, Name: "h", Index: 1, Sym: sym("h", resolve.KindField), Type: types.Float}}},
			{Sp: sp, Sym: empty},
		}},
	}
	f := &check.File{Sp: sp, Package: "p", Decls: []check.Decl{decl}}
	return f, map[string]*resolve.Symbol{
		"Circle": circle, "Rect": rect, "Empty": empty, "radius": radiusF,
	}, con
}

func TestADT(t *testing.T) {
	f, syms, con := shapeFile()
	ev := eval.New(f)
	circleSig := types.Fn(con, types.P(types.Float))
	rectSig := types.Fn(con, types.P(types.Float), types.P(types.Float))

	t.Run("nullary variant", func(t *testing.T) {
		v := evalOK(t, ev.Env(), vr(syms["Empty"], con))
		if v.String() != "Empty" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("variant construction and match", func(t *testing.T) {
		c := ev.Expr(directCall(con, syms["Circle"], circleSig, floatLit(1.5)))
		if !c.OK() {
			t.Fatalf("eval failed: %+v", c.Diags)
		}
		if c.Value.String() != "Circle ~radius: 1.5" {
			t.Fatalf("got %s", c.Value)
		}
		sPat, rSym := vpat("r", types.Float)
		subjPat, sSym := vpat("s", con)
		x := block(types.Float,
			let(subjPat, directCall(con, syms["Circle"], circleSig, floatLit(1.5))),
			match(types.Float, vr(sSym, con),
				arm(&check.CtorPat{Base: b(con), Ctor: syms["Circle"],
					Name: &syntax.Ident{Sp: sp, Name: "Circle"},
					Args: []check.CtorArg{{Pat: sPat}}}, vr(rSym, types.Float)),
				arm(wild(con), floatLit(0))))
		v := ev.Expr(x)
		if !v.OK() || v.Value.String() != "1.5" {
			t.Fatalf("result = %v diags=%v", v.Value, v.Diags)
		}
	})

	t.Run("variant field access by index", func(t *testing.T) {
		sPat, sSym := vpat("s", con)
		m := &check.MemberExpr{Base: b(types.Float), Recv: vr(sSym, con),
			Sel: &syntax.Ident{Sp: sp, Name: "w"}, Sym: sym("w", resolve.KindField), Index: 0}
		x := block(types.Float,
			let(sPat, directCall(con, syms["Rect"], rectSig, floatLit(2), floatLit(3))),
			m)
		r := ev.Expr(x)
		if !r.OK() || r.Value.String() != "2" {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("variant field access by name", func(t *testing.T) {
		sPat, sSym := vpat("s", con)
		m := &check.MemberExpr{Base: b(types.Float), Recv: vr(sSym, con),
			Sel: &syntax.Ident{Sp: sp, Name: "h"}, Sym: sym("h", resolve.KindField), Index: -1}
		x := block(types.Float,
			let(sPat, directCall(con, syms["Rect"], rectSig, floatLit(2), floatLit(3))),
			m)
		r := ev.Expr(x)
		if !r.OK() || r.Value.String() != "3" {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("variant structural equality", func(t *testing.T) {
		mk := func() eval.Value {
			r := ev.Expr(directCall(con, syms["Rect"], rectSig, floatLit(2), floatLit(3)))
			return r.Value
		}
		r3 := ev.Expr(directCall(con, syms["Rect"], rectSig, floatLit(2), floatLit(4)))
		if !eval.Equal(mk(), mk()) {
			t.Fatal("equal variants reported unequal")
		}
		if eval.Equal(mk(), r3.Value) {
			t.Fatal("unequal variants reported equal")
		}
	})

	t.Run("match failure is a diagnostic", func(t *testing.T) {
		sPat, sSym := vpat("s", con)
		x := block(types.Float,
			let(sPat, directCall(con, syms["Circle"], circleSig, floatLit(1))),
			match(types.Float, vr(sSym, con),
				arm(&check.CtorPat{Base: b(con), Ctor: syms["Rect"],
					Name: &syntax.Ident{Sp: sp, Name: "Rect"},
					Args: []check.CtorArg{{Pat: wild(types.Float)}, {Pat: wild(types.Float)}}}, floatLit(0))))
		r := ev.Expr(x)
		if r.OK() {
			t.Fatalf("match unexpectedly succeeded: %s", r.Value)
		}
		if r.Diags[0].Code != eval.CodeMatchFail {
			t.Fatalf("code = %s, want %s", r.Diags[0].Code, eval.CodeMatchFail)
		}
	})

	t.Run("ctor used as a function value", func(t *testing.T) {
		f := ev.Expr(vr(syms["Circle"], circleSig))
		if !f.OK() {
			t.Fatalf("ctor var: %+v", f.Diags)
		}
		applied := ev.Apply(f.Value, &eval.VFloat{F: 2})
		if applied.Value.String() != "Circle ~radius: 2" {
			t.Fatalf("got %s", applied.Value)
		}
	})
}

// --- try / Result / |?> ---

func resultDecl(tk, te types.Type, okSym, errSym *resolve.Symbol) *check.TypeDecl {
	return &check.TypeDecl{Sp: sp, Sym: sym("Result", resolve.KindType),
		Body: &check.SumType{Sp: sp, Variants: []check.Variant{
			{Sp: sp, Sym: okSym, Fields: []check.TField{{Sp: sp, Index: 0, Type: tk}}},
			{Sp: sp, Sym: errSym, Fields: []check.TField{{Sp: sp, Index: 0, Type: te}}},
		}}}
}

func TestTry(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	okSym := sym("Ok", resolve.KindCtor)
	errSym := sym("Err", resolve.KindCtor)
	decl := resultDecl(types.Int, types.GoError, okSym, errSym)

	mkOk := func(x check.Expr) check.Expr {
		return directCall(resT, okSym, types.Fn(resT, types.P(types.Int)), x)
	}
	mkErrV := func(text string) eval.Value {
		return &eval.VVariant{Type: resT.(*types.Con), Name: "Err",
			Fields: []eval.VField{{Index: 0, V: &eval.VGoErr{Text: text}}}}
	}

	t.Run("try unwraps Ok", func(t *testing.T) {
		// No `type Result` declaration — the builtin's ctor metadata
		// is derived lazily, so the Ok value is a builtin Result.
		ev := eval.New(&check.File{Sp: sp})
		r := ev.Expr(&check.Try{Base: b(types.Int), X: mkOk(intLit(7))})
		if !r.OK() || r.Value.String() != "7" {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("try refuses a file-local Result", func(t *testing.T) {
		// A declared `type Result` shadows the builtin: its Ok/Err
		// variants are ordinary ADT values, not escape-able Results.
		// The checker reports E0415 before this point — the evaluator's
		// invariant is the backstop.
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		r := ev.Expr(&check.Try{Base: b(types.Int), X: mkOk(intLit(7))})
		if r.OK() || r.Diags[0].Code != eval.CodeInternal {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("try escapes to enclosing fn boundary", func(t *testing.T) {
		eSym := sym("e", resolve.KindVar)
		uPat, _ := vpat("u", types.Unit)
		fSym := sym("f", resolve.KindFunc)
		sig := types.Fn(resT, types.P(types.Unit))
		decl := &check.FnDecl{Sp: sp, Sym: fSym, Sig: sig,
			Params: []check.Param{{Pat: uPat}},
			Body:   &check.Try{Base: b(types.Int), X: vr(eSym, resT)}}
		ev2 := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		ev2.Env().Bind(eSym, mkErrV("boom"))
		r := ev2.Expr(directCall(resT, fSym, sig, unitLit()))
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		if r.Value.String() != `Err (error("boom"))` {
			t.Fatalf("got %s", r.Value)
		}
	})

	t.Run("try escapes only to the innermost boundary", func(t *testing.T) {
		// outer calls inner; inner try-escapes and itself returns Err —
		// outer sees the Err value (no transparent propagation past
		// the boundary).
		eSym := sym("e", resolve.KindVar)
		uPat, _ := vpat("u", types.Unit)
		innerSym := sym("inner", resolve.KindFunc)
		sig := types.Fn(resT, types.P(types.Unit))
		inner := &check.FnDecl{Sp: sp, Sym: innerSym, Sig: sig,
			Params: []check.Param{{Pat: uPat}},
			Body:   &check.Try{Base: b(types.Int), X: vr(eSym, resT)}}
		outerSym := sym("outer", resolve.KindFunc)
		rPat, rSym2 := vpat("r", resT)
		outer := &check.FnDecl{Sp: sp, Sym: outerSym, Sig: sig,
			Params: []check.Param{{Pat: uPat}},
			Body: block(resT,
				let(rPat, directCall(resT, innerSym, sig, unitLit())),
				vr(rSym2, resT))}
		ev3 := eval.New(&check.File{Sp: sp, Decls: []check.Decl{inner, outer}})
		ev3.Env().Bind(eSym, mkErrV("deep"))
		r := ev3.Expr(directCall(resT, outerSym, sig, unitLit()))
		if !r.OK() || r.Value.String() != `Err (error("deep"))` {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("try on a non-Result is an invariant error", func(t *testing.T) {
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		r := ev.Expr(&check.Try{Base: b(types.Int), X: intLit(1)})
		if r.OK() || r.Diags[0].Code != eval.CodeInternal {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("try escaping with no boundary is an invariant error", func(t *testing.T) {
		eSym := sym("e", resolve.KindVar)
		env := eval.NewEnv()
		env.Bind(eSym, mkErrV("loose"))
		r := eval.Expr(env, &check.Try{Base: b(types.Int), X: vr(eSym, resT)})
		if r.OK() || r.Diags[0].Code != eval.CodeInternal {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("try escapes to a lambda boundary too", func(t *testing.T) {
		// The boundary is "the innermost function", not only FnDecls:
		// a lambda body returning through try yields the Err value.
		eSym := sym("e", resolve.KindVar)
		uPat, _ := vpat("u", types.Unit)
		lam := &check.Lambda{Base: b(types.Fn(resT, types.P(types.Unit))),
			Params: []check.Param{{Pat: uPat}},
			Body:   &check.Try{Base: b(types.Int), X: vr(eSym, resT)}}
		env := eval.NewEnv()
		env.Bind(eSym, mkErrV("lam"))
		r := eval.Expr(env, unknownCall(resT, lam, 1, unitLit()))
		if !r.OK() || r.Value.String() != `Err (error("lam"))` {
			t.Fatalf("result = %v diags=%v", r.Value, r.Diags)
		}
	})
}

func TestPipeTry(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	errV := func(text string) eval.Value {
		return &eval.VVariant{Type: resT.(*types.Con), Name: "Err",
			Fields: []eval.VField{{Index: 0, V: &eval.VGoErr{Text: text}}}}
	}
	errSymV := sym("e", resolve.KindVar)
	stepSym := sym("parse", resolve.KindVar)

	t.Run("Err passes through untouched", func(t *testing.T) {
		env := eval.NewEnv()
		env.Bind(errSymV, errV("boom"))
		env.Bind(stepSym, eval.NewHostFunc("parse", 1, func(args []eval.Value) (eval.Value, error) {
			return nil, errors.New("unreached")
		}))
		x := bin(syntax.BinPipeTry, resT, vr(errSymV, resT),
			vr(stepSym, types.Fn(resT, types.P(types.Int))))
		r := eval.Expr(env, x)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		if r.Value.String() != `Err (error("boom"))` {
			t.Fatalf("got %s", r.Value)
		}
	})

	t.Run("Err input never evaluates the step expression", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		env.Bind(errSymV, errV("boom"))
		// The step expression has a side effect; on Err input it must
		// not run at all (the step evaluates only inside the Ok arm).
		p, s := vpat("x", types.Int)
		step := block(types.Fn(resT, types.P(types.Int)),
			&check.Ignore{Base: b(types.Unit), X: tr.call("rhs")},
			&check.Lambda{Base: b(types.Fn(resT, types.P(types.Int))),
				Params: []check.Param{{Pat: p}}, Body: vr(s, resT)})
		x := bin(syntax.BinPipeTry, resT, vr(errSymV, resT), step)
		r := eval.Expr(env, x)
		if !r.OK() || r.Value.String() != `Err (error("boom"))` {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
		assertTrace(t, tr.calls, nil)
	})

	t.Run("Ok into a failing step wraps the GoError once", func(t *testing.T) {
		env := eval.NewEnv()
		env.Bind(errSymV, &eval.VVariant{Type: resT.(*types.Con), Name: "Ok",
			Fields: []eval.VField{{Index: 0, V: &eval.VInt{N: 5}}}})
		env.Bind(stepSym, eval.NewHostFunc("parse", 1, func(args []eval.Value) (eval.Value, error) {
			return errV("bad input"), nil
		}))
		x := bin(syntax.BinPipeTry, resT, vr(errSymV, resT),
			vr(stepSym, types.Fn(resT, types.P(types.Int))))
		r := eval.Expr(env, x)
		if !r.OK() || r.Value.String() != `Err (error("parse: bad input"))` {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
		// The wrap keeps the original error in Wrapped.
		variant := r.Value.(*eval.VVariant)
		goErr := variant.Fields[0].V.(*eval.VGoErr)
		if orig, ok := goErr.Wrapped.(*eval.VGoErr); !ok || orig.Text != "bad input" {
			t.Fatalf("Wrapped = %v, want the original error", goErr.Wrapped)
		}
	})

	t.Run("a later step does not re-wrap a flowing Err", func(t *testing.T) {
		// Ok 5 |?> f |?> g: f fails with `f: boom`; g sees nothing and
		// the error text stays single-wrapped.
		fSym := sym("f", resolve.KindVar)
		gSym := sym("g", resolve.KindVar)
		env := eval.NewEnv()
		env.Bind(errSymV, &eval.VVariant{Type: resT.(*types.Con), Name: "Ok",
			Fields: []eval.VField{{Index: 0, V: &eval.VInt{N: 5}}}})
		env.Bind(fSym, eval.NewHostFunc("f", 1, func(args []eval.Value) (eval.Value, error) {
			return errV("boom"), nil
		}))
		env.Bind(gSym, eval.NewHostFunc("g", 1, func(args []eval.Value) (eval.Value, error) {
			return nil, errors.New("g evaluated")
		}))
		inner := bin(syntax.BinPipeTry, resT, vr(errSymV, resT),
			vr(fSym, types.Fn(resT, types.P(types.Int))))
		x := bin(syntax.BinPipeTry, resT, inner,
			vr(gSym, types.Fn(resT, types.P(types.Int))))
		r := eval.Expr(env, x)
		if !r.OK() || r.Value.String() != `Err (error("f: boom"))` {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("non-Result start wraps the step's Err", func(t *testing.T) {
		env := eval.NewEnv()
		env.Bind(stepSym, eval.NewHostFunc("parse", 1, func(args []eval.Value) (eval.Value, error) {
			return errV("nope"), nil
		}))
		x := bin(syntax.BinPipeTry, resT, intLit(5),
			vr(stepSym, types.Fn(resT, types.P(types.Int))))
		r := eval.Expr(env, x)
		if !r.OK() || r.Value.String() != `Err (error("parse: nope"))` {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("Ok applies the step", func(t *testing.T) {
		env := eval.NewEnv()
		env.Bind(errSymV, &eval.VVariant{Type: resT.(*types.Con), Name: "Ok",
			Fields: []eval.VField{{Index: 0, V: &eval.VInt{N: 5}}}})
		env.Bind(stepSym, eval.NewHostFunc("parse", 1, func(args []eval.Value) (eval.Value, error) {
			return &eval.VVariant{Type: resT.(*types.Con), Name: "Ok",
				Fields: []eval.VField{{Index: 0, V: &eval.VInt{N: args[0].(*eval.VInt).N + 1}}}}, nil
		}))
		x := bin(syntax.BinPipeTry, resT, vr(errSymV, resT),
			vr(stepSym, types.Fn(resT, types.P(types.Int))))
		r := eval.Expr(env, x)
		if !r.OK() || r.Value.String() != "Ok 6" {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("non-Result first value is wrapped in Ok", func(t *testing.T) {
		env := eval.NewEnv()
		env.Bind(stepSym, eval.NewHostFunc("parse", 1, func(args []eval.Value) (eval.Value, error) {
			return &eval.VVariant{Type: resT.(*types.Con), Name: "Ok",
				Fields: []eval.VField{{Index: 0, V: args[0]}}}, nil
		}))
		x := bin(syntax.BinPipeTry, resT, intLit(3),
			vr(stepSym, types.Fn(resT, types.P(types.Int))))
		r := eval.Expr(env, x)
		if !r.OK() || r.Value.String() != "Ok 3" {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})
}

// --- collections ---

func TestCollections(t *testing.T) {
	env := eval.NewEnv()

	t.Run("tuple literal", func(t *testing.T) {
		x := &check.TupleLit{Base: b(types.Tup(types.Int, types.String)),
			Elems: []check.Expr{intLit(1), strLit("a")}}
		v := evalOK(t, env, x)
		if v.String() != `(1, "a")` {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("list literal and spread", func(t *testing.T) {
		rest, restSym := vpat("rest", types.List(types.Int))
		x := block(types.List(types.Int),
			let(rest, &check.ListLit{Base: b(types.List(types.Int)),
				Elems: []check.Expr{intLit(2), intLit(3)}}),
			&check.ListLit{Base: b(types.List(types.Int)), Elems: []check.Expr{
				intLit(1),
				&check.Spread{Base: b(types.List(types.Int)), X: vr(restSym, types.List(types.Int))},
			}})
		v := evalOK(t, env, x)
		if v.String() != "[1, 2, 3]" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("record literal and field access", func(t *testing.T) {
		con := &types.Con{Name: "P"}
		nameF, ageF := sym("name", resolve.KindField), sym("age", resolve.KindField)
		pat, pSym := vpat("p", con)
		rec := &check.RecordLit{Base: b(con), Fields: []check.FieldInit{
			{Sp: sp, Name: "name", Sym: nameF, Value: strLit("a")},
			{Sp: sp, Name: "age", Sym: ageF, Value: intLit(3)},
		}}
		x := block(types.Int,
			let(pat, rec),
			&check.MemberExpr{Base: b(types.Int), Recv: vr(pSym, con),
				Sel: &syntax.Ident{Sp: sp, Name: "age"}, Sym: ageF, Index: 1})
		v := evalOK(t, env, x)
		if v.String() != "3" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("record update keeps other fields", func(t *testing.T) {
		con := &types.Con{Name: "P"}
		nameF, ageF := sym("name", resolve.KindField), sym("age", resolve.KindField)
		pat, pSym := vpat("p", con)
		rec := &check.RecordLit{Base: b(con), Fields: []check.FieldInit{
			{Sp: sp, Name: "name", Sym: nameF, Value: strLit("a")},
			{Sp: sp, Name: "age", Sym: ageF, Value: intLit(3)},
		}}
		upd := &check.RecordLit{Base: b(con), With: vr(pSym, con), Fields: []check.FieldInit{
			{Sp: sp, Name: "age", Sym: ageF, Value: intLit(4)},
		}}
		x := block(con, let(pat, rec), upd)
		v := evalOK(t, env, x)
		if v.String() != `{ name = "a", age = 4 }` {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("record with declared type gets declaration order", func(t *testing.T) {
		con := &types.Con{Name: "P"}
		nameF, ageF := sym("name", resolve.KindField), sym("age", resolve.KindField)
		decl := &check.TypeDecl{Sp: sp, Sym: sym("P", resolve.KindType),
			Body: &check.RecordType{Sp: sp, Fields: []check.TField{
				{Sp: sp, Name: "name", Index: 0, Sym: nameF, Type: types.String},
				{Sp: sp, Name: "age", Index: 1, Sym: ageF, Type: types.Int},
			}}}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		rec := &check.RecordLit{Base: b(con), Fields: []check.FieldInit{
			{Sp: sp, Name: "age", Sym: ageF, Value: intLit(3)},
			{Sp: sp, Name: "name", Sym: nameF, Value: strLit("a")},
		}}
		r := ev.Expr(rec)
		if !r.OK() || r.Value.String() != `{ name = "a", age = 3 }` {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("record missing a declared field is an invariant error", func(t *testing.T) {
		con := &types.Con{Name: "P"}
		nameF, ageF := sym("name", resolve.KindField), sym("age", resolve.KindField)
		decl := &check.TypeDecl{Sp: sp, Sym: sym("P", resolve.KindType),
			Body: &check.RecordType{Sp: sp, Fields: []check.TField{
				{Sp: sp, Name: "name", Index: 0, Sym: nameF, Type: types.String},
				{Sp: sp, Name: "age", Index: 1, Sym: ageF, Type: types.Int},
			}}}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{decl}})
		rec := &check.RecordLit{Base: b(con), Fields: []check.FieldInit{
			{Sp: sp, Name: "name", Sym: nameF, Value: strLit("a")},
		}}
		r := ev.Expr(rec)
		if r.OK() || len(r.Diags) == 0 || r.Diags[0].Code != eval.CodeInternal {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})
}

// --- control flow and sugar ---

func TestControlAndSugar(t *testing.T) {
	env := eval.NewEnv()

	t.Run("if else", func(t *testing.T) {
		x := &check.If{Base: b(types.Int), Cond: boolLit(true), Then: intLit(1), Else: intLit(2)}
		if v := evalOK(t, env, x); v.String() != "1" {
			t.Fatalf("got %s", v)
		}
		x.Cond = boolLit(false)
		if v := evalOK(t, env, x); v.String() != "2" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("if without else returns unit", func(t *testing.T) {
		x := &check.If{Base: b(types.Unit), Cond: boolLit(false), Then: unitLit()}
		if v := evalOK(t, env, x); v.String() != "()" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("block value is last expr; let scopes", func(t *testing.T) {
		p, s := vpat("x", types.Int)
		blk := block(types.Int,
			let(p, intLit(1)),
			bin(syntax.BinAdd, types.Int, vr(s, types.Int), intLit(1)))
		if v := evalOK(t, env, blk); v.String() != "2" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("ignore discards", func(t *testing.T) {
		x := block(types.Int,
			&check.Ignore{Base: b(types.Unit), X: intLit(9)},
			intLit(4))
		if v := evalOK(t, env, x); v.String() != "4" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("string interpolation", func(t *testing.T) {
		p, s := vpat("name", types.String)
		x := block(types.String,
			let(p, strLit("world")),
			&check.StrInterp{Base: b(types.String), Parts: []check.StrPart{
				&check.StrText{Sp: sp, Text: "hello "},
				&check.StrExpr{Sp: sp, X: vr(s, types.String)},
				&check.StrText{Sp: sp, Text: "!"},
			}})
		v := evalOK(t, env, x)
		if v.String() != `"hello world!"` {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("for over a list", func(t *testing.T) {
		tr := newTapper()
		env := tapEnv(tr)
		p, s := vpat("x", types.String)
		_ = s
		x := &check.For{Base: b(types.Unit), Pat: p,
			Iter: &check.ListLit{Base: b(types.List(types.String)),
				Elems: []check.Expr{strLit("a"), strLit("b")}},
			Body: block(types.Unit, &check.Ignore{Base: b(types.Unit), X: tr.call("iter")})}
		v := evalOK(t, env, x)
		if v.String() != "()" {
			t.Fatalf("got %s", v)
		}
		assertTrace(t, tr.calls, []string{"iter", "iter"})
	})

	t.Run("pipe and back-pipe", func(t *testing.T) {
		inc := eval.NewHostFunc("inc", 1, func(args []eval.Value) (eval.Value, error) {
			return &eval.VInt{N: args[0].(*eval.VInt).N + 1}, nil
		})
		incSym := sym("inc", resolve.KindFunc)
		env := eval.NewEnv()
		env.Bind(incSym, inc)
		incVar := vr(incSym, types.Fn(types.Int, types.P(types.Int)))
		fwd := bin(syntax.BinPipeFwd, types.Int, intLit(1), incVar)
		back := bin(syntax.BinPipeBack, types.Int, incVar, intLit(1))
		if v := evalOK(t, env, fwd); v.String() != "2" {
			t.Fatalf("|> got %s", v)
		}
		if v := evalOK(t, env, back); v.String() != "2" {
			t.Fatalf("<| got %s", v)
		}
	})
}

// --- whole-file evaluation ---

func TestProgram(t *testing.T) {
	t.Run("recursive fn and main", func(t *testing.T) {
		factSym := sym("fact", resolve.KindFunc)
		nSym := sym("n", resolve.KindVar)
		sig := types.Fn(types.Int, types.P(types.Int))
		fact := &check.FnDecl{Sp: sp, Sym: factSym, Sig: sig,
			Params: []check.Param{{Pat: &check.VarPat{Base: b(types.Int), Sym: nSym}}},
			Body: &check.If{Base: b(types.Int),
				Cond: bin(syntax.BinLe, types.Bool, vr(nSym, types.Int), intLit(1)),
				Then: intLit(1),
				Else: bin(syntax.BinMul, types.Int, vr(nSym, types.Int),
					directCall(types.Int, factSym, sig,
						bin(syntax.BinSub, types.Int, vr(nSym, types.Int), intLit(1))))}}
		uPat, _ := vpat("u", types.Unit)
		mainSym := sym("main", resolve.KindFunc)
		rPat, _ := vpat("r", types.Int)
		mainDecl := &check.FnDecl{Sp: sp, Sym: mainSym,
			Sig:    types.Fn(types.Unit, types.P(types.Unit)),
			Params: []check.Param{{Pat: uPat}},
			Body: block(types.Unit,
				let(rPat, directCall(types.Int, factSym, sig, intLit(5))),
				unitLit())}
		f := &check.File{Sp: sp, Package: "main", Decls: []check.Decl{fact, mainDecl}}
		r := eval.Program(f)
		if !r.OK() {
			t.Fatalf("program failed: %+v", r.Diags)
		}
		if r.Value.String() != "()" {
			t.Fatalf("got %s", r.Value)
		}
	})

	t.Run("mutual recursion between top-level fns", func(t *testing.T) {
		// isEven / isOdd call each other — both are bound by New
		// before any body runs.
		evenSym := sym("isEven", resolve.KindFunc)
		oddSym := sym("isOdd", resolve.KindFunc)
		sig := types.Fn(types.Bool, types.P(types.Int))
		nP, nS := vpat("n", types.Int)
		nP2, nS2 := vpat("n", types.Int)
		even := &check.FnDecl{Sp: sp, Sym: evenSym, Sig: sig,
			Params: []check.Param{{Pat: nP}},
			Body: &check.If{Base: b(types.Bool),
				Cond: bin(syntax.BinEq, types.Bool, vr(nS, types.Int), intLit(0)),
				Then: boolLit(true),
				Else: directCall(types.Bool, oddSym, sig,
					bin(syntax.BinSub, types.Int, vr(nS, types.Int), intLit(1)))}}
		odd := &check.FnDecl{Sp: sp, Sym: oddSym, Sig: sig,
			Params: []check.Param{{Pat: nP2}},
			Body: &check.If{Base: b(types.Bool),
				Cond: bin(syntax.BinEq, types.Bool, vr(nS2, types.Int), intLit(0)),
				Then: boolLit(false),
				Else: directCall(types.Bool, evenSym, sig,
					bin(syntax.BinSub, types.Int, vr(nS2, types.Int), intLit(1)))}}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{even, odd}})
		if r := ev.Expr(directCall(types.Bool, evenSym, sig, intLit(10))); !r.OK() || r.Value.String() != "true" {
			t.Fatalf("isEven 10 = %v diags=%v", r.Value, r.Diags)
		}
		if r := ev.Expr(directCall(types.Bool, oddSym, sig, intLit(10))); !r.OK() || r.Value.String() != "false" {
			t.Fatalf("isOdd 10 = %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("let decls then main", func(t *testing.T) {
		p, s := vpat("x", types.Int)
		letD := &check.LetDecl{Sp: sp, Pat: p, Value: intLit(40)}
		uPat, _ := vpat("u", types.Unit)
		mainSym := sym("main", resolve.KindFunc)
		mainD := &check.FnDecl{Sp: sp, Sym: mainSym,
			Sig:    types.Fn(types.Int, types.P(types.Unit)),
			Params: []check.Param{{Pat: uPat}},
			Body:   bin(syntax.BinAdd, types.Int, vr(s, types.Int), intLit(2))}
		r := eval.Program(&check.File{Sp: sp, Decls: []check.Decl{letD, mainD}})
		if !r.OK() || r.Value.String() != "42" {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("top-level let reads a later let", func(t *testing.T) {
		// `let x = y + 1; let y = 40` compiles to package vars and
		// initializes y first (x = 41); the evaluator forces the
		// dependency on first read instead of walking source order.
		xP, xS := vpat("x", types.Int)
		yP, yS := vpat("y", types.Int)
		letX := &check.LetDecl{Sp: sp, Pat: xP,
			Value: bin(syntax.BinAdd, types.Int, vr(yS, types.Int), intLit(1))}
		letY := &check.LetDecl{Sp: sp, Pat: yP, Value: intLit(40)}
		uPat, _ := vpat("u", types.Unit)
		mainSym := sym("main", resolve.KindFunc)
		mainD := &check.FnDecl{Sp: sp, Sym: mainSym,
			Sig:    types.Fn(types.Int, types.P(types.Unit)),
			Params: []check.Param{{Pat: uPat}},
			Body:   vr(xS, types.Int)}
		r := eval.Program(&check.File{Sp: sp, Decls: []check.Decl{letX, letY, mainD}})
		if !r.OK() || r.Value.String() != "41" {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})

	t.Run("top-level let initialization cycle", func(t *testing.T) {
		// A compiled cycle would be a Go init-cycle error or panic;
		// the evaluator reports the re-entrant read instead of
		// recursing forever.
		xP, xS := vpat("x", types.Int)
		yP, yS := vpat("y", types.Int)
		letX := &check.LetDecl{Sp: sp, Pat: xP, Value: vr(yS, types.Int)}
		letY := &check.LetDecl{Sp: sp, Pat: yP,
			Value: bin(syntax.BinAdd, types.Int, vr(xS, types.Int), intLit(1))}
		r := eval.Program(&check.File{Sp: sp, Decls: []check.Decl{letX, letY}})
		if r.OK() {
			t.Fatal("expected initialization cycle diagnostic")
		}
		if r.Diags[0].Code != eval.CodePanic {
			t.Fatalf("code = %s, want %s", r.Diags[0].Code, eval.CodePanic)
		}
	})

	t.Run("test decl that returns Err fails the program", func(t *testing.T) {
		resT := types.Result(types.Unit, types.GoError)
		errV := &eval.VVariant{Type: resT.(*types.Con), Name: "Err",
			Fields: []eval.VField{{Index: 0, V: &eval.VGoErr{Text: "bad"}}}}
		eSym := sym("e", resolve.KindVar)
		testD := &check.TestDecl{Sp: sp, Name: "t1",
			Body: block(resT, vr(eSym, resT))}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{testD}})
		ev.Env().Bind(eSym, errV)
		r := ev.Run()
		if r.OK() {
			t.Fatal("expected test failure")
		}
		if r.Diags[0].Code != eval.CodeTestFail {
			t.Fatalf("code = %s, want %s", r.Diags[0].Code, eval.CodeTestFail)
		}
	})
}

// divergingFile is `fn f (x: Int) -> Int = f x` applied from main —
// unbounded recursion with no base case. Before the call-depth
// budget existed this died on the ~1 GB goroutine stack ceiling
// (~350K nested calls), an unrecoverable kill that no diagnostic
// survives.
func divergingFile() *check.File {
	fSym := sym("f", resolve.KindFunc)
	xP, xS := vpat("x", types.Int)
	sig := types.Fn(types.Int, types.P(types.Int))
	f := &check.FnDecl{Sp: sp, Sym: fSym, Sig: sig,
		Params: []check.Param{{Pat: xP}},
		Body:   directCall(types.Int, fSym, sig, vr(xS, types.Int))}
	uP, _ := vpat("u", types.Unit)
	rP, rS := vpat("r", types.Int)
	mainSym := sym("main", resolve.KindFunc)
	mainD := &check.FnDecl{Sp: sp, Sym: mainSym,
		Sig:    types.Fn(types.Int, types.P(types.Unit)),
		Params: []check.Param{{Pat: uP}},
		Body: block(types.Int,
			let(rP, directCall(types.Int, fSym, sig, intLit(0))),
			vr(rS, types.Int))}
	return &check.File{Sp: sp, Decls: []check.Decl{f, mainD}}
}

func TestCallDepthBudget(t *testing.T) {
	t.Run("unbounded recursion trips E9006 by default", func(t *testing.T) {
		// The verdict must be an ordinary diagnostic — not a dead
		// test binary.
		r := eval.Program(divergingFile())
		if r.OK() {
			t.Fatalf("diverging program returned %s", r.Value)
		}
		if r.Diags[0].Code != eval.CodeBudget {
			t.Fatalf("code = %s, want %s", r.Diags[0].Code, eval.CodeBudget)
		}
	})

	t.Run("SetMaxDepth tunes the bound", func(t *testing.T) {
		ev := eval.New(divergingFile())
		ev.SetMaxDepth(64)
		r := ev.Run()
		if r.OK() || r.Diags[0].Code != eval.CodeBudget {
			t.Fatalf("got value=%v diags=%v, want E9006", r.Value, r.Diags)
		}
	})

	t.Run("recursion under the budget still evaluates", func(t *testing.T) {
		// A self-recursive fn that terminates well inside the
		// default depth budget must keep working — the budget only
		// guards runaway depth.
		fSym := sym("count", resolve.KindFunc)
		nP, nS := vpat("n", types.Int)
		sig := types.Fn(types.Int, types.P(types.Int))
		count := &check.FnDecl{Sp: sp, Sym: fSym, Sig: sig,
			Params: []check.Param{{Pat: nP}},
			Body: &check.If{Base: b(types.Int),
				Cond: bin(syntax.BinLe, types.Bool, vr(nS, types.Int), intLit(0)),
				Then: intLit(0),
				Else: bin(syntax.BinAdd, types.Int, intLit(1),
					directCall(types.Int, fSym, sig,
						bin(syntax.BinSub, types.Int, vr(nS, types.Int), intLit(1))))}}
		uP, _ := vpat("u", types.Unit)
		mainSym := sym("main", resolve.KindFunc)
		mainD := &check.FnDecl{Sp: sp, Sym: mainSym,
			Sig:    types.Fn(types.Int, types.P(types.Unit)),
			Params: []check.Param{{Pat: uP}},
			Body:   directCall(types.Int, fSym, sig, intLit(500))}
		r := eval.Program(&check.File{Sp: sp, Decls: []check.Decl{count, mainD}})
		if !r.OK() || r.Value.String() != "500" {
			t.Fatalf("got %v diags=%v", r.Value, r.Diags)
		}
	})
}

// --- error paths ---

func TestErrorPaths(t *testing.T) {
	t.Run("todo panics", func(t *testing.T) {
		todoSym := sym("todo", resolve.KindBuiltin)
		x := &check.Apply{Base: b(types.Unit),
			Fn:   vr(todoSym, types.Fn(types.Unit, types.P(types.Unit))),
			Args: []check.Arg{arg(unitLit())},
			Call: check.CallInfo{Kind: check.CallDirect, Callee: todoSym, Arity: 1, Applied: 1, HoleIndex: -1}}
		d := evalFail(t, nil, x)
		if d.Code != eval.CodePanic {
			t.Fatalf("code = %s", d.Code)
		}
	})

	t.Run("assert_eq failure", func(t *testing.T) {
		aeSym := sym("assert_eq", resolve.KindBuiltin)
		x := &check.Apply{Base: b(types.Unit),
			Fn:   vr(aeSym, types.Fn(types.Unit, types.P(types.Int), types.P(types.Int))),
			Args: []check.Arg{arg(intLit(1)), arg(intLit(2))},
			Call: check.CallInfo{Kind: check.CallDirect, Callee: aeSym, Arity: 2, Applied: 2, HoleIndex: -1}}
		d := evalFail(t, nil, x)
		if d.Code != eval.CodePanic {
			t.Fatalf("code = %s", d.Code)
		}
		if d.Message == "" {
			t.Fatal("empty message")
		}
	})

	t.Run("go expr unsupported", func(t *testing.T) {
		x := &check.GoExpr{Base: b(types.Unit), Body: block(types.Unit, unitLit())}
		d := evalFail(t, nil, x)
		if d.Code != eval.CodeUnsupported {
			t.Fatalf("code = %s", d.Code)
		}
	})

	t.Run("select unsupported", func(t *testing.T) {
		x := &check.SelectExpr{Base: b(types.Int), Arms: []check.SelectArm{
			&check.DefaultArm{Sp: sp, Body: intLit(1)}}}
		d := evalFail(t, nil, x)
		if d.Code != eval.CodeUnsupported {
			t.Fatalf("code = %s", d.Code)
		}
	})

	// A host function can hand the evaluator a real channel as a
	// VHost — but `for` iterates List values only; channel iteration
	// is compiled-only like `go`/`select` (the concurrency decision
	// in docs/decisions/2026-11-04-eval-concurrency-compiled-only.md).
	t.Run("for over a host channel unsupported", func(t *testing.T) {
		goSym := &resolve.Symbol{Name: "makeChan", Kind: resolve.KindFunc, IsGo: true, Package: "rt"}
		env := eval.NewEnv()
		env.Bind(goSym, eval.NewHostFunc("rt.makeChan", 0, func(args []eval.Value) (eval.Value, error) {
			ch := make(chan int64, 1)
			ch <- 1
			close(ch)
			return &eval.VHost{V: ch}, nil
		}))
		member := &check.MemberExpr{Base: b(types.Fn(types.Chan(types.Int))),
			Recv: vr(sym("rt", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "makeChan"}, Sym: goSym, Index: -1}
		call := &check.Apply{Base: b(types.Chan(types.Int)), Fn: member,
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 0, Applied: 0, HoleIndex: -1}}
		p, _ := vpat("x", types.Int)
		x := &check.For{Base: b(types.Unit), Pat: p, Iter: call,
			Body: block(types.Unit, unitLit())}
		d := evalFail(t, env, x)
		if d.Code != eval.CodeUnsupported {
			t.Fatalf("code = %s, want %s", d.Code, eval.CodeUnsupported)
		}
	})

	t.Run("unbound Go call unsupported", func(t *testing.T) {
		resT := types.Result(types.Int, types.GoError)
		goSym := &resolve.Symbol{Name: "Atoi", Kind: resolve.KindFunc, IsGo: true, Package: "strconv"}
		member := &check.MemberExpr{Base: b(types.Fn(resT, types.P(types.String))),
			Recv: vr(sym("strconv", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Atoi"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(resT), Fn: member, Args: []check.Arg{arg(strLit("1"))},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 1, Applied: 1, HoleIndex: -1}}
		d := evalFail(t, nil, x)
		if d.Code != eval.CodeUnsupported {
			t.Fatalf("code = %s, want %s", d.Code, eval.CodeUnsupported)
		}
	})

	t.Run("stubbed Go call works", func(t *testing.T) {
		resT := types.Result(types.Int, types.GoError)
		goSym := &resolve.Symbol{Name: "Atoi", Kind: resolve.KindFunc, IsGo: true, Package: "strconv"}
		member := &check.MemberExpr{Base: b(types.Fn(resT, types.P(types.String))),
			Recv: vr(sym("strconv", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Atoi"}, Sym: goSym, Index: -1}
		env := eval.NewEnv()
		env.Bind(goSym, eval.NewHostFunc("strconv.Atoi", 1, func(args []eval.Value) (eval.Value, error) {
			n, err := strconv.Atoi(args[0].(*eval.VStr).S)
			if err != nil {
				return &eval.VVariant{Type: resT.(*types.Con), Name: "Err",
					Fields: []eval.VField{{Index: 0, V: &eval.VGoErr{Text: err.Error()}}}}, nil
			}
			return &eval.VVariant{Type: resT.(*types.Con), Name: "Ok",
				Fields: []eval.VField{{Index: 0, V: &eval.VInt{N: int64(n)}}}}, nil
		}))
		x := &check.Apply{Base: b(resT), Fn: member, Args: []check.Arg{arg(strLit("41"))},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 1, Applied: 1, HoleIndex: -1}}
		v := evalOK(t, env, x)
		if v.String() != "Ok 41" {
			t.Fatalf("got %s", v)
		}
	})

	t.Run("cute placeholder builds a function", func(t *testing.T) {
		goSym := &resolve.Symbol{Name: "Repeat", Kind: resolve.KindFunc, IsGo: true, Package: "strings"}
		env := eval.NewEnv()
		var calls []string
		env.Bind(goSym, eval.NewHostFunc("strings.Repeat", 2, func(args []eval.Value) (eval.Value, error) {
			calls = append(calls, fmt.Sprintf("%s x%d", args[0].(*eval.VStr).S, args[1].(*eval.VInt).N))
			return args[0], nil
		}))
		member := &check.MemberExpr{Base: b(types.Fn(types.String, types.P(types.String), types.P(types.Int))),
			Recv: vr(sym("strings", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Repeat"}, Sym: goSym, Index: -1}
		// strings.Repeat(_, 3)
		x := &check.Apply{Base: b(types.Fn(types.String, types.P(types.String))), Fn: member,
			Args: []check.Arg{{Value: &check.Hole{Base: b(types.String)}}, arg(intLit(3))},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 2, Applied: 2, HoleIndex: 0}}
		r := eval.Expr(env, x)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		f, ok := r.Value.(*eval.VFunc)
		if !ok {
			t.Fatalf("got %s, want fn", r.Value)
		}
		fSym := sym("holef", resolve.KindVar)
		env.Bind(fSym, f)
		done := eval.Expr(env, unknownCall(types.String, vr(fSym, types.Unit), 1, strLit("ab")))
		if !done.OK() || done.Value.String() != `"ab"` {
			t.Fatalf("got %v diags=%v", done.Value, done.Diags)
		}
		if len(calls) != 1 || calls[0] != "ab x3" {
			t.Fatalf("calls = %v", calls)
		}
	})

	t.Run("hole position disagreeing with HoleIndex is an invariant error", func(t *testing.T) {
		goSym := &resolve.Symbol{Name: "Repeat", Kind: resolve.KindFunc, IsGo: true, Package: "strings"}
		env := eval.NewEnv()
		env.Bind(goSym, eval.NewHostFunc("strings.Repeat", 2, func(args []eval.Value) (eval.Value, error) {
			return args[0], nil
		}))
		member := &check.MemberExpr{Base: b(types.Fn(types.String, types.P(types.String), types.P(types.Int))),
			Recv: vr(sym("strings", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Repeat"}, Sym: goSym, Index: -1}
		// The hole sits at Args[1] but CallInfo claims index 0.
		x := &check.Apply{Base: b(types.Fn(types.String, types.P(types.Int))), Fn: member,
			Args: []check.Arg{arg(strLit("ab")), {Value: &check.Hole{Base: b(types.Int)}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 2, Applied: 2, HoleIndex: 0}}
		d := evalFail(t, env, x)
		if d.Code != eval.CodeInternal {
			t.Fatalf("code = %s, want %s", d.Code, eval.CodeInternal)
		}
	})

	t.Run("CallGo labeled args reach the stub in declaration order", func(t *testing.T) {
		// io.Copy(dst, src) written ~src: s ~dst: d — ArgToParam maps
		// written order back to declaration order, like lowering's
		// declOrderArgs.
		goSym := &resolve.Symbol{Name: "Copy", Kind: resolve.KindFunc, IsGo: true, Package: "io"}
		env := eval.NewEnv()
		var got []string
		env.Bind(goSym, eval.NewHostFunc("io.Copy", 2, func(args []eval.Value) (eval.Value, error) {
			got = []string{args[0].(*eval.VStr).S, args[1].(*eval.VStr).S}
			return &eval.VUnit{}, nil
		}))
		member := &check.MemberExpr{Base: b(types.Fn(types.Unit, types.P(types.String), types.P(types.String))),
			Recv: vr(sym("io", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Copy"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(types.Unit), Fn: member,
			Args: []check.Arg{larg("src", strLit("s")), larg("dst", strLit("d"))},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 2, Applied: 2,
				ArgToParam: []int{1, 0}, HoleIndex: -1}}
		evalOK(t, env, x)
		if len(got) != 2 || got[0] != "d" || got[1] != "s" {
			t.Fatalf("host args = %v, want [d s]", got)
		}
	})

	t.Run("cute partial keeps the hole's declaration slot", func(t *testing.T) {
		// swap(~b: "B", ~a: _) — the hole is written second but fills
		// parameter 0; the captured "B" belongs to parameter 1.
		goSym := &resolve.Symbol{Name: "swap", Kind: resolve.KindFunc, IsGo: true, Package: "p"}
		env := eval.NewEnv()
		var got []string
		env.Bind(goSym, eval.NewHostFunc("p.swap", 2, func(args []eval.Value) (eval.Value, error) {
			got = []string{args[0].(*eval.VStr).S, args[1].(*eval.VStr).S}
			return &eval.VUnit{}, nil
		}))
		member := &check.MemberExpr{Base: b(types.Fn(types.Unit, types.LP("a", types.String), types.LP("b", types.String))),
			Recv: vr(sym("p", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "swap"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(types.Fn(types.Unit, types.P(types.String))), Fn: member,
			Args: []check.Arg{larg("b", strLit("B")), {Value: &check.Hole{Base: b(types.String)}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 2, Applied: 2,
				ArgToParam: []int{1, 0}, HoleIndex: 1}}
		r := eval.Expr(env, x)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		fSym := sym("holef2", resolve.KindVar)
		env.Bind(fSym, r.Value)
		done := eval.Expr(env, unknownCall(types.Unit, vr(fSym, types.Unit), 1, strLit("A")))
		if !done.OK() {
			t.Fatalf("apply failed: %+v", done.Diags)
		}
		if len(got) != 2 || got[0] != "A" || got[1] != "B" {
			t.Fatalf("host args = %v, want [A B]", got)
		}
	})

	t.Run("CallGo ..e spread splices a List into the variadic tail", func(t *testing.T) {
		// fmt.Println("tag", ..xs) — the host stub sees the flat
		// arguments `f("tag", xs...)` produces in Go: ["tag", x0, x1].
		goSym := &resolve.Symbol{Name: "Println", Kind: resolve.KindFunc, IsGo: true, Package: "fmt"}
		env := eval.NewEnv()
		var got []string
		env.Bind(goSym, eval.NewHostFunc("fmt.Println", 3, func(args []eval.Value) (eval.Value, error) {
			for _, a := range args {
				s, ok := a.(*eval.VStr)
				if !ok {
					return nil, fmt.Errorf("non-string arg %s", a)
				}
				got = append(got, s.S)
			}
			return &eval.VUnit{}, nil
		}))
		listT := types.List(types.String)
		xs := &check.ListLit{Base: b(listT), Elems: []check.Expr{strLit("x"), strLit("y")}}
		member := &check.MemberExpr{
			Base: b(types.Fn(types.Unit, types.P(types.String), types.P(types.GoSlice(types.GoAny)))),
			Recv: vr(sym("fmt", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Println"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(types.Unit), Fn: member,
			Args: []check.Arg{arg(strLit("tag")),
				{Value: &check.Spread{Base: b(listT), X: xs}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 2, Applied: 2,
				ArgToParam: []int{0, 1}, HoleIndex: -1, Variadic: true}}
		evalOK(t, env, x)
		if strings.Join(got, ",") != "tag,x,y" {
			t.Fatalf("host args = %v, want [tag x y]", got)
		}
	})

	t.Run("CallGo ..e spread alone and empty", func(t *testing.T) {
		// fmt.Println(..[]) splices nothing — the stub sees no args.
		goSym := &resolve.Symbol{Name: "Println", Kind: resolve.KindFunc, IsGo: true, Package: "fmt"}
		env := eval.NewEnv()
		nargs := -1
		env.Bind(goSym, eval.NewHostFunc("fmt.Println", 1, func(args []eval.Value) (eval.Value, error) {
			nargs = len(args)
			return &eval.VUnit{}, nil
		}))
		listT := types.List(types.String)
		member := &check.MemberExpr{
			Base: b(types.Fn(types.Unit, types.P(types.GoSlice(types.GoAny)))),
			Recv: vr(sym("fmt", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Println"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(types.Unit), Fn: member,
			Args: []check.Arg{{Value: &check.Spread{Base: b(listT),
				X: &check.ListLit{Base: b(listT)}}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 1, Applied: 1,
				ArgToParam: []int{0}, HoleIndex: -1, Variadic: true}}
		evalOK(t, env, x)
		if nargs != 0 {
			t.Fatalf("host arg count = %d, want 0", nargs)
		}
	})

	t.Run("CallGo ..e spread of a host Go slice splices host elements", func(t *testing.T) {
		// A Go.Slice arriving through a host binding is a VHost; its
		// elements reach the variadic callee as further host values.
		goSym := &resolve.Symbol{Name: "Println", Kind: resolve.KindFunc, IsGo: true, Package: "fmt"}
		env := eval.NewEnv()
		var got []string
		env.Bind(goSym, eval.NewHostFunc("fmt.Println", 1, func(args []eval.Value) (eval.Value, error) {
			for _, a := range args {
				h, ok := a.(*eval.VHost)
				if !ok {
					return nil, fmt.Errorf("non-host arg %s", a)
				}
				got = append(got, h.V.(string))
			}
			return &eval.VUnit{}, nil
		}))
		sliceT := types.GoSlice(types.GoAny)
		xsSym := sym("goslice", resolve.KindVar)
		env.Bind(xsSym, &eval.VHost{V: []any{"a", "b"}})
		member := &check.MemberExpr{
			Base: b(types.Fn(types.Unit, types.P(sliceT))),
			Recv: vr(sym("fmt", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Println"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(types.Unit), Fn: member,
			Args: []check.Arg{{Value: &check.Spread{Base: b(sliceT), X: vr(xsSym, sliceT)}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 1, Applied: 1,
				ArgToParam: []int{0}, HoleIndex: -1, Variadic: true}}
		evalOK(t, env, x)
		if strings.Join(got, ",") != "a,b" {
			t.Fatalf("host args = %v, want [a b]", got)
		}
	})

	t.Run("CallGo cute placeholder with ..e spread", func(t *testing.T) {
		// fmt.Println(_, ..xs) — the captured call holds the spliced
		// tail; supplying the hole runs the flat invocation.
		goSym := &resolve.Symbol{Name: "Println", Kind: resolve.KindFunc, IsGo: true, Package: "fmt"}
		env := eval.NewEnv()
		var got []string
		env.Bind(goSym, eval.NewHostFunc("fmt.Println", 3, func(args []eval.Value) (eval.Value, error) {
			for _, a := range args {
				got = append(got, a.(*eval.VStr).S)
			}
			return &eval.VUnit{}, nil
		}))
		listT := types.List(types.String)
		xs := &check.ListLit{Base: b(listT), Elems: []check.Expr{strLit("x"), strLit("y")}}
		member := &check.MemberExpr{
			Base: b(types.Fn(types.Unit, types.P(types.String), types.P(types.GoSlice(types.GoAny)))),
			Recv: vr(sym("fmt", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Println"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(types.Fn(types.Unit, types.P(types.String))), Fn: member,
			Args: []check.Arg{{Value: &check.Hole{Base: b(types.String)}},
				{Value: &check.Spread{Base: b(listT), X: xs}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 2, Applied: 2,
				ArgToParam: []int{0, 1}, HoleIndex: 0, Variadic: true}}
		r := eval.Expr(env, x)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		fSym := sym("spreadhole", resolve.KindVar)
		env.Bind(fSym, r.Value)
		done := eval.Expr(env, unknownCall(types.Unit, vr(fSym, types.Unit), 1, strLit("tag")))
		if !done.OK() {
			t.Fatalf("apply failed: %+v", done.Diags)
		}
		if strings.Join(got, ",") != "tag,x,y" {
			t.Fatalf("host args = %v, want [tag x y]", got)
		}
	})

	t.Run("CallGo ..e spread of a non-collection is an invariant error", func(t *testing.T) {
		goSym := &resolve.Symbol{Name: "Println", Kind: resolve.KindFunc, IsGo: true, Package: "fmt"}
		env := eval.NewEnv()
		env.Bind(goSym, eval.NewHostFunc("fmt.Println", 1, func(args []eval.Value) (eval.Value, error) {
			return &eval.VUnit{}, nil
		}))
		member := &check.MemberExpr{
			Base: b(types.Fn(types.Unit, types.P(types.GoSlice(types.GoAny)))),
			Recv: vr(sym("fmt", resolve.KindModule), types.Unit),
			Sel:  &syntax.Ident{Sp: sp, Name: "Println"}, Sym: goSym, Index: -1}
		x := &check.Apply{Base: b(types.Unit), Fn: member,
			Args: []check.Arg{{Value: &check.Spread{Base: b(types.Int), X: intLit(1)}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: goSym, Arity: 1, Applied: 1,
				ArgToParam: []int{0}, HoleIndex: -1, Variadic: true}}
		d := evalFail(t, env, x)
		if d.Code != eval.CodeInternal {
			t.Fatalf("code = %s, want %s", d.Code, eval.CodeInternal)
		}
	})
}

func TestGoMethods(t *testing.T) {
	bufT := &types.Go{Ref: types.NamedRef{Path: "bytes", Name: "Buffer"}}

	t.Run("bound method call", func(t *testing.T) {
		recvSym := sym("buf", resolve.KindVar)
		msym := &resolve.Symbol{Name: "M", Kind: resolve.KindMethod, IsGo: true, Package: "bytes"}
		env := eval.NewEnv()
		env.Bind(recvSym, &eval.VHost{V: "RECV"})
		var got []string
		env.Bind(msym, eval.NewHostFunc("bytes.Buffer.M", 3, func(args []eval.Value) (eval.Value, error) {
			got = []string{
				args[0].(*eval.VHost).V.(string),
				args[1].(*eval.VStr).S,
				strconv.FormatInt(args[2].(*eval.VInt).N, 10),
			}
			return &eval.VUnit{}, nil
		}))
		mSig := types.Fn(types.Unit, types.P(types.String), types.P(types.Int))
		member := &check.MemberExpr{Base: b(mSig), Recv: vr(recvSym, bufT),
			Sel: &syntax.Ident{Sp: sp, Name: "M"}, Sym: msym, Index: -1}
		x := &check.Apply{Base: b(types.Unit), Fn: member,
			Args: []check.Arg{arg(strLit("a")), arg(intLit(2))},
			Call: check.CallInfo{Kind: check.CallGo, Callee: msym, Arity: 2, Applied: 2, HoleIndex: -1}}
		evalOK(t, env, x)
		if len(got) != 3 || got[0] != "RECV" || got[1] != "a" || got[2] != "2" {
			t.Fatalf("host args = %v, want [RECV a 2]", got)
		}
	})

	t.Run("bound method placeholder", func(t *testing.T) {
		recvSym := sym("buf", resolve.KindVar)
		msym := &resolve.Symbol{Name: "M", Kind: resolve.KindMethod, IsGo: true, Package: "bytes"}
		env := eval.NewEnv()
		env.Bind(recvSym, &eval.VHost{V: "RECV"})
		var got []string
		env.Bind(msym, eval.NewHostFunc("bytes.Buffer.M", 3, func(args []eval.Value) (eval.Value, error) {
			got = []string{
				args[0].(*eval.VHost).V.(string),
				args[1].(*eval.VStr).S,
				strconv.FormatInt(args[2].(*eval.VInt).N, 10),
			}
			return &eval.VUnit{}, nil
		}))
		mSig := types.Fn(types.Unit, types.P(types.String), types.P(types.Int))
		member := &check.MemberExpr{Base: b(mSig), Recv: vr(recvSym, bufT),
			Sel: &syntax.Ident{Sp: sp, Name: "M"}, Sym: msym, Index: -1}
		// buf.M(_, 7) : String -> Unit
		x := &check.Apply{Base: b(types.Fn(types.Unit, types.P(types.String))), Fn: member,
			Args: []check.Arg{{Value: &check.Hole{Base: b(types.String)}}, arg(intLit(7))},
			Call: check.CallInfo{Kind: check.CallGo, Callee: msym, Arity: 2, Applied: 2, HoleIndex: 0}}
		r := eval.Expr(env, x)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		f, ok := r.Value.(*eval.VFunc)
		if !ok {
			t.Fatalf("got %s, want fn", r.Value)
		}
		fSym := sym("partial", resolve.KindVar)
		env.Bind(fSym, f)
		done := eval.Expr(env, unknownCall(types.Unit, vr(fSym, types.Unit), 1, strLit("z")))
		if !done.OK() {
			t.Fatalf("apply failed: %+v", done.Diags)
		}
		if len(got) != 3 || got[0] != "RECV" || got[1] != "z" || got[2] != "7" {
			t.Fatalf("host args = %v, want [RECV z 7]", got)
		}
	})

	t.Run("unbound method unsupported", func(t *testing.T) {
		recvSym := sym("buf", resolve.KindVar)
		msym := &resolve.Symbol{Name: "M", Kind: resolve.KindMethod, IsGo: true, Package: "bytes"}
		env := eval.NewEnv()
		env.Bind(recvSym, &eval.VHost{V: "RECV"})
		mSig := types.Fn(types.Unit, types.P(types.String))
		member := &check.MemberExpr{Base: b(mSig), Recv: vr(recvSym, bufT),
			Sel: &syntax.Ident{Sp: sp, Name: "M"}, Sym: msym, Index: -1}
		x := &check.Apply{Base: b(types.Unit), Fn: member,
			Args: []check.Arg{arg(strLit("a"))},
			Call: check.CallInfo{Kind: check.CallGo, Callee: msym, Arity: 1, Applied: 1, HoleIndex: -1}}
		d := evalFail(t, env, x)
		if d.Code != eval.CodeUnsupported {
			t.Fatalf("code = %s, want %s", d.Code, eval.CodeUnsupported)
		}
	})

	t.Run("cute arguments evaluate once", func(t *testing.T) {
		recvSym := sym("buf", resolve.KindVar)
		msym := &resolve.Symbol{Name: "M", Kind: resolve.KindMethod, IsGo: true, Package: "bytes"}
		tapSym := sym("tap", resolve.KindFunc)
		env := eval.NewEnv()
		env.Bind(recvSym, &eval.VHost{V: "RECV"})
		var taps, calls int
		env.Bind(tapSym, eval.NewHostFunc("tap", 1, func(args []eval.Value) (eval.Value, error) {
			taps++
			return args[0], nil
		}))
		env.Bind(msym, eval.NewHostFunc("bytes.Buffer.M", 3, func(args []eval.Value) (eval.Value, error) {
			calls++
			return &eval.VUnit{}, nil
		}))
		mSig := types.Fn(types.Unit, types.P(types.Int), types.P(types.String))
		member := &check.MemberExpr{Base: b(mSig), Recv: vr(recvSym, bufT),
			Sel: &syntax.Ident{Sp: sp, Name: "M"}, Sym: msym, Index: -1}
		// buf.M(tap 40, _) — the non-hole argument is evaluated at
		// partial creation, captured for every later application.
		tapCall := directCall(types.Int, tapSym, types.Fn(types.Int, types.P(types.Int)), intLit(40))
		x := &check.Apply{Base: b(types.Fn(types.Unit, types.P(types.String))), Fn: member,
			Args: []check.Arg{arg(tapCall), {Value: &check.Hole{Base: b(types.String)}}},
			Call: check.CallInfo{Kind: check.CallGo, Callee: msym, Arity: 2, Applied: 2, HoleIndex: 1}}
		r := eval.Expr(env, x)
		if !r.OK() {
			t.Fatalf("eval failed: %+v", r.Diags)
		}
		fSym := sym("partial2", resolve.KindVar)
		env.Bind(fSym, r.Value)
		for _, s := range []string{"x", "y"} {
			done := eval.Expr(env, unknownCall(types.Unit, vr(fSym, types.Unit), 1, strLit(s)))
			if !done.OK() {
				t.Fatalf("apply %q failed: %+v", s, done.Diags)
			}
		}
		if taps != 1 {
			t.Fatalf("captured argument evaluated %d times, want 1", taps)
		}
		if calls != 2 {
			t.Fatalf("method invoked %d times, want 2", calls)
		}
	})
}

func TestPipeNamelessStepLineIndex(t *testing.T) {
	resT := types.Result(types.Int, types.GoError)
	errSymV := sym("failure", resolve.KindVar)
	p, _ := vpat("n", types.Int)
	step := &check.Lambda{Base: b(types.Fn(resT, types.P(types.Int))),
		Params: []check.Param{{Pat: p}}, Body: vr(errSymV, resT)}
	x := bin(syntax.BinPipeTry, resT, intLit(1), step)
	ev := eval.New(&check.File{Sp: sp})
	ev.SetLineIndex(diag.NewLineIndex([]byte("// α\n// β\nstep")))
	ev.Env().Bind(errSymV, &eval.VVariant{Type: resT.(*types.Con), Name: "Err",
		Fields: []eval.VField{{Index: 0, V: &eval.VGoErr{Text: "bad"}}}})
	r := ev.Expr(x)
	if !r.OK() || r.Value.String() != `Err (error("test.gom:2: bad"))` {
		t.Fatalf("result = %v, diagnostics = %v", r.Value, r.Diags)
	}
}
