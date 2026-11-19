package iterdefer_test

// The Go consumer half of the deferred-iterator fixture: a `for`
// whose iterable is a claimed meta waits for the committed
// `Go.Slice`/`List` pair and copies at the boundary. staleSum pins
// the read-time copy (a mutation between capture and iteration is
// invisible); freshSum re-reads and sees it; nilSum covers the nil
// `[]int64` → empty `List` edge; viaCallResult covers the deferred
// apply result copying back to `[]int64`.

import (
	"testing"

	"example.com/gocall/box"
	"example.com/gocall/iterdefer"
)

func TestStaleSum(t *testing.T) {
	box.Reset()
	// Xs is {10, 20, 30}; the copy captured before MutateX still
	// sums the original backing.
	if got := iterdefer.StaleSum(struct{}{}); got != 60 {
		t.Fatalf("staleSum = %v, want 60", got)
	}
	// A fresh read sees the Go-side write: 99 + 20 + 30.
	if got := iterdefer.FreshSum(struct{}{}); got != 149 {
		t.Fatalf("freshSum = %v, want 149", got)
	}
}

func TestNilSum(t *testing.T) {
	if got := iterdefer.NilSum(struct{}{}); got != 0 {
		t.Fatalf("nilSum = %v, want 0", got)
	}
}

func TestViaCallResult(t *testing.T) {
	if got := iterdefer.ViaCallResult(struct{}{}); got != 3 {
		t.Fatalf("viaCallResult = %v, want 3", got)
	}
}
