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

// TestOptionToList pins the layering conversion that backs
// `Option.to_list` — surfaced on the Option module but living in
// this package (decision stdlib-conv-placement).
func TestOptionToList(t *testing.T) {
	one := list.OptionToList(option.Option_Some[int]{V0: 7})
	if len(one) != 1 || one[0] != 7 {
		t.Fatalf("OptionToList(Some 7) = %#v, want [7]", one)
	}
	empty := list.OptionToList(option.Option_None[int]{})
	if len(empty) != 0 {
		t.Fatalf("OptionToList(None) = %#v, want []", empty)
	}
}

func TestOptionToListInvalid(t *testing.T) {
	// A nil Option is not None — the conversion panics like a
	// match's default arm.
	defer func() {
		if recover() == nil {
			t.Fatal("OptionToList(nil) did not panic")
		}
	}()
	list.OptionToList[int](nil)
}

func TestListViewSub(t *testing.T) {
	xs := list.List[int64]{1, 2, 3, 4}
	v := list.ListSub(xs, 1, 3)
	// Zero-copy: the view aliases the list's backing array, so the
	// first view element is the same cell as xs[1] — a copying
	// implementation would fail this.
	if &v[0] != &xs[1] {
		t.Fatal("ListSub: view does not share the list's backing")
	}
	if got := list.ViewLen(v); got != 2 {
		t.Fatalf("len: got %d", got)
	}
	if x, ok := list.ViewGet(v, 1); !ok || x != 3 {
		t.Fatalf("get 1: got %v %v", x, ok)
	}
	if _, ok := list.ViewGet(v, 2); ok {
		t.Fatal("get 2: want not-ok")
	}
	w := list.ViewSub(v, 0, 1)
	if x, ok := list.ViewGet(w, 0); !ok || x != 2 {
		t.Fatalf("sub get: got %v %v", x, ok)
	}
	// The view reads the list's backing — no copy: share proven by
	// ViewToList returning the same underlying array's subrange.
	l := list.ViewToList(v)
	if &l[0] != &xs[1] {
		t.Fatal("ViewToList: does not share the backing")
	}
	if len(l) != 2 || l[0] != 2 || l[1] != 3 {
		t.Fatalf("to_list: got %v", l)
	}
	if got := list.ViewFold(v, func(acc, x int64) int64 { return acc + x }, 0); got != 5 {
		t.Fatalf("fold: got %d", got)
	}
	if got := list.ViewLen(list.ListView(xs)); got != 4 {
		t.Fatalf("ListView len: got %d", got)
	}
}
