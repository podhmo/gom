package list_test

// Boundary tests for the hand-written List representation
// (stdlib/list/types.go): the immutability contract at the slice
// boundary — the conversion helpers copy in both directions, so a
// Go caller's writes cannot reach into a Gom list and a Gom list
// cannot change under a Go callee's slice. Generated API surfaces
// pass a List verbatim instead — the no-write contract on that side
// is spec 8.7 (docs/decisions/2026-11-05-boundary-mutation-contract.md).

import (
	"testing"

	"github.com/podhmo/gom/stdlib/list"
	"github.com/podhmo/gom/stdlib/option"
)

// TestFromSliceIndependence mutates the source slice after the
// conversion — the List must stay the snapshot FromSlice took.
func TestFromSliceIndependence(t *testing.T) {
	xs := []int{1, 2, 3}
	l := list.FromSlice(xs)
	xs[0] = 99
	if l[0] != 1 {
		t.Fatalf("FromSlice did not copy: l[0] = %v after xs[0] = 99", l[0])
	}
	if got := list.Of(9); got[0] != 9 {
		t.Fatalf("sanity: Of(9)[0] = %v", got[0])
	}
}

// TestToSliceIndependence mutates the returned slice — writes
// through it must not reach the List's backing array.
func TestToSliceIndependence(t *testing.T) {
	l := list.FromSlice([]int{1, 2, 3})
	xs := l.ToSlice()
	xs[0] = 99
	if l[0] != 1 {
		t.Fatalf("ToSlice did not copy: l[0] = %v after xs[0] = 99", l[0])
	}
}

// TestNilListIsEmpty pins the zero-value contract: a nil List is
// the empty list — length 0, and the Gom-side helpers that can miss
// return None rather than panic.
func TestNilListIsEmpty(t *testing.T) {
	var l list.List[int]
	if !list.IsEmpty(l) {
		t.Fatal("IsEmpty(nil List) = false, want true")
	}
	if list.Length(l) != 0 {
		t.Fatalf("Length(nil List) = %v, want 0", list.Length(l))
	}
	if _, ok := list.Head(l).(option.Option_None[int]); !ok {
		t.Fatalf("Head(nil List) = %#v, want Option_None", list.Head(l))
	}
}
