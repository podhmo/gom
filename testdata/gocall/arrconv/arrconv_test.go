package arrconv

import (
	"testing"

	"github.com/podhmo/gom/stdlib/list"
)

// Runtime check for the named-array `Go.conv` split: diagnostics and
// emitted Go are pinned by testdata/spec + testdata/golden; this test
// writes through the returned pointer and re-reads the source list —
// the composite shares the fresh ToSlice copy, never the List's
// backing.
func TestArrayPtrConvIsolatesList(t *testing.T) {
	xs := list.List[int64]{1, 2, 3, 4}
	p := MakeP(xs)
	if p[0] != 1 || p[1] != 2 || p[2] != 3 {
		t.Fatalf("MakeP = %v, want the first 3 elements of %v", *p, xs)
	}
	p[0] = -1
	if xs[0] != 1 {
		t.Fatalf("write through conv'd array pointer reached the source list: xs = %v", xs)
	}
}

func TestArrayValueConvCopiesPrefix(t *testing.T) {
	xs := list.List[int64]{1, 2, 3, 4}
	a := MakeA(xs)
	if a[0] != 1 || a[1] != 2 || a[2] != 3 {
		t.Fatalf("MakeA = %v, want the first 3 elements of %v", a, xs)
	}
	a[0] = -9 // mutating the returned array value must never reach xs
	if xs[0] != 1 {
		t.Fatalf("mutating the conv'd array value reached the source list: xs = %v", xs)
	}
}
