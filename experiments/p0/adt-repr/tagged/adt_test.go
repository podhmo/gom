package tagged

import (
	"strings"
	"testing"
)

func TestArea(t *testing.T) {
	if got := Area(Shape_Circle(1.0)); got != 3.14159 {
		t.Errorf("circle: got %v", got)
	}
	if got := Area(Shape_Rect(2.0, 3.0)); got != 6.0 {
		t.Errorf("rect: got %v", got)
	}
	if got := Area(Shape_Empty()); got != 0.0 {
		t.Errorf("empty: got %v", got)
	}
}

func TestEval(t *testing.T) {
	e := Expr_Add(Expr_Lit(1), Expr_Neg(Expr_Lit(2)))
	if got := Eval(e); got != -1 {
		t.Errorf("got %v, want -1", got)
	}
	if got := Eval(BuildTree(10)); got != 0 {
		t.Errorf("BuildTree(10): got %v, want 0", got)
	}
}

func TestGetOrElse(t *testing.T) {
	if got := GetOrElse(Option_Some[int64](7), 9); got != 7 {
		t.Errorf("Some: got %v", got)
	}
	if got := GetOrElse(Option_None[int64](), 9); got != 9 {
		t.Errorf("None: got %v", got)
	}
}

func TestSum(t *testing.T) {
	l := ListFromSlice([]int64{1, 2, 3})
	if got := Sum(l); got != 6 {
		t.Errorf("got %v, want 6", got)
	}
	if got := Sum(List_Nil[int64]()); got != 0 {
		t.Errorf("Nil: got %v, want 0", got)
	}
}

// Plain Go == on the tagged struct compares payload pointers by
// identity, so two separately constructed but equal trees do NOT
// compare equal. Structural equality would need a generated Equal
// method; this test pins down the raw behavior.
func TestEqualityIsNotStructural(t *testing.T) {
	a := Expr_Add(Expr_Lit(1), Expr_Lit(2))
	b := Expr_Add(Expr_Lit(1), Expr_Lit(2))
	if a == b {
		t.Error("== compares payload pointers; equal trees differ")
	}
	// ... but leaf variants with inline payloads do compare equal.
	if Expr_Lit(1) != Expr_Lit(1) {
		t.Error("Lit values must compare equal")
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

// There is no nil Expr: the closest a Go caller can get is the zero
// struct, whose tag is Invalid. The match panics with InvalidVariant.
func TestZeroExprPanics(t *testing.T) {
	var e Expr // zero struct, tag == ExprTag_Invalid
	r := mustPanic(t, func() { Eval(e) })
	msg := r.(error).Error()
	if !strings.Contains(msg, "tagged.Expr") {
		t.Errorf("panic message must name the type, got: %s", msg)
	}
	t.Logf("panic: %s", msg)
}

// Fields are unexported, so an outside caller cannot corrupt the tag;
// inside the package a corrupted tag is still possible and must panic.
func TestCorruptTagPanics(t *testing.T) {
	e := Expr{tag: ExprTag(99), lit: 1}
	r := mustPanic(t, func() { Eval(e) })
	t.Logf("panic: %v", r)
}

// A valid tag with a missing payload escapes the InvalidVariant check
// only if the consumer skips the tag switch and calls an accessor
// directly — generated match code never does this.
func TestAccessorOnWrongTagPanicsWithNilDeref(t *testing.T) {
	e := Expr_Lit(1)
	r := mustPanic(t, func() { e.AsAdd() })
	t.Logf("panic: %v", r)
}
