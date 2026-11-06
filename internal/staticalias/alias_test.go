package staticalias_test

import (
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/staticalias"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Hand-built check.Decl fixtures pin Classify's three maps
// directly — which bindings join Aliases, which keep an Elems
// entry without aliasing, which are flagged Init. The shapes the
// maps produce end-to-end (wrapper FuncDecl vs hidden var, eval
// node kinds) stay covered by internal/lower's alias tests and
// internal/build's diff_initorder probes.

var sp = diag.SpanIn("test.gom", 0, 1)

func base(t types.Type) check.Base { return check.Base{Sp: sp, Typ: t} }

var sig = types.Fn(types.Int, types.P(types.Unit))

func vsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindVar}
}

func fsym(name string) *resolve.Symbol {
	return &resolve.Symbol{Name: name, Kind: resolve.KindFunc}
}

func vr(s *resolve.Symbol, t types.Type) *check.Var {
	return &check.Var{Base: base(t), Sym: s}
}

func intLit() *check.Lit {
	return &check.Lit{Base: base(types.Int), Kind: syntax.LitInt, Text: "0"}
}

func unitLit() *check.Lit {
	return &check.Lit{Base: base(types.Unit), Kind: syntax.LitUnit, Text: "()"}
}

// letVar is `let name = value` with a VarPat of type t; returns
// the decl and the symbol the pattern binds.
func letVar(name string, t types.Type, value check.Expr) (*check.LetDecl, *resolve.Symbol) {
	s := vsym(name)
	return &check.LetDecl{
		Sp:    sp,
		Pat:   &check.VarPat{Base: base(t), Sym: s},
		Value: value,
	}, s
}

// fnDecl is `fn name (u: Unit) -> Int = 0` — a local non-thunk
// function, the only root a reference chain may alias to.
func fnDecl(name string, thunk bool) (*check.FnDecl, *resolve.Symbol) {
	s := fsym(name)
	return &check.FnDecl{
		Sp:  sp,
		Sym: s, Sig: sig, Thunk: thunk,
		Params: []check.Param{{Pat: &check.VarPat{Base: base(types.Unit), Sym: vsym("u")}}},
		Body:   intLit(),
	}, s
}

// call is a saturated direct call `callee ()`.
func call(callee *resolve.Symbol, fnTyp *types.Func) *check.Apply {
	return &check.Apply{
		Base: base(types.Int),
		Fn:   vr(callee, fnTyp),
		Args: []check.Arg{{Value: unitLit()}},
		Call: check.CallInfo{
			Kind: check.CallDirect, Callee: callee,
			Arity: 1, Applied: 1, HoleIndex: -1,
		},
	}
}

