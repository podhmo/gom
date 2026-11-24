package deflit

import (
	"testing"
	"time"
)

// The generated package compiles the materialized literals
// (`UserId(0)`, `I32(-2147483648)`) and the Go side calls the
// functions like any others.
func TestLiteralAtDeclaredType(t *testing.T) {
	if got := ClampIdLit(UserId(200)); got != UserId(100) {
		t.Fatalf("ClampIdLit = %v, want 100", got)
	}
	if got := BumpId(UserId(7)); got != UserId(7) {
		t.Fatalf("BumpId = %v, want 7", got)
	}
	if got := IdBound(struct{}{}); got != UserId(0) {
		t.Fatalf("IdBound = %v, want 0", got)
	}
	if got := NegI32(struct{}{}); got != I32(-2147483648) {
		t.Fatalf("NegI32 = %v, want -2147483648", got)
	}
	if got := Elapse(Span(time.Second)); got != Span(time.Second) {
		t.Fatalf("Elapse = %v, want 1s", got)
	}
	if !EqCount(Count[int64](3)) {
		t.Fatal("EqCount = false, want true")
	}
	if got := Unwrap(UserId(42)); got != 42 {
		t.Fatalf("Unwrap = %v, want 42", got)
	}
}
