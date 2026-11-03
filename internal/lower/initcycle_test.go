package lower

import (
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Tests for the aggregate static-alias lowering and the residual
// initialization-cycle diagnostic (initcycle.go;
// docs/decisions/2026-11-05-aggregate-function-bindings.md): a
// function-typed variable a top-level pattern binds becomes a
// canonical ValueAlias wrapper when its element is a pure local
// function reference, and whatever variable cycle that leaves is
// reported as E0907.

// TestAggregateAliasTuple: `let (again, x) = (count, 0)` — the
// hidden tuple stays an eager VarDecl, `again` becomes a ValueAlias
// FuncDecl calling count, and `x` keeps the pure projection.
func TestAggregateAliasTuple(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	again, x := vsym("again", fd.Sig), vsym("x", types.Int)
	tupT := types.Tup(fd.Sig, types.Int)
	d := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			vpat(again, fd.Sig), vpat(x, types.Int),
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{
			vr(fd.Sym, fd.Sig), intLit("0"),
		}},
	}
	out := mustLower(t, fd, d)
	if len(out.Decls) != 4 {
		t.Fatalf("decls = %d, want 4 (fn + tmp + again + x)", len(out.Decls))
	}
	tmp, ok := out.Decls[1].(*core.VarDecl)
	if !ok {
		t.Fatalf("decl1 is %T, want VarDecl (hidden tuple)", out.Decls[1])
	}
	a, ok := out.Decls[2].(*core.FuncDecl)
	if !ok || !a.ValueAlias || a.Sym != again {
		t.Fatalf("decl2 = %+v, want ValueAlias FuncDecl for again", out.Decls[2])
	}
	vx, ok := out.Decls[3].(*core.VarDecl)
	if !ok || vx.Sym != x {
		t.Fatalf("decl3 = %+v, want VarDecl for x", out.Decls[3])
	}
	fg, ok := vx.Body.Tail.(*core.FieldGet)
	if !ok || fg.Recv.(*core.Var).Sym != tmp.Sym {
		t.Fatalf("x's body = %+v, want FieldGet off the hidden tuple", vx.Body.Tail)
	}
}

// TestAggregateAliasChain: `let b = count; let (a, _) = (b, 0)` —
// the element is another let-bound function variable, so `a` wraps
// `b` (itself a ValueAlias FuncDecl) — the chain stays static.
func TestAggregateAliasChain(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	a, bv := vsym("a", fd.Sig), vsym("b", fd.Sig)
	bd := &check.LetDecl{Sp: sp, Pat: vpat(bv, fd.Sig), Value: vr(fd.Sym, fd.Sig)}
	tupT := types.Tup(fd.Sig, types.Int)
	ad := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			vpat(a, fd.Sig), wpat(types.Int),
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{
			vr(bv, fd.Sig), intLit("0"),
		}},
	}
	out := mustLower(t, fd, ad, bd)
	// fn + tmp + a(FuncDecl) + b(FuncDecl)
	if len(out.Decls) != 4 {
		t.Fatalf("decls = %d, want 4", len(out.Decls))
	}
	aa, ok := out.Decls[2].(*core.FuncDecl)
	if !ok || !aa.ValueAlias || aa.Sym != a {
		t.Fatalf("decl2 = %+v, want ValueAlias for a", out.Decls[2])
	}
	bb, ok := out.Decls[3].(*core.FuncDecl)
	if !ok || !bb.ValueAlias || bb.Sym != bv {
		t.Fatalf("decl3 = %+v, want ValueAlias for b", out.Decls[3])
	}
}

