package lower

import (
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

func TestStaticAliasRepresentation(t *testing.T) {
	n := vsym("n", types.Int)
	fd := decl("f", []check.Param{param(vpat(n, types.Int))}, vr(n, types.Int), nil)
	g := vsym("g", fd.Sig)
	h := vsym("h", fd.Sig)
	gd := &check.LetDecl{Sp: sp, Pat: vpat(g, fd.Sig), Value: vr(fd.Sym, fd.Sig)}
	hd := &check.LetDecl{Sp: sp, Pat: vpat(h, fd.Sig), Value: vr(g, fd.Sig)}
	// Reverse source order also exercises forward alias-chain indexing.
	out := mustLower(t, hd, gd, fd)
	for i := range 2 {
		a := fnDeclOf(t, out, i)
		if !a.ValueAlias || len(a.Fn.Params) != 1 {
			t.Fatalf("alias %d = %+v", i, a)
		}
	}
	if g.Kind != resolve.KindVar || h.Kind != resolve.KindVar {
		t.Fatal("lowering mutated checked variable symbols")
	}
}

func TestComputedFunctionValueStaysEager(t *testing.T) {
	n, m := vsym("n", types.Int), vsym("m", types.Int)
	fd := decl("add", []check.Param{param(vpat(n, types.Int)), param(vpat(m, types.Int))}, vr(n, types.Int), nil)
	res := types.Fn(types.Int, types.P(types.Int))
	g := vsym("g", res)
	gd := &check.LetDecl{Sp: sp, Pat: vpat(g, res), Value: app(vr(fd.Sym, fd.Sig), []check.Expr{intLit("1")},
		check.CallInfo{Kind: check.CallPartial, Callee: fd.Sym, Arity: 2, Applied: 1, Unapplied: 1, HoleIndex: -1}, res)}
	out := mustLower(t, fd, gd)
	if _, ok := out.Decls[1].(*core.VarDecl); !ok {
		t.Fatalf("computed binding = %T, want eager VarDecl", out.Decls[1])
	}
}
