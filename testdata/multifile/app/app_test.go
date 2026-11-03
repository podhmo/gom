package app_test

// The Go consumer of the generated package: the .gomi's public
// surface arrives as ordinary Go — the ADT as a sealed interface
// plus variant structs, the functions as flat Go functions, the
// function value as a curried function value.

import (
	"math"
	"testing"

	"example.com/multifile/shapes"
)

func TestAreaFromGo(t *testing.T) {
	c := shapes.Shape_Circle{Radius: 2.0}
	if got := shapes.Area(c); math.Abs(got-12.56636) > 1e-3 {
		t.Fatalf("Area(Circle 2) = %v, want ~12.566", got)
	}
	r := shapes.Shape_Rect{W: 2.0, H: 3.0}
	if got := shapes.Area(r); got != 6.0 {
		t.Fatalf("Area(Rect 2 3) = %v, want 6", got)
	}
}

func TestFunctionValueFromGo(t *testing.T) {
	// scale is a flat Go function; a bare reference adapts it to
	// the curried value shape only where the Gom side needs one —
	// from Go both call forms are plain calls.
	double := func(x float64) float64 { return x * 2.0 }
	s := shapes.Scale(double, shapes.Shape_Circle{Radius: 1.0})
	if got := shapes.Area(s); math.Abs(got-12.56636) > 1e-3 {
		t.Fatalf("Area(Scale double (Circle 1)) = %v, want ~12.566", got)
	}
	// twice — a `let`-declared function — is a generic Go func.
	if got := shapes.Twice(func(n int64) int64 { return n + 1 }, int64(0)); got != 2 {
		t.Fatalf("Twice inc 0 = %v, want 2", got)
	}
	// tau — an exported `let` member — is a package-level Go var.
	if shapes.Tau < 6.28 || shapes.Tau > 6.29 {
		t.Fatalf("shapes.Tau = %v, want ~6.283", shapes.Tau)
	}
}
