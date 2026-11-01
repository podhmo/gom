package treedump

import (
	"testing"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// tb builds a typed node base with the given type.
func tb(a, b int, t types.Type) check.Base { return check.Base{Sp: sp(a, b), Typ: t} }

// typedSample exercises the typed decls, the call-info variants on
// Apply, member access, patterns and the `(syms)` section.
func typedSample() *check.File {
	xS := &resolve.Symbol{Name: "x", Kind: resolve.KindVar}
	yS := &resolve.Symbol{Name: "y", Kind: resolve.KindVar}
	drawS := &resolve.Symbol{Name: "draw", Kind: resolve.KindFunc}
	putS := &resolve.Symbol{Name: "Put", Kind: resolve.KindFunc, IsGo: true, Package: "fmt"}
	shapeS := &resolve.Symbol{Name: "Shape", Kind: resolve.KindType}
	circleS := &resolve.Symbol{Name: "Circle", Kind: resolve.KindCtor}
	radiusS := &resolve.Symbol{Name: "radius", Kind: resolve.KindField}
	fmtS := &resolve.Symbol{Name: "fmt", Kind: resolve.KindModule, IsGo: true, Package: "fmt"}
	writeS := &resolve.Symbol{Name: "Write", Kind: resolve.KindMethod, IsGo: true, Package: "bytes"}
	bS := &resolve.Symbol{Name: "b", Kind: resolve.KindVar}
	vS := &resolve.Symbol{Name: "v", Kind: resolve.KindVar}
	restS := &resolve.Symbol{Name: "rest", Kind: resolve.KindVar}
	i2i := types.Fn(types.Int, types.P(types.Int))

	member := &check.MemberExpr{
		Base: tb(10, 20, types.String),
		Recv: &check.Var{Base: tb(10, 14, &types.Con{Name: "Buf"}), Sym: bS},
		Sel:  id(15, 20, "Write"), Sym: writeS, Index: -1,
	}
	return &check.File{
		Sp: sp(0, 400), Package: "geom", PkgSp: sp(8, 12),
		Imports: []check.Import{
			{Sp: sp(2, 7), Path: "fmt", IsGo: true, Sym: fmtS},
		},
		Decls: []check.Decl{
			&check.LetDecl{Sp: sp(5, 30),
				Pat: &check.VarPat{Base: tb(9, 10, types.Int), Sym: xS},
				Value: &check.Apply{
					Base: tb(13, 30, types.Int),
					Fn:   &check.Var{Base: tb(13, 16, i2i), Sym: drawS},
					Args: []check.Arg{
						{Value: &check.Hole{Base: tb(17, 18, types.Int)}},
						{Label: "to", LabelSp: sp(19, 22), Value: &check.Lit{Base: tb(23, 24, types.Int), Kind: syntax.LitInt, Text: "3"}},
					},
					Call: check.CallInfo{Kind: check.CallGo, Callee: putS, Arity: 2, Applied: 2, HoleIndex: 0, Variadic: true},
				}},
			&check.TypeDecl{Sp: sp(32, 60), Sym: shapeS,
				TypeParams: []*types.Var{{ID: 0, Name: "a"}},
				Body: &check.SumType{Sp: sp(40, 60), Variants: []check.Variant{
					{Sp: sp(42, 55), Sym: circleS, Fields: []check.TField{
						{Sp: sp(50, 55), Name: "radius", Index: 0, Sym: radiusS, Type: types.Int},
						{Sp: sp(56, 58), Label: "to", Index: 1, Type: types.Float},
						{Sp: sp(59, 60), Index: 2, Type: types.Bool},
					}},
				}}},
			&check.FnDecl{Sp: sp(62, 90), Sym: drawS, Sig: i2i,
				Params: []check.Param{
					{Label: "to", Pat: &check.VarPat{Base: tb(65, 66, types.Int), Sym: yS}},
				},
				Body: &check.Match{Base: tb(70, 90, types.Unit), Subj: member, Arms: []check.MatchArm{
					{Sp: sp(72, 88),
						Pat: &check.CtorPat{Base: tb(72, 80, &types.Con{Name: "Shape"}),
							Name: id(72, 78, "Circle"), Ctor: circleS,
							Args: []check.CtorArg{{Label: "v", Pat: &check.VarPat{Base: tb(79, 80, types.Int), Sym: vS}}},
							Fields: []check.FieldPat{{Sp: sp(79, 80), Name: "radius", Sym: radiusS, Index: 0,
								Pat: &check.WildPat{Base: tb(79, 80, types.Int)}}},
							Rest: true},
						Guard: &check.Var{Base: tb(81, 82, types.Bool), Sym: vS},
						Body: &check.ListLit{Base: tb(85, 86, types.List(types.Int)), Elems: []check.Expr{
							&check.Var{Base: tb(85, 86, types.Int), Sym: xS},
							&check.Spread{Base: tb(85, 86, types.List(types.Int)), X: &check.Var{Base: tb(85, 86, types.List(types.Int)), Sym: restS}},
						}}},
				}}},
			&check.TestDecl{Sp: sp(91, 99), Name: "smoke", NameSp: sp(92, 98),
				Body: &check.Block{Base: tb(99, 100, types.Unit), Stmts: []check.Expr{
					&check.Ascribe{Base: tb(100, 101, types.Int), Conv: true, X: &check.Var{Base: tb(100, 101, types.Int), Sym: xS}},
					&check.Apply{Base: check.Base{Sp: sp(102, 103)},
						Fn:   &check.Var{Base: check.Base{Sp: sp(102, 103)}, Sym: drawS},
						Call: check.CallInfo{Kind: check.CallPartial, Callee: drawS, Arity: 3, Applied: 1, Unapplied: 2, ArgToParam: []int{2}, HoleIndex: -1}},
				}}},
		},
	}
}

func TestDumpTyped(t *testing.T) {
	got := DumpTyped(typedSample())
	want := `(file@0:400 geom@8:12
  (import@2:7 "fmt" fmt%1 :go)
  (let@5:30
    (varpat@9:10 :Int x%2)
    (apply@13:30 :Int
      (call go callee=Put%3 arity=2 applied=2 hole=0 variadic)
      (var@13:16 :(Int -> Int) draw%4)
      (arg@17:18 (hole@17:18 :Int))
      (arg@19:24 ~to@19:22 (lit@23:24 :Int int 3))))
  (type@32:60 Shape%5
    (tparams a)
    (sum@40:60
      (variant@42:55 Circle%6
        (field@50:55 :Int radius -> radius%7 :i0)
        (field@56:58 :Float ~to :i1)
        (field@59:60 :Bool :i2))))
  (fn@62:90 draw%4 :(Int -> Int)
    (params (param ~to (varpat@65:66 :Int y%8)))
    (match@70:90 :Unit
      (member@10:20 :String Write@15:20 -> Write%10 :i-1 (var@10:14 :Buf b%9))
      (arm@72:88
        (ctorpat@72:80 :Shape Circle@72:78 -> Circle%6
          (carg ~v (varpat@79:80 :Int v%11))
          (fpat@79:80 radius -> radius%7 :i0 (wildpat@79:80 :Int))
          :rest)
        (guard (var@81:82 :Bool v%11))
        (list@85:86 :(List Int)
          (var@85:86 :Int x%2)
          (spread@85:86 :(List Int) (var@85:86 :(List Int) rest%12))))))
  (test@91:99 "smoke"@92:98
    (block@99:100 :Unit
      (ascribe@100:101 :Int :conv (var@100:101 :Int x%2))
      (apply@102:103 :?
        (call partial callee=draw%4 arity=3 applied=1 unapplied=2 argtoparam=[2])
        (var@102:103 :? draw%4))))
  (syms
    (sym %1 fmt module :pkg "fmt" :go)
    (sym %2 x var)
    (sym %3 Put func :pkg "fmt" :go)
    (sym %4 draw func)
    (sym %5 Shape type)
    (sym %6 Circle ctor)
    (sym %7 radius field)
    (sym %8 y var)
    (sym %9 b var)
    (sym %10 Write method :pkg "bytes" :go)
    (sym %11 v var)
    (sym %12 rest var)))
`
	if got != want {
		t.Errorf("DumpTyped mismatch:\n%s", got)
	}
}

// TestTypedDeterminism dumps the same typed file repeatedly — the
// interner must number symbols by first-encounter order only.
func TestTypedDeterminism(t *testing.T) {
	f := typedSample()
	want := DumpTyped(f)
	for i := 0; i < 10; i++ {
		if got := DumpTyped(f); got != want {
			t.Fatalf("dump %d differs:\n%s", i, got)
		}
	}
}