// TestAggregateAliasCtor: `let Box g = Box count` — the tested
// pattern keeps its eager match (a hidden discard VarDecl running
// the constructor check at init) while `g` becomes the wrapper.
func TestAggregateAliasCtor(t *testing.T) {
	box := &resolve.Symbol{Name: "Box", Kind: resolve.KindType}
	mk := csym("Box")
	boxT := &types.Con{Name: "Box"}
	fnT := types.Fn(types.Int, types.P(types.Int))
	td := &check.TypeDecl{Sp: sp, Sym: box, Body: &check.SumType{
		Sp: sp,
		Variants: []check.Variant{{
			Sp: sp, Sym: mk,
			Fields: []check.TField{{Index: 0, Type: fnT}},
		}},
	}}
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	g := vsym("g", fnT)
	ctorApp := app(vr(mk, types.Fn(boxT, types.P(fnT))),
		[]check.Expr{vr(fd.Sym, fd.Sig)},
		check.CallInfo{Kind: check.CallDirect, Callee: mk, Arity: 1, Applied: 1, HoleIndex: -1}, boxT)
	d := &check.LetDecl{Sp: sp,
		Pat:   &check.CtorPat{Base: b(boxT), Ctor: mk, Args: []check.CtorArg{{Pat: vpat(g, fnT)}}},
		Value: ctorApp,
	}
	out := mustLower(t, td, fd, d)
	// TypeDecl + fn + tmp(Box) + discard(match) + g(FuncDecl)
	if len(out.Decls) != 5 {
		t.Fatalf("decls = %d, want 5", len(out.Decls))
	}
	discard, ok := out.Decls[3].(*core.VarDecl)
	if !ok {
		t.Fatalf("decl3 is %T, want VarDecl (match discard)", out.Decls[3])
	}
	if _, ok := discard.Body.Tail.(*core.Match); !ok {
		t.Fatalf("discard body is %T, want Match", discard.Body.Tail)
	}
	gg, ok := out.Decls[4].(*core.FuncDecl)
	if !ok || !gg.ValueAlias || gg.Sym != g {
		t.Fatalf("decl4 = %+v, want ValueAlias for g", out.Decls[4])
	}
}

// TestAggregateUntraceableElement: `let (f, _) = (mk (), 0)` — the
// element is a call, not a traceable reference, so `f` keeps the
// eager projection VarDecl; with no back-reference from `mk` there
// is no cycle and no diagnostic.
func TestAggregateUntraceableElement(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	fnT := types.Fn(types.Int, types.P(types.Int))
	mkDecl := decl("mk", []check.Param{param(wpat(types.Unit))}, vr(fd.Sym, fd.Sig),
		types.Fn(fnT, types.P(types.Unit)))
	f := vsym("f", fnT)
	tupT := types.Tup(fnT, types.Int)
	call := app(vr(mkDecl.Sym, mkDecl.Sig), []check.Expr{unitLit()},
		check.CallInfo{Kind: check.CallDirect, Callee: mkDecl.Sym, Arity: 1, Applied: 1, HoleIndex: -1}, fnT)
	d := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			vpat(f, fnT), wpat(types.Int),
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{call, intLit("0")}},
	}
	out := mustLower(t, fd, mkDecl, d)
	if _, ok := out.Decls[3].(*core.VarDecl); !ok {
		t.Fatalf("decl3 is %T, want eager VarDecl for f", out.Decls[3])
	}
}

// TestInitCycleNonFunc: `fn g (n) = x; let (f, x) = (g, 41)` — the
// cycle routes through a non-function binding; `x` keeps the
// projection and the leftover variable cycle is reported per
// participant (the hidden tuple and x).
func TestInitCycleNonFunc(t *testing.T) {
	n, x := vsym("n", types.Int), vsym("x", types.Int)
	gd := decl("g", []check.Param{param(vpat(n, types.Int))}, vr(x, types.Int), nil)
	f := vsym("f", gd.Sig)
	tupT := types.Tup(gd.Sig, types.Int)
	d := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			vpat(f, gd.Sig), vpat(x, types.Int),
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{
			vr(gd.Sym, gd.Sig), intLit("41"),
		}},
	}
	_, diags := lower1(t, gd, d)
	wantCodes(t, diags, "E0907", "E0907")
}

// TestInitCycleComputed: `let (f, _) = (mk (), 0)` where `mk`'s
// body returns `f` — the element is untraceable and the cycle is
// reported rather than handed to go build.
func TestInitCycleComputed(t *testing.T) {
	fnT := types.Fn(types.Int, types.P(types.Int))
	f := vsym("f", fnT)
	mkDecl := decl("mk", []check.Param{param(wpat(types.Unit))}, vr(f, fnT),
		types.Fn(fnT, types.P(types.Unit)))
	tupT := types.Tup(fnT, types.Int)
	call := app(vr(mkDecl.Sym, mkDecl.Sig), []check.Expr{unitLit()},
		check.CallInfo{Kind: check.CallDirect, Callee: mkDecl.Sym, Arity: 1, Applied: 1, HoleIndex: -1}, fnT)
	d := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			vpat(f, fnT), wpat(types.Int),
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{call, intLit("0")}},
	}
	_, diags := lower1(t, mkDecl, d)
	wantCodes(t, diags, "E0907", "E0907")
}

