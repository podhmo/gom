package deftilde

import (
	"testing"
)

// The generated package must compile `Clamp64[UserId]` — a defined
// type under a `~int64` union term — and the Go side calls it like
// any other function.
func TestClampIdOnDefinedType(t *testing.T) {
	if got := ClampId(UserId(5), UserId(1), UserId(10)); got != UserId(5) {
		t.Fatalf("ClampId = %v, want 5", got)
	}
	// A value above the bound pins the argument order: Clamp64 is
	// (x, lo, hi), so this must return hi=10, not lo=20.
	if got := ClampId(UserId(20), UserId(1), UserId(10)); got != UserId(10) {
		t.Fatalf("ClampId = %v, want 10", got)
	}
}
