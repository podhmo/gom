package declslice

import "testing"

// Go-side assertions for `~[]E` over a declared element: the
// generated package's own `Rec` flows through `slices.SortFunc`
// (with a `Go.func2` cmp) and `slices.Reverse`.
func TestSortFuncDeclaredElem(t *testing.T) {
	rs := []Rec{{Name: "c", N: 3}, {Name: "a", N: 1}, {Name: "b", N: 2}}
	SortByN(rs)
	if rs[0].N != 1 || rs[1].N != 2 || rs[2].N != 3 {
		t.Fatalf("SortByN = %v, want n ascending", rs)
	}
}

func TestReverseDeclaredElem(t *testing.T) {
	rs := []Rec{{Name: "a", N: 1}, {Name: "b", N: 2}}
	Rev(rs)
	if rs[0].N != 2 || rs[1].N != 1 {
		t.Fatalf("Rev = %v, want reversed", rs)
	}
}
