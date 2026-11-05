package treedump

import (
	"slices"
	"testing"

	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
)

// resolvedSample is a small resolved file: one import, one fn whose
// parameter shadows at a use site, and Defs/Uses tables over it.
func resolvedSample() *resolve.Resolved {
	fn := &syntax.FnDecl{
		Sp:   sp(14, 79),
		Name: id(17, 20, "add"),
		Params: []syntax.Param{
			{Sp: sp(21, 28), Pat: &syntax.VarPat{Sp: sp(22, 23), Name: id(22, 23, "x")}, Type: tn(25, 28, "Int")},
			{Sp: sp(30, 37), Pat: &syntax.VarPat{Sp: sp(31, 32), Name: id(31, 32, "y")}, Type: tn(34, 37, "Int")},
		},
		Ret: tn(42, 45, "Int"),
		Body: &syntax.Block{Sp: sp(48, 79), Stmts: []syntax.Expr{
			&syntax.Let{Sp: sp(50, 60),
				Pat:   &syntax.VarPat{Sp: sp(54, 55), Name: id(54, 55, "x")},
				Value: id(58, 59, "y")},
			&syntax.Binary{Sp: sp(62, 75), Op: syntax.BinAdd, X: id(62, 63, "x"), Y: id(66, 67, "x")},
		}},
	}
	f := &syntax.File{
		Sp:      sp(0, 80),
		Package: id(8, 12, "main"),
		Decls: []syntax.Decl{
			&syntax.ImportDecl{Sp: sp(13, 14), Go: true, Path: "fmt", PathSp: sp(13, 14), Name: id(13, 14, "fmt")},
			fn,
		},
	}

	xParam := fn.Params[0].Pat.(*syntax.VarPat).Name
	yParam := fn.Params[1].Pat.(*syntax.VarPat).Name
	stmts := fn.Body.(*syntax.Block).Stmts
	let := stmts[0].(*syntax.Let)
	bin := stmts[1].(*syntax.Binary)

	addSym := &resolve.Symbol{Name: "add", Kind: resolve.KindFunc, Def: fn}
	xSym := &resolve.Symbol{Name: "x", Kind: resolve.KindVar, Def: fn.Params[0].Pat}
	ySym := &resolve.Symbol{Name: "y", Kind: resolve.KindVar, Def: fn.Params[1].Pat}
	x2Sym := &resolve.Symbol{Name: "x", Kind: resolve.KindVar, Def: let.Pat}
	fmtSym := &resolve.Symbol{Name: "fmt", Kind: resolve.KindModule, IsGo: true, Package: "fmt"}
	intSym := &resolve.Symbol{Name: "Int", Kind: resolve.KindBuiltin}
	testSym := &resolve.Symbol{Name: "t0", Kind: resolve.KindFunc, TestOnly: true, Def: fn}

	return &resolve.Resolved{
		File: f,
		Defs: map[*syntax.Ident]*resolve.Symbol{
			fn.Name:                              addSym,
			xParam:                               xSym,
			yParam:                               ySym,
			let.Pat.(*syntax.VarPat).Name:        x2Sym,
			f.Decls[0].(*syntax.ImportDecl).Name: fmtSym,
			id(99, 101, "t0"):                    testSym, // stray table entry not in the tree
		},
		Uses: map[*syntax.Ident]*resolve.Symbol{
			let.Value.(*syntax.Ident):                 ySym,
			bin.X.(*syntax.Ident):                     x2Sym,
			bin.Y.(*syntax.Ident):                     x2Sym,
			fn.Ret.(*syntax.TypeName).Name:            intSym,
			fn.Params[0].Type.(*syntax.TypeName).Name: intSym,
			fn.Params[1].Type.(*syntax.TypeName).Name: intSym,
			fn.Params[0].Pat.(*syntax.VarPat).Name:    nil, // impossible in practice; exercises nil guard
		},
	}
}

func TestDumpResolved(t *testing.T) {
	got := DumpResolved(resolvedSample())
	want := `(resolved
  (file@0:80 main@8:12
    (import@13:14 "fmt"@13:14 fmt%1@13:14 :go)
    (fn@14:79 add%2@17:20
      (params
        (param@21:28
          (varpat@22:23 x%3@22:23)
          (: Int%4@25:28))
        (param@30:37
          (varpat@31:32 y%5@31:32)
          (: Int%4@34:37)))
      (-> Int%4@42:45)
      (block@48:79
        (let@50:60 (varpat@54:55 x%6@54:55) y%5@58:59)
        (binary@62:75 + x%6@62:63 x%6@66:67))))
  (defs
    (def@13:14 -> fmt%1)
    (def@17:20 -> add%2)
    (def@22:23 -> x%3)
    (def@31:32 -> y%5)
    (def@54:55 -> x%6)
    (def@99:101 -> t0%7))
  (uses
    (use@22:23 -> ?%)
    (use@25:28 -> Int%4)
    (use@34:37 -> Int%4)
    (use@42:45 -> Int%4)
    (use@58:59 -> y%5)
    (use@62:63 -> x%6)
    (use@66:67 -> x%6))
  (syms
    (sym %1 fmt module :pkg "fmt" :go)
    (sym %2 add func def@14:79)
    (sym %3 x var def@22:23)
    (sym %4 Int builtin)
    (sym %5 y var def@31:32)
    (sym %6 x var def@54:55)
    (sym %7 t0 func def@14:79 :test)))
`
	if got != want {
		t.Errorf("DumpResolved mismatch:\n%s", got)
	}
}

// TestDeterminism dumps the same resolved file repeatedly — Defs/Uses
// are maps, so iteration order varies run to run — plus a copy whose
// tables were built in a different insertion order. All dumps must be
// byte-identical.
func TestDeterminism(t *testing.T) {
	r := resolvedSample()

	// Rebuild the tables in reverse insertion order.
	defs := make(map[*syntax.Ident]*resolve.Symbol, len(r.Defs))
	uses := make(map[*syntax.Ident]*resolve.Symbol, len(r.Uses))
	keys := make([]*syntax.Ident, 0, len(r.Defs))
	for k := range r.Defs {
		keys = append(keys, k)
	}
	for _, key := range slices.Backward(keys) {
		defs[key] = r.Defs[key]
	}
	keys = keys[:0]
	for k := range r.Uses {
		keys = append(keys, k)
	}
	for _, key := range slices.Backward(keys) {
		uses[key] = r.Uses[key]
	}
	r2 := &resolve.Resolved{File: r.File, Defs: defs, Uses: uses}

	want := DumpResolved(r)
	if got := DumpResolved(r2); got != want {
		t.Errorf("dump differs when tables are built in another order:\n%s", got)
	}
	for i := range 10 {
		if got := DumpResolved(r); got != want {
			t.Fatalf("dump %d differs:\n%s", i, got)
		}
	}
}
