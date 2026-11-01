package sealed

import "github.com/podhmo/gom/experiments/p0/adt-repr/rt"

// type Expr =
//
//	| Lit Int
//	| Add Expr Expr
//	| Neg Expr
//
// Recursive positions hold the interface itself; a child is boxed once
// when constructed and then passed around as a two-word interface value.
type Expr interface{ isExpr() }

type Expr_Lit struct{ V0 int64 }
type Expr_Add struct{ V0, V1 Expr }
type Expr_Neg struct{ V0 Expr }

func (Expr_Lit) isExpr() {}
func (Expr_Add) isExpr() {}
func (Expr_Neg) isExpr() {}

// fn eval (e: Expr) -> Int
func Eval(e Expr) int64 {
	switch e := e.(type) {
	case Expr_Lit:
		return e.V0
	case Expr_Add:
		return Eval(e.V0) + Eval(e.V1)
	case Expr_Neg:
		return -Eval(e.V0)
	default:
		panic(rt.InvalidVariant("sealed.Expr", e))
	}
}

// BuildTree returns Add(BuildTree(d-1), Neg(BuildTree(d-1))) and a Lit
// leaf at depth 0, so Eval(BuildTree(d)) == 0 for any d > 0.
func BuildTree(depth int) Expr {
	if depth <= 0 {
		return Expr_Lit{V0: 1}
	}
	return Expr_Add{
		V0: BuildTree(depth - 1),
		V1: Expr_Neg{V0: BuildTree(depth - 1)},
	}
}
