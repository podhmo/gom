package sealed

import (
	"strings"
	"testing"
)

func TestArea(t *testing.T) {
	if got := Area(Shape_Circle{Radius: 1.0}); got != 3.14159 {
		t.Errorf("circle: got %v", got)
	}
	if got := Area(Shape_Rect{W: 2.0, H: 3.0}); got != 6.0 {
		t.Errorf("rect: got %v", got)
	}
	if got := Area(Shape_Empty{}); got != 0.0 {
		t.Errorf("empty: got %v", got)
	}
}

func TestEval(t *testing.T) {
	e := Expr_Add{V0: Expr_Lit{V0: 1}, V1: Expr_Neg{V0: Expr_Lit{V0: 2}}}
	if got := Eval(e); got != -1 {
		t.Errorf("got %v, want -1", got)
	}
	if got := Eval(BuildTree(10)); got != 0 {
		t.Errorf("BuildTree(10): got %v, want 0", got)
	}
}

func TestGetOrElse(t *testing.T) {
	if got := GetOrElse(Option_Some[int64]{V0: 7}, 9); got != 7 {
		t.Errorf("Some: got %v", got)
	}
	if got := GetOrElse(Option[int64](Option_None[int64]{}), 9); got != 9 {
		t.Errorf("None: got %v", got)
	}
}

func TestSum(t *testing.T) {
	l := ListFromSlice([]int64{1, 2, 3})
	if got := Sum(l); got != 6 {
		t.Errorf("got %v, want 6", got)
	}
	var nil_ List[int64] = List_Nil[int64]{}
	if got := Sum(nil_); got != 0 {
		t.Errorf("Nil: got %v, want 0", got)
	}
}

// Structural == comes for free: two separately constructed but equal
// trees compare equal because every variant struct is comparable and
// interface equality compares the boxed values.
func TestEquality(t *testing.T) {
	a := Expr_Add{V0: Expr_Lit{V0: 1}, V1: Expr_Lit{V0: 2}}
	b := Expr_Add{V0: Expr_Lit{V0: 1}, V1: Expr_Lit{V0: 2}}
	if a != b {
		t.Error("equal trees must compare equal")
	}
	c := Expr_Add{V0: Expr_Lit{V0: 1}, V1: Expr_Lit{V0: 3}}
	if a == c {
		t.Error("different trees must not compare equal")
	}
}

// mustPanic runs f and returns the recovered panic value, or fails.
func mustPanic(t *testing.T, f func()) any {
	t.Helper()
	var r any
	func() {
		defer func() { r = recover() }()
		f()
	}()
	if r == nil {
		t.Fatal("expected panic, got none")
	}
	return r
}

// A Go caller can hand Gom code a nil interface. The match must panic
// with the InvalidVariant contract violation, not "unreachable".
func TestNilExprPanics(t *testing.T) {
	var e Expr // nil interface
	r := mustPanic(t, func() { Eval(e) })
	msg := r.(error).Error()
	if !strings.Contains(msg, "sealed.Expr") {
		t.Errorf("panic message must name the type, got: %s", msg)
	}
	t.Logf("panic: %s", msg)
}

// A typed nil pointer also lands in the default arm: *Expr_Lit
// satisfies the interface (value receiver) but is not the declared
// value-struct variant, so the type switch rejects it.
func TestTypedNilPointerPanics(t *testing.T) {
	var e Expr = (*Expr_Lit)(nil)
	r := mustPanic(t, func() { Eval(e) })
	t.Logf("panic: %v", r)
}

// A variant can legally be constructed with a nil child; the contract
// violation surfaces at the first match that reaches the child.
func TestNilChildPanicsAtMatch(t *testing.T) {
	e := Expr_Add{V0: Expr_Lit{V0: 1}, V1: nil}
	r := mustPanic(t, func() { Eval(e) })
	t.Logf("panic: %v", r)
}
