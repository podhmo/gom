package fnrepr_test

// Go-side usage: this file is an external consumer of both generated
// packages, demonstrating what each representation looks like across a
// package boundary.

import (
	"fmt"
	"slices"

	"github.com/podhmo/gom/experiments/p0/fn-repr/arity"
	"github.com/podhmo/gom/experiments/p0/fn-repr/curried"
)

// Style B (arity): Gom function values are ordinary Go func types. Go
// callers pass plain functions — including funcs that did not come from
// Gom at all — and can partially apply with an ordinary closure.
func Example_arityFromGo() {
	// A plain Go func satisfies the Gom signature `fold : (Int -> Int ->
	// Int) -> Int -> List Int -> Int` directly.
	sum := arity.Fold(func(acc, x int) int { return acc + x }, 0, []int{1, 2, 3, 4})

	// `transfer` with a labeled argument skipped reads like an ordinary
	// closure from the Go side.
	awaitingTo := arity.Transfer_From_Amount(100, 10)
	balance := awaitingTo(5)

	// A Gom value of function type goes straight into a Go API.
	xs := []int{1, 3, 2}
	slices.SortFunc(xs, arity.CmpDesc)

	fmt.Println(sum, balance, xs)
	// Output: 10 105 [3 2 1]
}

// Style A (curried): the same call sites need nested function values. Go
// callers must either write nested literals or use the generated CurryN
// adapters; a Gom value needs UncurryN before it can enter a Go API.
func Example_curriedFromGo() {
	// Hand-written curried form, or the adapter around a flat func.
	add := func(a int) func(int) int { return func(x int) int { return a + x } }
	sum := curried.Fold(add, 0, []int{1, 2, 3, 4})
	sum2 := curried.Fold(curried.Curry2(func(acc, x int) int { return acc + x }), 0, []int{1, 2, 3, 4})

	// Passing a Go func into a curried-parameter API, and a curried Gom
	// value into a flat-parameter API, both need conversion.
	xs := []int{1, 3, 2}
	slices.SortFunc(xs, curried.Uncurry2(curried.CmpDescVal))

	fmt.Println(sum, sum2, xs)
	// Output: 10 10 [3 2 1]
}
