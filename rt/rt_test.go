package rt

import (
	"testing"

	"github.com/podhmo/gom/stdlib/list"
)

// TestEqualNilEmptyList: a nil list.List is the empty list — the
// representation contract in stdlib/list — and must compare equal to
// `[]`'s non-nil empty literal even though reflect.DeepEqual keeps
// them apart (eval's element-wise comparison answers true).
func TestEqualNilEmptyList(t *testing.T) {
	var nilList list.List[int64]
	if !Equal(nilList, list.List[int64]{}) {
		t.Error("Equal(nil list, [] list) = false, want true")
	}
	if !Equal(list.List[int64]{}, nilList) {
		t.Error("Equal([] list, nil list) = false, want true")
	}
	if !Equal(nilList, nilList) {
		t.Error("Equal(nil list, nil list) = false, want true")
	}
}

// TestEqualStillStructural: the DeepEqual reading stays the answer
// everywhere else — different lengths, different elements, and
// different types keep answering false.
func TestEqualStillStructural(t *testing.T) {
	if Equal(list.List[int64]{1}, list.List[int64]{}) {
		t.Error("Equal([1], []) = true, want false")
	}
	if Equal(list.List[int64]{1}, list.List[int64]{2}) {
		t.Error("Equal([1], [2]) = true, want false")
	}
	if !Equal(list.List[int64]{1, 2}, list.List[int64]{1, 2}) {
		t.Error("Equal([1 2], [1 2]) = false, want true")
	}
	var nilList list.List[int64]
	if Equal(nilList, []int64{}) {
		t.Error("Equal(list.List nil, []int64 {}) = true, want false — different types")
	}
}
