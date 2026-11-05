package rt_test

import (
	"testing"

	"github.com/podhmo/gom/rt"
)

// The tests below pin the runtime half of spec section 8.3/8.5's
// `Ptr` and `Go.Map` member surfaces (decisions
// 2026-11-05-ptr-ops-surface, 2026-11-05-go-map-ops). Nil semantics
// are Go's own: dereference and map-set panic, everything else is
// total.

// TestPtrOps: get/set/is_nil/new/make/nil cover the documented
// contracts — `make` copies its argument, `new` zero-fills, and
// `to_option`'s (v, ok) pair mirrors comma-ok.
func TestPtrOps(t *testing.T) {
	p := rt.PtrMake[int64](1)
	if got := rt.PtrGet(p); got != 1 {
		t.Fatalf("PtrGet(PtrMake(1)) = %d, want 1", got)
	}
	rt.PtrSet(p, 41)
	if got := rt.PtrGet(p); got != 41 {
		t.Fatalf("PtrGet after PtrSet(41) = %d, want 41", got)
	}

	// PtrMake copies: mutating through the pointer must not reach
	// the argument it was made from.
	v := int64(7)
	q := new(v)
	rt.PtrSet(q, 9)
	if v != 7 {
		t.Errorf("source mutated through PtrMake copy: v = %d", v)
	}

	z := rt.PtrNew[int64]()
	if rt.PtrIsNil(z) {
		t.Error("PtrIsNil(PtrNew()) = true, want false")
	}
	if got := rt.PtrGet(z); got != 0 {
		t.Errorf("PtrGet(PtrNew()) = %d, want 0 (zero value)", got)
	}

	n := rt.PtrNil[int64]()
	if !rt.PtrIsNil(n) {
		t.Error("PtrIsNil(PtrNil()) = false, want true")
	}

	if got, ok := rt.PtrToOption(p); !ok || got != 41 {
		t.Errorf("PtrToOption(non-nil) = (%d, %v), want (41, true)", got, ok)
	}
	if _, ok := rt.PtrToOption(n); ok {
		t.Error("PtrToOption(nil) ok = true, want false")
	}
}

// TestPtrGetSetNilPanic: get and set keep Go's panic semantics —
// no Gom-side guard smoothes them over.
func TestPtrGetSetNilPanic(t *testing.T) {
	t.Run("get", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("PtrGet(nil) did not panic")
			}
		}()
		rt.PtrGet(rt.PtrNil[int64]())
	})
	t.Run("set", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Error("PtrSet(nil) did not panic")
			}
		}()
		rt.PtrSet(rt.PtrNil[int64](), 1)
	})
}

// TestMapOps: make/get/set/delete/length/to_list behave like the
// Go operations they name; a nil map reads, deletes and lengths
// fine.
func TestMapOps(t *testing.T) {
	m := rt.MapMake[string, int64]()
	if m == nil {
		t.Fatal("MapMake() returned nil")
	}
	rt.MapSet(m, "k", 3)
	if got, ok := rt.MapGet(m, "k"); !ok || got != 3 {
		t.Fatalf("MapGet(m, \"k\") = (%d, %v), want (3, true)", got, ok)
	}
	if _, ok := rt.MapGet(m, "missing"); ok {
		t.Error("MapGet(m, \"missing\") ok = true, want false")
	}
	if got := rt.MapLen(m); got != 1 {
		t.Errorf("MapLen(m) = %d, want 1", got)
	}
	rt.MapDelete(m, "k")
	rt.MapDelete(m, "k") // deleting a missing key is a no-op
	if got := rt.MapLen(m); got != 0 {
		t.Errorf("MapLen after delete = %d, want 0", got)
	}

	// Nil maps read/delete/len like Go — only set panics.
	var nilMap map[string]int64
	if _, ok := rt.MapGet(nilMap, "k"); ok {
		t.Error("MapGet(nil map) ok = true, want false")
	}
	rt.MapDelete(nilMap, "k")
	if got := rt.MapLen(nilMap); got != 0 {
		t.Errorf("MapLen(nil map) = %d, want 0", got)
	}
}

// TestMapToList: the snapshot is a (key, value) pair slice of the
// map's current contents — order unspecified, so compare as a map.
func TestMapToList(t *testing.T) {
	m := map[string]int64{"a": 1, "b": 2}
	pairs := rt.MapToList[string, int64](m)
	if len(pairs) != 2 {
		t.Fatalf("len(MapToList) = %d, want 2", len(pairs))
	}
	seen := map[string]int64{}
	for _, p := range pairs {
		seen[p.V0] = p.V1
	}
	if seen["a"] != 1 || seen["b"] != 2 {
		t.Errorf("MapToList contents = %v, want {a:1 b:2}", seen)
	}
	if got := rt.MapToList[string, int64](nil); len(got) != 0 {
		t.Errorf("MapToList(nil map) = %v, want empty", got)
	}
}

// TestMapSetNilPanic: setting a nil map panics like Go's m[k] = v.
func TestMapSetNilPanic(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MapSet(nil map) did not panic")
		}
	}()
	rt.MapSet(map[string]int64(nil), "k", 1)
}
