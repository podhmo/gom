package arity

import (
	"slices"
	"strconv"
	"testing"
)

func TestSaturatedCall(t *testing.T) {
	// gom: add3 1 2 3  (known function, all arguments -> direct call)
	if got := Add3(1, 2, 3); got != 6 {
		t.Fatalf("Add3(1,2,3) = %d, want 6", got)
	}
}

func TestPartialApplication(t *testing.T) {
	// gom: let g = add3 1
	//      let h = g 2
	//      h 3
	//
	// g : func(int, int) int, h : func(int) int — each step is one
	// closure allocation of the remaining arity.
	g := Add3_1(1)
	h := Apply1of2(g, 2)
	if got := h(3); got != 6 {
		t.Fatalf("add3 1 2 3 = %d, want 6", got)
	}
	// partial applications are reusable and independent
	if got := g(10, 20); got != 31 {
		t.Fatalf("add3 1 10 20 = %d, want 31", got)
	}
}

func TestValueForm(t *testing.T) {
	// gom: let f = add3 in f 1 2 3 — an unknown function value applied to
	// all arguments is still a direct call, because the remaining arity
	// is part of the static type.
	var f func(int, int, int) int = Add3
	if got := f(1, 2, 3); got != 6 {
		t.Fatalf("f 1 2 3 = %d, want 6", got)
	}
}

func TestFunctionReturningFunction(t *testing.T) {
	// gom: fn outer (a: Int) -> (Int -> Int -> Int) = ...
	// Declared arity 1 returning an arity-2 function nests naturally.
	outer := func(a int) func(int, int) int {
		return func(b, c int) int { return a + b + c }
	}
	if got := outer(1)(2, 3); got != 6 {
		t.Fatalf("outer 1 2 3 = %d, want 6", got)
	}
}

func TestDeclaredArityVsValueForm(t *testing.T) {
	// `mk_add` saturated at its declared arity and fully applied.
	if got := MkAdd(1)(2); got != 3 {
		t.Fatalf("mk_add 1 2 = %d, want 3", got)
	}
	// As a value of Gom type Int -> Int -> Int, the canonical form is
	// func(int, int) int — the flatten wrapper is used.
	var f func(int, int) int = MkAddFlat
	if got := f(1, 2); got != 3 {
		t.Fatalf("mk_add as flat value = %d, want 3", got)
	}
}

func TestLabeledPartial(t *testing.T) {
	// gom: let f = transfer ~amount: 10      // f : Int -> ~to:Int -> Int
	//      f 100 ~to: 5                      // = (100 - 5) + 10
	f := Transfer_Amount(10)
	if got := f(100, 5); got != 105 {
		t.Fatalf("transfer ~amount:10 then 100 ~to:5 = %d, want 105", got)
	}

	// gom: let g = transfer alice ~amount: 10   // g : ~to:Int -> Int
	//      g ~to: 5
	g := Transfer_From_Amount(100, 10)
	if got := g(5); got != 105 {
		t.Fatalf("transfer 100 ~amount:10 ~to:5 = %d, want 105", got)
	}
}

// TestLabeledEvalOrder verifies the generated code evaluates arguments in
// source order even when labels put them out of declaration order
// (docs/design.md 2.1, spec 2.4).
func TestLabeledEvalOrder(t *testing.T) {
	var order []string
	eval := func(name string, v int) int {
		order = append(order, name)
		return v
	}

	// gom: transfer ~amount:(eval "amount" 10) ~to:(eval "to" 5) (eval "from" 100)
	// Core IR binds temporaries in source order, then calls in
	// declaration order — identical in both representations.
	t1 := eval("amount", 10)
	t2 := eval("to", 5)
	t3 := eval("from", 100)
	got := Transfer(t3, t2, t1)

	if got != 105 {
		t.Fatalf("Transfer = %d, want 105", got)
	}
	want := []string{"amount", "to", "from"}
	if !slices.Equal(order, want) {
		t.Fatalf("eval order = %v, want %v", order, want)
	}
}

func TestMapAsValue(t *testing.T) {
	// gom: let m = map    with a = Int, b = String — instantiation only.
	// The Go expression Map[int, string] is already the value.
	m := Map[int, string]
	got := m(strconv.Itoa, []int{1, 2, 3})
	if !slices.Equal(got, []string{"1", "2", "3"}) {
		t.Fatalf("map as value = %v", got)
	}
}

func TestZipWithPartial(t *testing.T) {
	// gom: let z = zip_with add2 in z [1 2 3] [10 20 30]
	z := ZipWith_F[int, int, int](Add2)
	if got := z([]int{1, 2, 3}, []int{10, 20, 30}); !slices.Equal(got, []int{11, 22, 33}) {
		t.Fatalf("zip_with add2 = %v", got)
	}
}

func TestFold(t *testing.T) {
	if got := Fold(Add2, 0, []int{1, 2, 3, 4}); got != 10 {
		t.Fatalf("Fold = %d, want 10", got)
	}
	// and the same call goes through a stored value identically
	f := Add2
	if got := Fold(f, 0, []int{1, 2, 3, 4}); got != 10 {
		t.Fatalf("Fold via value = %d, want 10", got)
	}
}

func TestGoAPIInterop(t *testing.T) {
	// A Gom function value flows directly into a Go API that wants
	// func(A, B) R — no adapter.
	xs := []int{1, 3, 2}
	slices.SortFunc(xs, CmpDesc)
	if !slices.Equal(xs, []int{3, 2, 1}) {
		t.Fatalf("sorted = %v", xs)
	}
}
