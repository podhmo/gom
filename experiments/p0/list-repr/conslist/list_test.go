package conslist

import (
	"slices"
	"testing"
)

func TestSemantics(t *testing.T) {
	var zero List[int]
	if !zero.IsEmpty() || zero.Len() != 0 {
		t.Fatal("zero value is not the empty list")
	}

	l := Of(1, 2, 3)
	if l.Len() != 3 || l.Head() != 1 || l.At(2) != 3 {
		t.Fatalf("Of: got %v", l.ToSlice())
	}

	h, rest, ok := Uncons(l)
	if !ok || h != 1 || !slices.Equal(rest.ToSlice(), []int{2, 3}) {
		t.Fatalf("Uncons: got %v %v %v", h, rest.ToSlice(), ok)
	}

	l2 := Cons(0, l)
	if !slices.Equal(l2.ToSlice(), []int{0, 1, 2, 3}) {
		t.Fatalf("Cons: got %v", l2.ToSlice())
	}
	if !slices.Equal(l.ToSlice(), []int{1, 2, 3}) {
		t.Fatalf("Cons mutated receiver: %v", l.ToSlice())
	}

	if _, _, ok := Uncons(Nil[int]()); ok {
		t.Fatal("Uncons on empty list reported ok")
	}
}

// TestPersistence shows the defining property of the cons rep: two lists
// may share a tail and neither can observe the other's extension.
func TestPersistence(t *testing.T) {
	rest := Of(3, 4)
	a := Cons(1, rest)
	b := Cons(2, rest)
	if !slices.Equal(a.ToSlice(), []int{1, 3, 4}) {
		t.Fatalf("a: %v", a.ToSlice())
	}
	if !slices.Equal(b.ToSlice(), []int{2, 3, 4}) {
		t.Fatalf("b: %v", b.ToSlice())
	}
	if !slices.Equal(rest.ToSlice(), []int{3, 4}) {
		t.Fatalf("rest: %v", rest.ToSlice())
	}
}

func TestBoundaryCopyContract(t *testing.T) {
	src := []int{1, 2, 3}
	l := FromSlice(src)
	src[0] = 99 // structurally impossible to alias
	if l.Head() != 1 {
		t.Fatalf("FromSlice aliased input: %v", l.ToSlice())
	}
	out := l.ToSlice()
	out[0] = 42
	if l.Head() != 1 {
		t.Fatalf("ToSlice aliased list: %v", l.ToSlice())
	}
}

func TestMapFoldAppendReverse(t *testing.T) {
	l := Of(1, 2, 3, 4)
	if got := Map(l, func(x int) int { return x * 2 }).ToSlice(); !slices.Equal(got, []int{2, 4, 6, 8}) {
		t.Fatalf("Map: %v", got)
	}
	if sum := Fold(l, 0, func(a, x int) int { return a + x }); sum != 10 {
		t.Fatalf("Fold: %d", sum)
	}
	if got := Append(Of(1, 2), Of(3, 4)).ToSlice(); !slices.Equal(got, []int{1, 2, 3, 4}) {
		t.Fatalf("Append: %v", got)
	}
	if got := Reverse(l).ToSlice(); !slices.Equal(got, []int{4, 3, 2, 1}) {
		t.Fatalf("Reverse: %v", got)
	}
}
