package codegen

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// TestShowTupleDecl: a tuple display function renders `(a, b)` through
// each element's leaf recipe — decimal ints, quoted strings — and
// lands in the test half, not the package source.
func TestShowTupleDecl(t *testing.T) {
	tup := types.Tup(types.Int, types.String)
	f := &core.File{Sp: lsp(1), Package: "shapes", PkgSp: lsp(1)}
	f.ShowDecls = []*core.ShowDecl{{
		Sp:  lsp(3),
		Sym: &resolve.Symbol{Name: "gomShow1_deadbeef", Kind: resolve.KindFunc},
		Typ: tup,
		Plan: &core.ShowPlan{Kind: core.ShowTuple, Elems: []*core.ShowPlan{
			{Kind: core.ShowScalar, Scalar: "Int"},
			{Kind: core.ShowScalar, Scalar: "String"},
		}},
	}}
	out, err := File(f, fakeSrc())
	if err != nil {
		t.Fatal(err)
	}
	src := string(out.Source)
	if strings.Contains(src, "gomShow") {
		t.Errorf("display function leaked into the package source:\n%s", src)
	}
	// A file with only display decls produces no test source — the
	// ShowDecls ride along a TestDecl's file.
	if out.TestSource != nil {
		t.Errorf("test source emitted without tests:\n%s", out.TestSource)
	}
}

// TestShowDeclInTestHalf: display functions follow the tests into
// _test.go; assert_eq renders display calls into rt.AssertEq.
func TestShowDeclInTestHalf(t *testing.T) {
	tup := types.Tup(types.Int, types.String)
	showD := &core.ShowDecl{
		Sp:  lsp(3),
		Sym: &resolve.Symbol{Name: "gomShow1_deadbeef", Kind: resolve.KindFunc},
		Typ: tup,
		Plan: &core.ShowPlan{Kind: core.ShowTuple, Elems: []*core.ShowPlan{
			{Kind: core.ShowScalar, Scalar: "Int"},
			{Kind: core.ShowScalar, Scalar: "String"},
		}},
	}
	f := &core.File{Sp: lsp(1), Package: "shapes", PkgSp: lsp(1)}
	xB, xS := vbind("x", tup)
	f.ShowDecls = []*core.ShowDecl{showD}
	f.Decls = append(f.Decls, &core.TestDecl{
		Sp: lsp(4), Name: "pair", NameSp: lsp(4),
		Fn: &core.Func{
			Sp:  lsp(4),
			Sig: types.Fn(types.Unit),
			Body: &core.Body{
				Lets: []*core.Let{{Sp: lsp(5), Name: xB, X: &core.TupleNew{
					Base:  core.Base{Sp: lsp(5), Typ: tup},
					Elems: []core.Atom{intLit("1"), &core.Lit{Base: core.Base{Sp: lsp(5), Typ: types.String}, Kind: syntax.LitStr, Text: `"a"`}},
				}}},
				Tail: &core.CallBuiltin{
					Base: core.Base{Sp: lsp(6), Typ: types.Unit},
					Sym:  bsym("assert_eq"),
					Args: []core.Atom{vref(xS, tup), vref(xS, tup)},
					Eq:   &core.EqPlan{Kind: core.EqNative},
					Show: &core.ShowPlan{Kind: core.ShowCall, Fn: showD},
				},
			},
		},
	})
	out, err := File(f, fakeSrc())
	if err != nil {
		t.Fatal(err)
	}
	src := string(out.TestSource)
	for _, want := range []string{
		"func TestPair(t *testing.T)",
		"rt.AssertEq(t, x == x, gomShow1Deadbeef(x), gomShow1Deadbeef(x))",
		"func gomShow1Deadbeef(v rt.Tuple2[int64, string]) string",
		`"(" + strconv.FormatInt(v.V0, 10) + ", " + strconv.Quote(v.V1) + ")"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("test source lacks %q:\n%s", want, src)
		}
	}
}

// TestShowRecordDecl: a record display prints `{ name = v }` in
// declaration order, and a composite operand parenthesizes.
func TestShowRecordDecl(t *testing.T) {
	pt := &types.Con{Name: "Point"}
	f := &core.File{Sp: lsp(1), Package: "shapes", PkgSp: lsp(1)}
	f.Decls = append(f.Decls, &core.TypeDecl{
		Sp:  lsp(3),
		Sym: tsym("Point"),
		Body: &core.TRecord{Sp: lsp(3), Fields: []core.TField{
			{Sp: lsp(3), Name: "x", Index: 0, Sym: fdsym("x"), Type: types.Float},
			{Sp: lsp(3), Name: "y", Index: 1, Sym: fdsym("y"), Type: types.Float},
		}},
	})
	f.ShowDecls = []*core.ShowDecl{{
		Sp:  lsp(4),
		Sym: &resolve.Symbol{Name: "gomShow1_deadbeef", Kind: resolve.KindFunc},
		Typ: pt,
		Plan: &core.ShowPlan{Kind: core.ShowRecord, Fields: []core.ShowField{
			{Sym: fdsym("x"), Name: "x", Index: 0, Plan: &core.ShowPlan{Kind: core.ShowScalar, Scalar: "Float"}},
			{Sym: fdsym("y"), Name: "y", Index: 1, Plan: &core.ShowPlan{Kind: core.ShowScalar, Scalar: "Float"}},
		}},
	}}
	// A test decl is required for the test half to render at all.
	f.Decls = append(f.Decls, &core.TestDecl{
		Sp: lsp(5), Name: "p", NameSp: lsp(5),
		Fn: &core.Func{Sp: lsp(5), Sig: types.Fn(types.Unit),
			Body: &core.Body{Tail: unitLit()}},
	})
	out, err := File(f, fakeSrc())
	if err != nil {
		t.Fatal(err)
	}
	src := string(out.TestSource)
	for _, want := range []string{
		"func gomShow1Deadbeef(v Point) string",
		`"{ " + "x = " + strconv.FormatFloat(v.X, 'g', -1, 64) + ", " + "y = " + strconv.FormatFloat(v.Y, 'g', -1, 64) + " }"`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("record display lacks %q:\n%s", want, src)
		}
	}
}
