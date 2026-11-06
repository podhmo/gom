package eval_test

import (
	"fmt"
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/eval"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// The ready-frontier tests below pin Go's package-initialization
// pick (spec 5.1): every step initializes the earliest-declared
// top-level let whose dependencies have all initialized. A host
// `seq` function — seq(name, v) logs name and returns v — observes
// the order through initializer side effects, the only channel a
// bound value's evaluation order is visible through.
//
// Pure lazy forcing (what the frontier replaced) reaches the same
// bindings but orders independent vars by the forcing context: an
// earlier-declared `let a` that depends on a later `let b` drags b's
// effects ahead of a ready var declared between them.

// bindSeq binds seq to a host function logging its first argument's
// text on each call and returning its second argument.
func bindSeq(t *testing.T, ev *eval.Evaluator, seq *resolve.Symbol, log *[]string) {
	t.Helper()
	ev.Env().Bind(seq, eval.NewHostFunc("seq", 2, func(args []eval.Value) (eval.Value, error) {
		v, ok := args[0].(*eval.VStr)
		if !ok {
			return nil, fmt.Errorf("seq: arg0 = %s, want Str", args[0])
		}
		*log = append(*log, v.S)
		return args[1], nil
	}))
}

// seqSym is the host symbol every `seq` call shares.
func seqSym() *resolve.Symbol { return sym("seq", resolve.KindFunc) }

// seqCall is `seq "name" value` — evaluate value, log name, yield
// value. resT is the call's (and value's) type.
func seqCall(resT types.Type, seq *resolve.Symbol, name string, value check.Expr) *check.Apply {
	return unknownCall(resT,
		vr(seq, types.Fn(resT, types.P(types.String), types.P(resT))), 2,
		strLit(name), value)
}

// letDecl is `let name = value`.
func letDecl(name string, value check.Expr) (*check.LetDecl, *resolve.Symbol) {
	p, s := vpat(name, value.Type())
	return &check.LetDecl{Sp: sp, Pat: p, Value: value}, s
}

// runOK runs the evaluator to completion.
func runOK(t *testing.T, ev *eval.Evaluator) {
	t.Helper()
	if r := ev.Run(); !r.OK() {
		t.Fatalf("run failed: %+v", r.Diags)
	}
}

func TestInitReadyFrontier(t *testing.T) {
	spa := diag.SpanIn("a.gom", 0, 1)
	spb := diag.SpanIn("b.gom", 0, 1)

	t.Run("independent ready var between a var and its dependency", func(t *testing.T) {
		// `let a` depends on the later `let b`; `let c` declared
		// between them is ready at step one. Go picks c then b
		// then a; lazy forcing dragged b (inside a's read) ahead
		// of c.
		seq := seqSym()
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", intLit(1)))
		aD, _ := letDecl("a", seqCall(types.Int, seq, "a", vr(bS, types.Int)))
		cD, _ := letDecl("c", seqCall(types.Int, seq, "c", intLit(0)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, cD, bD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[c b a]" {
			t.Fatalf("init order = %v, want [c b a]", log)
		}
	})

	t.Run("dependency counted through a function body", func(t *testing.T) {
		// a's initializer calls `zed`, whose body reads b — the
		// transitive function-mediated reference Go's dependency
		// analysis (and the vardep port) counts. Same frontier as
		// the direct-read case: c, b, a.
		seq := seqSym()
		uP, _ := vpat("u", types.Unit)
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", intLit(1)))
		zed := &check.FnDecl{Sp: sp, Sym: sym("zed", resolve.KindFunc),
			Sig:    types.Fn(types.Int, types.P(types.Unit)),
			Params: []check.Param{{Pat: uP}},
			Body:   vr(bS, types.Int)}
		aD, _ := letDecl("a", seqCall(types.Int, seq, "a",
			directCall(types.Int, zed.Sym, zed.Sig, unitLit())))
		cD, _ := letDecl("c", seqCall(types.Int, seq, "c", intLit(0)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, cD, zed, bD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[c b a]" {
			t.Fatalf("init order = %v, want [c b a]", log)
		}
	})

	t.Run("dependency counted through a bare Var in value position", func(t *testing.T) {
		// `let a = seq "a" zed` — zed is a Var reference in value
		// position, not a call — still counts: a's initializer
		// references fn zed, whose body reads b, so a waits on b
		// exactly like the call form. Without the *check.Var
		// collection a would be ready at step one and print
		// [a c b].
		seq := seqSym()
		uP, _ := vpat("u", types.Unit)
		fnT := types.Fn(types.Int, types.P(types.Unit))
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", intLit(1)))
		zed := &check.FnDecl{Sp: sp, Sym: sym("zed", resolve.KindFunc),
			Sig:    fnT,
			Params: []check.Param{{Pat: uP}},
			Body:   vr(bS, types.Int)}
		aD, _ := letDecl("a", seqCall(fnT, seq, "a", vr(zed.Sym, fnT)))
		cD, _ := letDecl("c", seqCall(types.Int, seq, "c", intLit(0)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, cD, zed, bD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[c b a]" {
			t.Fatalf("init order = %v, want [c b a]", log)
		}
	})

	t.Run("dependency counted through a lambda literal", func(t *testing.T) {
		// a's initializer holds a function literal whose body
		// reads b — Go reads the initializer's references
		// lexically, so a still depends on b even though nothing
		// calls the function at init time.
		seq := seqSym()
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", intLit(1)))
		uP, _ := vpat("u", types.Unit)
		fnT := types.Fn(types.Int, types.P(types.Unit))
		lam := &check.Lambda{Base: b(fnT),
			Params: []check.Param{{Pat: uP}},
			Body:   vr(bS, types.Int)}
		aD, _ := letDecl("a", seqCall(fnT, seq, "a", lam))
		cD, _ := letDecl("c", seqCall(types.Int, seq, "c", intLit(0)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, cD, bD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[c b a]" {
			t.Fatalf("init order = %v, want [c b a]", log)
		}
	})

	t.Run("frontier pick is declaration order not reverse-dependency", func(t *testing.T) {
		// a -> b -> c with independent d last. c and d are ready
		// first; c precedes d by declaration. Once c and b have
		// run, a — declared first — beats d even though d was in
		// the ready set all along: [c b a d].
		seq := seqSym()
		cD, cS := letDecl("c", seqCall(types.Int, seq, "c", intLit(0)))
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", vr(cS, types.Int)))
		aD, _ := letDecl("a", seqCall(types.Int, seq, "a", vr(bS, types.Int)))
		dD, _ := letDecl("d", seqCall(types.Int, seq, "d", intLit(9)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, bD, cD, dD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[c b a d]" {
			t.Fatalf("init order = %v, want [c b a d]", log)
		}
	})

	t.Run("wildcard let occupies a frontier slot", func(t *testing.T) {
		// `let _ = e` binds nothing but still compiles to a
		// package var — the hidden temporary runs for effect at
		// its declaration position in the frontier.
		seq := seqSym()
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", intLit(1)))
		aD, _ := letDecl("a", seqCall(types.Int, seq, "a", vr(bS, types.Int)))
		wildD := &check.LetDecl{Sp: sp, Pat: wild(types.Int),
			Value: seqCall(types.Int, seq, "wild", intLit(0))}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, wildD, bD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[wild b a]" {
			t.Fatalf("init order = %v, want [wild b a]", log)
		}
	})

	t.Run("destructure let is one frontier node", func(t *testing.T) {
		// `let (p, q) = e` emits a shared temporary for e's value
		// plus pure projections — one effectful step. It is ready
		// at step one; a declared before it that depends on the
		// later b still comes last.
		seq := seqSym()
		pairT := types.Tup(types.Int, types.Int)
		pP, pS := vpat("p", types.Int)
		qP, qS := vpat("q", types.Int)
		pairD := &check.LetDecl{Sp: sp,
			Pat:   &check.TuplePat{Base: b(pairT), Elems: []check.Pat{pP, qP}},
			Value: seqCall(pairT, seq, "pair", &check.TupleLit{Base: b(pairT), Elems: []check.Expr{intLit(1), intLit(2)}})}
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", intLit(1)))
		aD, _ := letDecl("a", seqCall(types.Int, seq, "a", vr(bS, types.Int)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, pairD, bD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[pair b a]" {
			t.Fatalf("init order = %v, want [pair b a]", log)
		}
		// The bound symbols resolved through the same node: a
		// reader of either sees the projected values.
		if got := ev.Global("p").String() + ev.Global("q").String(); got != "12" {
			t.Fatalf("p,q = %v, want 12", got)
		}
		_ = pS
		_ = qS
	})

	t.Run("sibling files keep the frontier order", func(t *testing.T) {
		// The same shape spread across the unit: a in a.gom
		// depends on b in b.gom; c in b.gom is declared (in file
		// order) between them and initializes first.
		seq := seqSym()
		bD, bS := letDecl("b", seqCall(types.Int, seq, "b", intLit(1)))
		bD.Sp = spb
		aD, _ := letDecl("a", seqCall(types.Int, seq, "a", vr(bS, types.Int)))
		aD.Sp = spa
		cD, _ := letDecl("c", seqCall(types.Int, seq, "c", intLit(0)))
		cD.Sp = spb
		a := &check.File{Sp: spa, Package: "m", Decls: []check.Decl{aD}}
		bf := &check.File{Sp: spb, Package: "m", Decls: []check.Decl{cD, bD}}
		ev := eval.NewFiles([]*check.File{bf, a})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[c b a]" {
			t.Fatalf("init order = %v, want [c b a]", log)
		}
	})

	t.Run("test decl runs after all package initialization", func(t *testing.T) {
		// eval used to run a `test` block at its declaration
		// position; the compiled `go test` binary starts test
		// functions only after every package var has initialized.
		// The frontier model runs all lets first, so the test sees
		// [init:x init:y test] not [init:x test init:y].
		seq := seqSym()
		xD, _ := letDecl("x", seqCall(types.Int, seq, "init:x", intLit(1)))
		yD, _ := letDecl("y", seqCall(types.Int, seq, "init:y", intLit(2)))
		testD := &check.TestDecl{Sp: sp, Name: "t",
			Body: block(types.Unit,
				seqCall(types.Int, seq, "test", intLit(0)),
				unitLit())}
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{xD, testD, yD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[init:x init:y test]" {
			t.Fatalf("init order = %v, want [init:x init:y test]", log)
		}
	})

	t.Run("pure alias on a function-path loop", func(t *testing.T) {
		// `let f = g` is a pure alias lower emits as a ValueAlias
		// FuncDecl — a wrapper, no package var — and the shared
		// classification gives f the same function-node shape here:
		// w's call path w → f → g → {f, count} is functions only,
		// so w is ready and the order is [w z] like the compiled
		// binary. Before the classification was shared, g's body
		// reference to f resolved to a var node — a self-dependency
		// that stalled the frontier and forced [z w] through the
		// leftover sweep (decision 2026-11-06-eval-init-ready-order
		// §5's recorded residual).
		seq := seqSym()
		uP, _ := vpat("u", types.Unit)
		retT := types.Fn(types.Int, types.P(types.Unit)) // () -> Int
		gT := types.Fn(retT, types.P(types.Unit))        // () -> (() -> Int)
		count := &check.FnDecl{Sp: sp, Sym: sym("count", resolve.KindFunc),
			Sig:    retT,
			Params: []check.Param{{Pat: uP}},
			Body:   intLit(1)}
		fP, fS := vpat("f", gT)
		g := &check.FnDecl{Sp: sp, Sym: sym("g", resolve.KindFunc),
			Sig:    gT,
			Params: []check.Param{{Pat: uP}},
			Body: block(retT,
				&check.Ignore{Base: b(types.Unit), X: vr(fS, gT)},
				vr(count.Sym, retT))}
		fD := &check.LetDecl{Sp: sp, Pat: fP, Value: vr(g.Sym, gT)}
		wD, _ := letDecl("w", seqCall(retT, seq, "w",
			unknownCall(retT, vr(fS, gT), 1, unitLit())))
		zD, _ := letDecl("z", seqCall(types.Int, seq, "z", intLit(0)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{wD, fD, g, count, zD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[w z]" {
			t.Fatalf("init order = %v, want [w z]", log)
		}
	})

	t.Run("effectful alias initializer splits temp var and wrapper", func(t *testing.T) {
		// `let f = { …; count }` lowers to a hidden temp var
		// running the block *plus* a ValueAlias FuncDecl wrapper —
		// and the shared classification gives eval the same split:
		// `a`'s `f ()` call traverses the wrapper's body (count →
		// the w it reads), not f's var node, so a waits on w only;
		// f's block effects run at the declaration's own slot.
		// Compiled prints [w a f]; before the lift eval treated the
		// f reference as a var dependency and printed [w f a]
		// (decision 2026-11-06-eval-init-ready-order §5 shape (a)).
		seq := seqSym()
		uP, _ := vpat("u", types.Unit)
		retT := types.Fn(types.Int, types.P(types.Unit)) // () -> Int
		wD, wS := letDecl("w", seqCall(types.Int, seq, "w", intLit(1)))
		count := &check.FnDecl{Sp: sp, Sym: sym("count", resolve.KindFunc),
			Sig:    retT,
			Params: []check.Param{{Pat: uP}},
			Body:   vr(wS, types.Int)}
		fP, fS := vpat("f", retT)
		fD := &check.LetDecl{Sp: sp, Pat: fP,
			Value: block(retT,
				&check.Ignore{Base: b(types.Unit), X: seqCall(types.Int, seq, "f", intLit(0))},
				vr(count.Sym, retT))}
		aD, _ := letDecl("a",
			block(types.Int,
				&check.Ignore{Base: b(types.Unit), X: seqCall(types.Int, seq, "a", intLit(0))},
				unknownCall(types.Int, vr(fS, retT), 1, unitLit())))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{aD, fD, count, wD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		runOK(t, ev)
		if fmt.Sprint(log) != "[w a f]" {
			t.Fatalf("init order = %v, want [w a f]", log)
		}
	})

	t.Run("ready vars initialize before a pending cycle reports", func(t *testing.T) {
		// Only hand-built units carry an initialization cycle —
		// the compiled one rejects it as E0907. The frontier still
		// initializes every var it can (z is ready and runs), then
		// the cyclic leftovers report through the same lazy
		// re-entrant-read path as before.
		seq := seqSym()
		xP, xS := vpat("x", types.Int)
		yP, yS := vpat("y", types.Int)
		xD := &check.LetDecl{Sp: sp, Pat: xP, Value: vr(yS, types.Int)}
		yD := &check.LetDecl{Sp: sp, Pat: yP,
			Value: bin(syntax.BinAdd, types.Int, vr(xS, types.Int), intLit(1))}
		zD, _ := letDecl("z", seqCall(types.Int, seq, "z", intLit(1)))
		ev := eval.New(&check.File{Sp: sp, Decls: []check.Decl{xD, yD, zD}})
		var log []string
		bindSeq(t, ev, seq, &log)
		r := ev.Run()
		if r.OK() {
			t.Fatal("expected initialization cycle diagnostic")
		}
		if r.Diags[0].Code != eval.CodePanic {
			t.Fatalf("code = %s, want %s", r.Diags[0].Code, eval.CodePanic)
		}
		if fmt.Sprint(log) != "[z]" {
			t.Fatalf("init order = %v, want [z]", log)
		}
	})
}
