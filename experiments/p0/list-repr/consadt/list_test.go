package consadt

import (
	"slices"
	"testing"
)

func TestSemantics(t *testing.T) {
	l := Of(1, 2, 3)
	if Len[int](l) != 3 {
		t.Fatalf("Len: %d", Len[int](l))
	}
	h, rest, ok := Uncons[int](l)
	if !ok || h != 1 || !slices.Equal(ToSlice[int](rest), []int{2, 3}) {
		t.Fatalf("Uncons: %v %v %v", h, ToSlice[int](rest), ok)
	}
	l2 := Cons(0, l)
	if !slices.Equal(ToSlice[int](l2), []int{0, 1, 2, 3}) {
		t.Fatalf("Cons: %v", ToSlice[int](l2))
	}
	if _, _, ok := Uncons[int](Nil[int]()); ok {
		t.Fatal("Uncons on Nil reported ok")
	}
}

// TestNilIsInvalidVariant: unlike the other two representations, the
// zero value of the interface type is not a valid list. Matching it
// panics as a contract violation (design.md 4.1, rt.InvalidVariant).
func TestNilIsInvalidVariant(t *testing.T) {
	var l List[int] // nil interface: constructible from Go, never from Gom
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on nil List")
		}
	}()
	Uncons[int](l)
}

// TestNoMutationHazard documents a bonus of the interface encoding:
// destructuring copies the variant struct out of its box, so even though
// the fields are exported, a Go caller's writes only touch its own copy.
// Shared cells cannot be corrupted from Go — the same guarantee the
// opaque conslist form gets from unexported fields.
func TestNoMutationHazard(t *testing.T) {
	rest := Of(2, 3)
	a := Cons(1, rest)
	c := rest.(List_Cons[int])
	c.V0 = 99 // mutates only the copy pulled out of the box
	if got := ToSlice[int](a); got[1] != 2 {
		t.Fatalf("shared cell changed: %v", got)
	}
}

func TestMapFold(t *testing.T) {
	l := Of(1, 2, 3, 4)
	if got := ToSlice[int](Map(l, func(x int) int { return x * 2 })); !slices.Equal(got, []int{2, 4, 6, 8}) {
		t.Fatalf("Map: %v", got)
	}
	if sum := Fold(l, 0, func(a, x int) int { return a + x }); sum != 10 {
		t.Fatalf("Fold: %d", sum)
	}
}
