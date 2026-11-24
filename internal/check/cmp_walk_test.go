package check

import (
	"testing"

	"github.com/podhmo/gom/internal/types"
)

// A payload that repeats one wide spelled tuple across fields walks
// thousands of identical nodes: 8 fields of a depth-3 8-ary tuple is
// ~4.7k raw visits, over cmpNodeBudget. The shared subtree is one
// finite shape — the tuple memo walks it once and admits the
// comparison. Without the memo this rejected a valid
// `slices.Contains` as E0508.
func TestCmpWalkSharedTuplePayload(t *testing.T) {
	repeat := func(t types.Type, n int) []types.Type {
		ts := make([]types.Type, n)
		for i := range ts {
			ts[i] = t
		}
		return ts
	}
	leaf := types.Tup(repeat(types.Int, 8)...) // 8-ary
	mid := types.Tup(repeat(leaf, 8)...)       // 64 leaves, 73 nodes
	wide := types.Tup(repeat(mid, 8)...)       // 512 leaves, 585 nodes
	payload := types.Tup(repeat(wide, 8)...)   // ~4.7k raw visits

	c := &checker{tyCons: map[conKey]*typeInfo{}}
	w := &cmpWalk{c: c, memo: map[cmpKey]bool{}}
	if !w.has(payload) {
		t.Fatal("a finite payload repeating a spelled tuple must be comparable")
	}
	if w.nodes >= cmpNodeBudget {
		t.Fatalf("node visits = %d, want well under %d", w.nodes, cmpNodeBudget)
	}
}
