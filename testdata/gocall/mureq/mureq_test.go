package mureq

import "testing"

// The emitted `type A []B` / `type B []A` comparators recurse across
// the two record types through the list elements: equal nested
// payloads agree and a truncated tail differs.
func TestMutualRecordEq(t *testing.T) {
	if !EqA(Lhs, Rhs) {
		t.Fatal("EqA(Lhs, Rhs) = false, want true")
	}
	if EqA(Lhs, Dif) {
		t.Fatal("EqA(Lhs, Dif) = true, want false")
	}
}
