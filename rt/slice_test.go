package rt_test

import (
	"testing"

	"github.com/podhmo/gom/rt"
)

// The tests below pin the runtime half of spec section 8.5's
// `Go.Slice` member surface (decisions 2026-11-05-go-slice-ops).
// Nil and bounds semantics are Go's own: `length` is 0 on nil,
// `get` reports ok=false out of range, `slice` panics on bad
// bounds, and `to_list` is the only member that copies — the
// clone is the boundary conversion's (`list.FromSlice` at the
// generated call site), not `SliceToList`'s.

// TestSliceOps covers length/get/slice/fold/to_list over a plain
// slice — get's (v, ok) pair mirrors the Option shape generated
// code wraps.
func TestSliceOps(t *testing.T) {
	s := []int64{10, 20, 30}

	if got := rt.SliceLen(s); got != 3 {
		t.Fatalf("SliceLen = %d, want 3", got)
	}
	if v, ok := rt.SliceGet(s, 1); !ok || v != 20 {
		t.Fatalf("SliceGet(s, 1) = (%d, %v), want (20, true)", v, ok)
	}
	if _, ok := rt.SliceGet(s, 3); ok {
		t.Error("SliceGet(s, 3) ok = true, want false (past the end)")
	}
	if _, ok := rt.SliceGet(s, -1); ok {
		t.Error("SliceGet(s, -1) ok = true, want false (negative)")
	}

	sub := rt.SliceSub(s, 1, 3)
	if got := rt.SliceLen(sub); got != 2 {
		t.Fatalf("SliceLen(SliceSub(s, 1, 3)) = %d, want 2", got)
	}
	// The sub-view shares backing — Go's own slice semantics.
	sub[0] = 99
	if s[1] != 99 {
		t.Errorf("SliceSub view did not share backing: s[1] = %d, want 99", s[1])
	}
	s[1] = 20

	sum := rt.SliceFold(s, func(acc int64) func(int64) int64 {
		return func(v int64) int64 { return acc + v }
	}, 0)
	if sum != 60 {
		t.Fatalf("SliceFold sum = %d, want 60", sum)
	}

	// SliceToList is pass-through — it must not clone: the
	// generated call site wraps the result in list.FromSlice,
	// which snapshots, so an inner clone would be a second O(n)
	// copy for nothing.
	out := rt.SliceToList(s)
	if &out[0] != &s[0] {
		t.Error("SliceToList cloned the slice, want pass-through — the boundary conversion owns the snapshot")
	}
}

// TestSliceOpsNil: a nil slice reads as empty — length 0, get
// misses, fold returns the accumulator, to_list an empty slice.
func TestSliceOpsNil(t *testing.T) {
	var s []int64

	if got := rt.SliceLen(s); got != 0 {
		t.Fatalf("SliceLen(nil) = %d, want 0", got)
	}
	if _, ok := rt.SliceGet(s, 0); ok {
		t.Error("SliceGet(nil, 0) ok = true, want false")
	}
	if got := rt.SliceFold(s, func(acc int64) func(int64) int64 {
		return func(v int64) int64 { return acc + v }
	}, 5); got != 5 {
		t.Fatalf("SliceFold(nil, f, 5) = %d, want 5", got)
	}
	if got := rt.SliceToList(s); len(got) != 0 {
		t.Fatalf("SliceToList(nil) len = %d, want 0", len(got))
	}
	// nil[0:0] is legal Go — the empty sub-view of a nil slice.
	if got := rt.SliceSub(s, 0, 0); len(got) != 0 {
		t.Fatalf("SliceSub(nil, 0, 0) len = %d, want 0", len(got))
	}
}

// TestSliceSubPanics: out-of-range and inverted bounds panic like
// Go's own slice expression — no clamping, no Option.
func TestSliceSubPanics(t *testing.T) {
	s := []int64{1, 2}
	for _, c := range [][2]int64{{-1, 1}, {0, 3}, {2, 1}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("SliceSub(s, %d, %d) did not panic", c[0], c[1])
				}
			}()
			rt.SliceSub(s, c[0], c[1])
		}()
	}
}

// TestSliceFoldOrder pins the left fold: index order, acc first —
// `f(acc)(v)` as in the Gom `b -> a -> b` shape.
func TestSliceFoldOrder(t *testing.T) {
	got := rt.SliceFold([]int64{1, 2, 3}, func(acc string) func(int64) string {
		return func(v int64) string { return acc + " " + string(rune('0'+v)) }
	}, "x")
	if got != "x 1 2 3" {
		t.Fatalf("SliceFold order = %q, want %q", got, "x 1 2 3")
	}
}
