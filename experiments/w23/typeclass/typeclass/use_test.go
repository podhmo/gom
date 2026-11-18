package typeclass

import "testing"

func TestImplicitDictExpansion(t *testing.T) {
	v, ok := CallSite()
	if !ok || v != 1 {
		t.Fatalf("CallSite = %v, %v; want 1, true", v, ok)
	}
	if _, ok := Min(IntOrd, []int64{}); ok {
		t.Fatal("empty list should have no min")
	}
}