// TestInitCycleVarRHS: `let pair = (count, 0); let (a, b) = pair`
// where `count`'s body applies `a` — the trace cannot see through
// the `pair` binding, so the tuple var, the destructuring
// temporary, and `a` close the cycle.
func TestInitCycleVarRHS(t *testing.T) {
	n := vsym("n", types.Int)
	a := vsym("a", types.Fn(types.Int, types.P(types.Int)))
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	// `fn count (n) = a n` — count's body applies the destructured
	// function variable.
	fd.Body = app(vr(a, fd.Sig), []check.Expr{vr(n, types.Int)},
		check.CallInfo{Kind: check.CallUnknown}, types.Int)
	pairT := types.Tup(fd.Sig, types.Int)
	pair, bv := vsym("pair", pairT), vsym("b", types.Int)
	pd := &check.LetDecl{Sp: sp, Pat: vpat(pair, pairT),
		Value: &check.TupleLit{Base: b(pairT),
			Elems: []check.Expr{vr(fd.Sym, fd.Sig), intLit("0")}}}
	ad := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(pairT), Elems: []check.Pat{
			vpat(a, fd.Sig), vpat(bv, types.Int),
		}},
		Value: vr(pair, pairT),
	}
	_, diags := lower1(t, pd, fd, ad)
	wantCodes(t, diags, "E0907", "E0907", "E0907")
}

// TestAggregateAliasAsPat: `let (f as g, _) = (count, 0)` — `as`
// binds both names to the same element, so both become wrappers.
func TestAggregateAliasAsPat(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	f, g := vsym("f", fd.Sig), vsym("g", fd.Sig)
	tupT := types.Tup(fd.Sig, types.Int)
	d := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			&check.AsPat{Base: b(fd.Sig), Sym: g, Pat: vpat(f, fd.Sig)},
			wpat(types.Int),
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{
			vr(fd.Sym, fd.Sig), intLit("0"),
		}},
	}
	out := mustLower(t, fd, d)
	if len(out.Decls) != 4 {
		t.Fatalf("decls = %d, want 4 (fn + tmp + f + g)", len(out.Decls))
	}
	// boundVars yields the `as` name before the inner pattern's.
	for i, sym := range []*resolve.Symbol{g, f} {
		w, ok := out.Decls[2+i].(*core.FuncDecl)
		if !ok || !w.ValueAlias || w.Sym != sym {
			t.Fatalf("decl%d = %+v, want ValueAlias for %s", 2+i, out.Decls[2+i], sym.Name)
		}
	}
}

// TestAggregateAliasNested: `let ((a, _), (_, m)) = ((count, 0),
// (0, 41))` — the trace descends through nested tuple literals.
func TestAggregateAliasNested(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	a, m := vsym("a", fd.Sig), vsym("m", types.Int)
	in1, in2 := types.Tup(fd.Sig, types.Int), types.Tup(types.Int, types.Int)
	tupT := types.Tup(in1, in2)
	d := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			&check.TuplePat{Base: b(in1), Elems: []check.Pat{
				vpat(a, fd.Sig), wpat(types.Int)}},
			&check.TuplePat{Base: b(in2), Elems: []check.Pat{
				wpat(types.Int), vpat(m, types.Int)}},
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{
			&check.TupleLit{Base: b(in1), Elems: []check.Expr{
				vr(fd.Sym, fd.Sig), intLit("0")}},
			&check.TupleLit{Base: b(in2), Elems: []check.Expr{
				intLit("0"), intLit("41")}},
		}},
	}
	out := mustLower(t, fd, d)
	if len(out.Decls) != 4 {
		t.Fatalf("decls = %d, want 4 (fn + tmp + a + m)", len(out.Decls))
	}
	w, ok := out.Decls[2].(*core.FuncDecl)
	if !ok || !w.ValueAlias || w.Sym != a {
		t.Fatalf("decl2 = %+v, want ValueAlias for a", out.Decls[2])
	}
	vm, ok := out.Decls[3].(*core.VarDecl)
	if !ok || vm.Sym != m {
		t.Fatalf("decl3 = %+v, want VarDecl for m", out.Decls[3])
	}
}

// TestAggregateAliasBlockElem: `let (a, _) = ({ …; count }, 0)` —
// a block element still traces to its tail; the hidden tuple
// evaluates the block's statements eagerly.
func TestAggregateAliasBlockElem(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	a := vsym("a", fd.Sig)
	tupT := types.Tup(fd.Sig, types.Int)
	block := &check.Block{Base: b(fd.Sig), Stmts: []check.Expr{
		unitLit(), vr(fd.Sym, fd.Sig),
	}}
	d := &check.LetDecl{Sp: sp,
		Pat: &check.TuplePat{Base: b(tupT), Elems: []check.Pat{
			vpat(a, fd.Sig), wpat(types.Int),
		}},
		Value: &check.TupleLit{Base: b(tupT), Elems: []check.Expr{
			block, intLit("0"),
		}},
	}
	out := mustLower(t, fd, d)
	if len(out.Decls) != 3 {
		t.Fatalf("decls = %d, want 3 (fn + tmp + a)", len(out.Decls))
	}
	w, ok := out.Decls[2].(*core.FuncDecl)
	if !ok || !w.ValueAlias || w.Sym != a {
		t.Fatalf("decl2 = %+v, want ValueAlias for a", out.Decls[2])
	}
}

