// Command example exercises both ADT representations the way a Go
// program consuming generated Gom packages would: it constructs values,
// dispatches on them by hand, compares type sizes, and probes the
// invalid-value paths that Gom itself can never produce.
package main

import (
	"fmt"
	"unsafe"

	"github.com/podhmo/gom/experiments/p0/adt-repr/sealed"
	"github.com/podhmo/gom/experiments/p0/adt-repr/tagged"
)

// describe dispatches on a sealed.Expr like a Go consumer would: an
// ordinary type switch, the Go idiom for a closed set of types.
func describeSealed(e sealed.Expr) string {
	switch e := e.(type) {
	case sealed.Expr_Lit:
		return fmt.Sprintf("Lit %d", e.V0)
	case sealed.Expr_Add:
		return fmt.Sprintf("Add(%s, %s)", describeSealed(e.V0), describeSealed(e.V1))
	case sealed.Expr_Neg:
		return fmt.Sprintf("Neg(%s)", describeSealed(e.V0))
	default:
		return "<invalid>"
	}
}

// describeTagged does the same for tagged.Expr: a switch on the tag
// plus the generated As* accessors. There is no type switch to lean
// on, and forgetting a case is not a compile error either way.
func describeTagged(e tagged.Expr) string {
	switch e.Tag() {
	case tagged.ExprTag_Lit:
		return fmt.Sprintf("Lit %d", e.AsLit())
	case tagged.ExprTag_Add:
		l, r := e.AsAdd()
		return fmt.Sprintf("Add(%s, %s)", describeTagged(l), describeTagged(r))
	case tagged.ExprTag_Neg:
		return fmt.Sprintf("Neg(%s)", describeTagged(e.AsNeg()))
	default:
		return "<invalid>"
	}
}

// panicMessage runs f and reports what it panicked with.
func panicMessage(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return "no panic"
}

func main() {
	fmt.Println("== construction & dispatch ==")

	s1 := sealed.Expr_Add{V0: sealed.Expr_Lit{V0: 1}, V1: sealed.Expr_Neg{V0: sealed.Expr_Lit{V0: 2}}}
	t1 := tagged.Expr_Add(tagged.Expr_Lit(1), tagged.Expr_Neg(tagged.Expr_Lit(2)))
	fmt.Println("sealed:", describeSealed(s1), "=", sealed.Eval(s1))
	fmt.Println("tagged:", describeTagged(t1), "=", tagged.Eval(t1))

	fmt.Println("sealed Option:", sealed.GetOrElse(sealed.Option_Some[int64]{V0: 7}, 9),
		sealed.GetOrElse(sealed.Option[int64](sealed.Option_None[int64]{}), 9))
	fmt.Println("tagged Option:", tagged.GetOrElse(tagged.Option_Some[int64](7), 9),
		tagged.GetOrElse(tagged.Option_None[int64](), 9))

	xs := []int64{1, 2, 3, 4, 5}
	fmt.Println("sealed List sum:", sealed.Sum(sealed.ListFromSlice(xs)))
	fmt.Println("tagged List sum:", tagged.Sum(tagged.ListFromSlice(xs)))

	fmt.Println("\n== type sizes (bytes) ==")
	fmt.Printf("sealed.Expr interface header : %d\n", unsafe.Sizeof(sealed.Expr(nil)))
	fmt.Printf("sealed.Expr_Add struct      : %d\n", unsafe.Sizeof(sealed.Expr_Add{}))
	fmt.Printf("tagged.Expr struct          : %d\n", unsafe.Sizeof(tagged.Expr{}))
	fmt.Printf("sealed.Option[int64] iface  : %d\n", unsafe.Sizeof(sealed.Option[int64](nil)))
	fmt.Printf("tagged.Option[int64] struct : %d\n", unsafe.Sizeof(tagged.Option[int64]{}))
	fmt.Printf("sealed.Option[[64]int64]    : %d (payload boxed once)\n", unsafe.Sizeof(sealed.Option[[64]int64](nil)))
	fmt.Printf("tagged.Option[[64]int64]    : %d (payload inline)\n", unsafe.Sizeof(tagged.Option[[64]int64]{}))

	fmt.Println("\n== structural equality ==")
	a := sealed.Expr_Add{V0: sealed.Expr_Lit{V0: 1}, V1: sealed.Expr_Lit{V0: 2}}
	b := sealed.Expr_Add{V0: sealed.Expr_Lit{V0: 1}, V1: sealed.Expr_Lit{V0: 2}}
	fmt.Println("sealed  Add(Lit 1, Lit 2) == Add(Lit 1, Lit 2):", a == b)
	ta := tagged.Expr_Add(tagged.Expr_Lit(1), tagged.Expr_Lit(2))
	tb := tagged.Expr_Add(tagged.Expr_Lit(1), tagged.Expr_Lit(2))
	fmt.Println("tagged  Add(Lit 1, Lit 2) == Add(Lit 1, Lit 2):", ta == tb)

	fmt.Println("\n== invalid values from Go ==")
	var sNil sealed.Expr // nil interface: the only invalid sealed value
	fmt.Println("sealed  var e Expr (nil)          :", panicMessage(func() { sealed.Eval(sNil) }))
	var sTypedNil sealed.Expr = (*sealed.Expr_Lit)(nil) // typed nil pointer
	fmt.Println("sealed  (*Expr_Lit)(nil)          :", panicMessage(func() { sealed.Eval(sTypedNil) }))
	sNilChild := sealed.Expr_Add{V0: sealed.Expr_Lit{V0: 1}, V1: nil} // legal shape, nil child
	fmt.Println("sealed  Add{..., V1: nil}         :", panicMessage(func() { sealed.Eval(sNilChild) }))

	var tZero tagged.Expr // zero struct: tag == Invalid
	fmt.Println("tagged  var e Expr (zero)         :", panicMessage(func() { tagged.Eval(tZero) }))
	tZeroOpt := tagged.Option[int64]{}
	fmt.Println("tagged  Option[int64]{} (zero)    :", panicMessage(func() { tagged.GetOrElse(tZeroOpt, 9) }))
	tBadAccessor := tagged.Expr_Lit(1)
	fmt.Println("tagged  Lit(1).AsAdd()            :", panicMessage(func() { tBadAccessor.AsAdd() }))

	fmt.Println("\n== what Go consumers cannot write ==")
	fmt.Println("sealed: cannot add a new variant (unexported isExpr method)")
	fmt.Println("tagged: cannot write Expr{...} or corrupt the tag (unexported fields)")
}
