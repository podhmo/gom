package tagged

import "github.com/podhmo/gom/experiments/p0/adt-repr/rt"

// type Expr =
//
//	| Lit Int
//	| Add Expr Expr
//	| Neg Expr
//
// A Go struct cannot contain itself, so any variant whose payload
// mentions Expr stores its payload behind a pointer.
type Expr struct {
	tag ExprTag
	lit int64    // Lit's payload, stored inline
	add *exprAdd // Add's payload; pointer because it contains Expr
	neg *Expr    // Neg's payload; pointer because it is an Expr
}

type exprAdd struct{ v0, v1 Expr }

type ExprTag uint8

const (
	ExprTag_Invalid ExprTag = iota // zero value: not a real variant
	ExprTag_Lit
	ExprTag_Add
	ExprTag_Neg
)

// Constructors, one per variant.

func Expr_Lit(v int64) Expr {
	return Expr{tag: ExprTag_Lit, lit: v}
}
func Expr_Add(l, r Expr) Expr {
	return Expr{tag: ExprTag_Add, add: &exprAdd{v0: l, v1: r}}
}
func Expr_Neg(x Expr) Expr {
	return Expr{tag: ExprTag_Neg, neg: &x}
}

// Tag and destructuring accessors.

func (e Expr) Tag() ExprTag        { return e.tag }
func (e Expr) AsLit() int64        { return e.lit }
func (e Expr) AsAdd() (Expr, Expr) { return e.add.v0, e.add.v1 }
func (e Expr) AsNeg() Expr         { return *e.neg }

// fn eval (e: Expr) -> Int
func Eval(e Expr) int64 {
	switch e.Tag() {
	case ExprTag_Lit:
		return e.AsLit()
	case ExprTag_Add:
		l, r := e.AsAdd()
		return Eval(l) + Eval(r)
	case ExprTag_Neg:
		return -Eval(e.AsNeg())
	default:
		panic(rt.InvalidVariant("tagged.Expr", e))
	}
}

// BuildTree returns Add(BuildTree(d-1), Neg(BuildTree(d-1))) and a Lit
// leaf at depth 0, so Eval(BuildTree(d)) == 0 for any d > 0.
func BuildTree(depth int) Expr {
	if depth <= 0 {
		return Expr_Lit(1)
	}
	return Expr_Add(BuildTree(depth-1), Expr_Neg(BuildTree(depth-1)))
}