// TestAggregateAliasVarBlock: `let g = { (); count }` — a VarPat
// block initializer is emitted eagerly under a hidden variable
// (its statements carry effects the wrapper cannot stand in for),
// alongside the wrapper FuncDecl.
func TestAggregateAliasVarBlock(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	g := vsym("g", fd.Sig)
	d := &check.LetDecl{Sp: sp, Pat: vpat(g, fd.Sig),
		Value: &check.Block{Base: b(fd.Sig), Stmts: []check.Expr{
			unitLit(), vr(fd.Sym, fd.Sig),
		}},
	}
	out := mustLower(t, fd, d)
	if len(out.Decls) != 3 {
		t.Fatalf("decls = %d, want 3 (fn + hidden init + g)", len(out.Decls))
	}
	tmp, ok := out.Decls[1].(*core.VarDecl)
	if !ok {
		t.Fatalf("decl1 is %T, want VarDecl (hidden initializer)", out.Decls[1])
	}
	if tmp.Sym == g {
		t.Fatalf("the initializer must live under a hidden symbol, not g")
	}
	w, ok := out.Decls[2].(*core.FuncDecl)
	if !ok || !w.ValueAlias || w.Sym != g {
		t.Fatalf("decl2 = %+v, want ValueAlias for g", out.Decls[2])
	}
}

// TestAggregateAliasVarPlain: `let g = (count: Int -> Int)` — a
// transparent ascription is not an evaluation, so the wrapper is
// the only declaration the binding emits.
func TestAggregateAliasVarPlain(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("count", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	g := vsym("g", fd.Sig)
	d := &check.LetDecl{Sp: sp, Pat: vpat(g, fd.Sig),
		Value: &check.Ascribe{Base: b(fd.Sig), X: vr(fd.Sym, fd.Sig)},
	}
	out := mustLower(t, fd, d)
	if len(out.Decls) != 2 {
		t.Fatalf("decls = %d, want 2 (fn + g)", len(out.Decls))
	}
	w, ok := out.Decls[1].(*core.FuncDecl)
	if !ok || !w.ValueAlias || w.Sym != g {
		t.Fatalf("decl1 = %+v, want ValueAlias for g", out.Decls[1])
	}
}

// TestInitCycleVarBlock: `let g = { h }` where `h` is bound to a
// call — the tail is a `Var` but not a static-alias root, so `g`
// keeps its eager initializer and the residual cycle is reported.
func TestInitCycleVarBlock(t *testing.T) {
	fnT := types.Fn(types.Int, types.P(types.Int))
	g := vsym("g", fnT)
	mkDecl := decl("mk", []check.Param{param(wpat(types.Unit))}, vr(g, fnT),
		types.Fn(fnT, types.P(types.Unit)))
	h := vsym("h", fnT)
	hd := &check.LetDecl{Sp: sp, Pat: vpat(h, fnT),
		Value: app(vr(mkDecl.Sym, mkDecl.Sig), []check.Expr{unitLit()},
			check.CallInfo{Kind: check.CallDirect, Callee: mkDecl.Sym, Arity: 1, Applied: 1, HoleIndex: -1},
			fnT)}
	gd := &check.LetDecl{Sp: sp, Pat: vpat(g, fnT),
		Value: &check.Block{Base: b(fnT), Stmts: []check.Expr{vr(h, fnT)}},
	}
	_, diags := lower1(t, mkDecl, hd, gd)
	wantCodes(t, diags, "E0907", "E0907")
}

// TestInitCycleSelfCall: `let r = f 0` with `f`'s body reading `r`
// — a one-variable cycle through the function's body.
func TestInitCycleSelfCall(t *testing.T) {
	n, r := vsym("n", types.Int), vsym("r", types.Int)
	fd := decl("f", []check.Param{param(vpat(n, types.Int))}, vr(r, types.Int), nil)
	rd := &check.LetDecl{Sp: sp, Pat: vpat(r, types.Int),
		Value: app(vr(fd.Sym, fd.Sig), []check.Expr{intLit("0")},
			check.CallInfo{Kind: check.CallDirect, Callee: fd.Sym, Arity: 1, Applied: 1, HoleIndex: -1},
			types.Int)}
	_, diags := lower1(t, rd, fd)
	wantCodes(t, diags, "E0907")
}
