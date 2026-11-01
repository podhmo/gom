// Package example shows each List representation from the side Go
// callers see: constructing, iterating, destructuring with [x, ..rest],
// and crossing the boundary to an ordinary Go API that takes []int.
package example

import (
	"fmt"

	"github.com/podhmo/gom/experiments/p0/list-repr/conslist"
	"github.com/podhmo/gom/experiments/p0/list-repr/slicelist"
)

// sumSlice is a stand-in for a pre-existing Go API: it takes a plain
// slice, knows nothing about Gom, and may keep or mutate what it gets.
func sumSlice(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

// The slice representation is a slice: Go callers use len(), indexing
// and range directly, and only conversions at the boundary need care.
func Example_sliceRep() {
	l := slicelist.Of(1, 2, 3)

	// len(), indexing and range work without any wrapper.
	fmt.Println(len(l), l[0])
	total := 0
	for _, x := range l {
		total += x
	}
	fmt.Println(total)

	// [x, ..rest] destructuring is a slice header split: O(1).
	head, rest, ok := slicelist.Uncons(l)
	fmt.Println(head, rest, ok)

	// [x, ..rest] construction copies: O(n).
	fmt.Println(slicelist.Cons(0, l))

	// Boundary: both directions copy, so a Go callee can never mutate
	// the list, and a later write to `data` cannot leak in.
	out := sumSlice(l.ToSlice())
	data := []int{4, 5}
	in := slicelist.FromSlice(data)
	data[0] = 99
	fmt.Println(out, in)

	// Output:
	// 3 1
	// 6
	// 1 [2 3] true
	// [0 1 2 3]
	// 6 [4 5]
}

// The cons representation is opaque: Go callers lose len()/range and go
// through methods and the iter.Seq iterator instead. Conversions still
// copy, but they can never alias by construction.
func Example_consRep() {
	l := conslist.Of(1, 2, 3)

	// No len() builtin or indexing; Len is O(n), At is O(i).
	fmt.Println(l.Len())

	total := 0
	for x := range l.All() {
		total += x
	}
	fmt.Println(total)

	// [x, ..rest] both ways are O(1) and share structure.
	head, rest, ok := conslist.Uncons(l)
	fmt.Println(head, rest.Len(), ok)
	fmt.Println(conslist.Cons(0, l).Len())

	// Boundary: copies are mandatory and structural, so aliasing is
	// impossible rather than merely forbidden.
	out := sumSlice(l.ToSlice())
	data := []int{4, 5}
	in := conslist.FromSlice(data)
	data[0] = 99
	fmt.Println(out, in.Len(), in.Head())

	// Output:
	// 3
	// 6
	// 1 2 true
	// 4
	// 6 2 4
}

// A Gom-side recursive function the way generated code might write it:
// sum over a list by repeatedly matching [x, ..rest].
func Example_recursiveWalk() {
	var sumSliceRep func(l slicelist.List[int]) int
	sumSliceRep = func(l slicelist.List[int]) int {
		if h, rest, ok := slicelist.Uncons(l); ok {
			return h + sumSliceRep(rest)
		}
		return 0
	}
	var sumConsRep func(l conslist.List[int]) int
	sumConsRep = func(l conslist.List[int]) int {
		if h, rest, ok := conslist.Uncons(l); ok {
			return h + sumConsRep(rest)
		}
		return 0
	}
	fmt.Println(sumSliceRep(slicelist.Of(1, 2, 3)), sumConsRep(conslist.Of(1, 2, 3)))
	// Output:
	// 6 6
}
