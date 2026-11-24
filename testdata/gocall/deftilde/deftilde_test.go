package deftilde

import (
	"testing"
)

// The generated package must compile `Clamp64[UserId]` — a defined
// type under a `~int64` union term — and the Go side calls it like
// any other function.
func TestClampIdOnDefinedType(t *testing.T) {
	if got := ClampId(UserId(1), UserId(5), UserId(10)); got != UserId(5) {
		t.Fatalf("ClampId = %v, want 5", got)
	}
	if got := ClampId(UserId(1), UserId(-2), UserId(10)); got != UserId(1) {
		t.Fatalf("ClampId = %v, want 1", got)
	}
}