func TestClassify(t *testing.T) {
	t.Run("pure alias to a local function", func(t *testing.T) {
		// `let f = count` — the element is a bare Var rooted in a
		// FnDecl: f joins Aliases, keeps the Var as its Elems
		// entry, and needs no hidden initializer.
		fd, count := fnDecl("count", false)
		ld, f := letVar("f", sig, vr(count, sig))
		decls := []check.Decl{ld, fd}
		idx := staticalias.Classify(decls, decls)
		if !idx.Aliases[f] {
			t.Fatal("let f = count not classified as alias")
		}
		if idx.Init[f] {
			t.Fatal("pure alias flagged Init — no hidden temp exists")
		}
		if e, ok := idx.Elems[f].(*check.Var); !ok || e.Sym != count {
			t.Fatalf("Elems[f] = %#v, want Var count", idx.Elems[f])
		}
	})

	t.Run("alias chain resolves transitively", func(t *testing.T) {
		// `let f = g; let g = count` — forward-declared too: f's
		// chain reaches count through g's binding, so both alias.
		fd, count := fnDecl("count", false)
		gd, g := letVar("g", sig, vr(count, sig))
		ld, f := letVar("f", sig, vr(g, sig))
		decls := []check.Decl{ld, gd, fd}
		idx := staticalias.Classify(decls, decls)
		if !idx.Aliases[f] || !idx.Aliases[g] {
			t.Fatalf("chain Aliases = %v, want f and g", idx.Aliases)
		}
	})

	t.Run("effectful initializer keeps Init", func(t *testing.T) {
		// `let f = { (); count }` — the block tail is the Var but
		// the non-tail statement still has to run: f aliases *and*
		// gets the hidden-temp flag.
		fd, count := fnDecl("count", false)
		ld, f := letVar("f", sig, &check.Block{Base: base(sig),
			Stmts: []check.Expr{unitLit(), vr(count, sig)}})
		decls := []check.Decl{ld, fd}
		idx := staticalias.Classify(decls, decls)
		if !idx.Aliases[f] || !idx.Init[f] {
			t.Fatalf("block alias: Aliases=%v Init=%v, want both", idx.Aliases[f], idx.Init[f])
		}
	})

	t.Run("thunk root is not an alias", func(t *testing.T) {
		// `let f = g` where g is a generalized value-let thunk:
		// the element trace reaches g's Var (Elems keeps it) but
		// a thunk is not a static root — no wrapper.
		gd, g := fnDecl("g", true)
		ld, f := letVar("f", sig, vr(g, sig))
		decls := []check.Decl{ld, gd}
		idx := staticalias.Classify(decls, decls)
		if idx.Aliases[f] {
			t.Fatal("alias rooted in a thunk classified as alias")
		}
		if _, ok := idx.Elems[f]; !ok {
			t.Fatal("Elems[f] missing — the trace did reach the Var")
		}
	})

	t.Run("computed initializer is not an alias", func(t *testing.T) {
		// `let x = count ()` — the element trace stops at the
		// application; nothing is recorded for x at all.
		fd, count := fnDecl("count", false)
		ld, x := letVar("x", types.Int, call(count, sig))
		decls := []check.Decl{ld, fd}
		idx := staticalias.Classify(decls, decls)
		if idx.Aliases[x] || idx.Init[x] {
			t.Fatalf("call initializer: Aliases=%v Init=%v", idx.Aliases[x], idx.Init[x])
		}
		if _, ok := idx.Elems[x]; ok {
			t.Fatal("Elems[x] recorded for a non-Var element")
		}
	})

	t.Run("aggregate element aliases alone", func(t *testing.T) {
		// `let (again, x) = (count, 0)` — positional tuple tracing
		// reaches count for again; x's element is a literal, so
		// only again joins the maps.
		fd, count := fnDecl("count", false)
		tupT := types.Tup(sig, types.Int)
		again, x := vsym("again"), vsym("x")
		ld := &check.LetDecl{
			Sp: sp,
			Pat: &check.TuplePat{Base: base(tupT), Elems: []check.Pat{
				&check.VarPat{Base: base(sig), Sym: again},
				&check.VarPat{Base: base(types.Int), Sym: x},
			}},
			Value: &check.TupleLit{Base: base(tupT),
				Elems: []check.Expr{vr(count, sig), intLit()}},
		}
		decls := []check.Decl{ld, fd}
		idx := staticalias.Classify(decls, decls)
		if !idx.Aliases[again] {
			t.Fatal("tuple element alias not classified")
		}
		if idx.Aliases[x] {
			t.Fatal("int element bound a ValueAlias")
		}
		if _, ok := idx.Elems[x]; ok {
			t.Fatal("Elems[x] recorded for a literal element")
		}
	})

	t.Run("scan does not cross the decls boundary", func(t *testing.T) {
		// a.gom's `let h = f` names a sibling file's alias
		// binding: f classifies as an alias inside its own file's
		// decls, but h's file never saw that binding shape — h's
		// chain stops at the f Var (a variable, not a scanned
		// alias, not a fn root), so h is a plain var with an
		// Elems entry only. This is the file-local boundary
		// lower's per-file Classify(f.Decls, unit) call mirrors.
		fd, g := fnDecl("g", false)
		fld, f := letVar("f", sig, vr(g, sig)) // sibling file's decls
		hld, h := letVar("h", sig, vr(f, sig)) // this file's decls
		unit := []check.Decl{hld, fld, fd}
		idx := staticalias.Classify([]check.Decl{hld}, unit)
		if idx.Aliases[h] {
			t.Fatal("h aliases a sibling file's let binding")
		}
		if _, ok := idx.Elems[h]; !ok {
			t.Fatal("Elems[h] missing — the trace did reach the f Var")
		}
		// Inside f's own decls the same unit indexes g: f is an
		// alias — the boundary is the scan set, not the universe.
		fidx := staticalias.Classify([]check.Decl{fld}, unit)
		if !fidx.Aliases[f] {
			t.Fatal("f's own file does not classify f as alias")
		}
	})

	t.Run("alias-only cycle has no static root", func(t *testing.T) {
		// `let f = g; let g = f` — the reference chain loops
		// without reaching a function: neither aliases, and the
		// visited set keeps the walk finite.
		gd, g := letVar("g", sig, unitLit())
		ld, f := letVar("f", sig, vr(g, sig))
		gd.Value = vr(f, sig)
		decls := []check.Decl{ld, gd}
		idx := staticalias.Classify(decls, decls)
		if idx.Aliases[f] || idx.Aliases[g] {
			t.Fatalf("rootless alias cycle classified: %v", idx.Aliases)
		}
	})
}
