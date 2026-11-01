package slicelist

import (
	"slices"
	"testing"
)

func TestSemantics(t *testing.T) {
	l := Of(1, 2, 3)
	if len(l) != 3 || l[0] != 1 || l[2] != 3 {
		t.Fatalf("Of: got %v", []int(l))
	}

	h, rest, ok := Uncons(l)
	if !ok || h != 1 || !slices.Equal(rest, List[int]{2, 3}) {
		t.Fatalf("Uncons: got %v %v %v", h, []int(rest), ok)
	}

	l2 := Cons(0, l)
	if !slices.Equal(l2, List[int]{0, 1, 2, 3}) {
		t.Fatalf("Cons: got %v", []int(l2))
	}
	// Cons must not disturb the original.
	if !slices.Equal(l, List[int]{1, 2, 3}) {
		t.Fatalf("Cons mutated receiver: %v", []int(l))
	}

	if _, _, ok := Uncons(Nil[int]()); ok {
		t.Fatal("Uncons on empty list reported ok")
	}
}

func TestBoundaryCopyContract(t *testing.T) {
	// FromSlice copies: mutating the caller's slice must not change the List.
	src := []int{1, 2, 3}
	l := FromSlice(src)
	src[0] = 99
	if l[0] != 1 {
		t.Fatalf("FromSlice aliased input: %v", []int(l))
	}

	// ToSlice copies: mutating the result must not change the List.
	out := l.ToSlice()
	out[0] = 99
	if l[0] != 1 {
		t.Fatalf("ToSlice aliased list: %v", []int(l))
	}
}

// TestAliasingHazard documents what the boundary copy prevents: the raw
// conversions alias, and mutation crosses the boundary in both directions.
func TestAliasingHazard(t *testing.T) {
	src := []int{1, 2, 3}
	l := WrapUnchecked(src)
	src[0] = 99 // caller mutates after handing the slice to Gom
	if l[0] != 99 {
		t.Fatalf("expected aliasing, got %v", []int(l))
	}

	back := l.SliceUnchecked()
	back[1] = 42 // callee mutates the "immutable" list
	if l[1] != 42 {
		t.Fatalf("expected aliasing, got %v", []int(l))
	}
}

// TestTailSharesBacking shows that ..rest retains the whole array:
// Tail's capacity reaches back into the original array object.
func TestTailSharesBacking(t *testing.T) {
	l := Of(1, 2, 3, 4)
	rest := l.Tail()
	if cap(rest) != cap(l)-1 {
		t.Fatalf("expected shared backing: cap(rest)=%d cap(l)=%d", cap(rest), cap(l))
	}
	freed := l.TailCopy()
	if cap(freed) != len(freed) {
		t.Fatalf("TailCopy should right-size: cap=%d len=%d", cap(freed), len(freed))
	}
}

func TestMapFoldAppend(t *testing.T) {
	l := Of(1, 2, 3, 4)
	doubled := Map(l, func(x int) int { return x * 2 })
	if !slices.Equal(doubled, List[int]{2, 4, 6, 8}) {
		t.Fatalf("Map: %v", []int(doubled))
	}
	sum := Fold(l, 0, func(a, x int) int { return a + x })
	if sum != 10 {
		t.Fatalf("Fold: %d", sum)
	}
	cat := Append(Of(1, 2), Of(3, 4))
	if !slices.Equal(cat, List[int]{1, 2, 3, 4}) {
		t.Fatalf("Append: %v", []int(cat))
	}
}
