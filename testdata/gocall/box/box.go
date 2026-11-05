// Package box is the Go-owned mutable state of the boundary fixture
// (docs/sketch/2026-11-04-boundary-aliasing-hypotheses.md): a named
// struct with scalar and slice fields, kept behind a shared pointer
// so Go-side mutations are observable from Gom re-reads.
package box

// Box is a plain Go struct. N and Name are scalar fields; Xs is the
// []int64 field the hypothesis file asks about — the field's type is
// a slice nested inside a struct, not a signature position of its
// own. Is is the []int edge: its element views split int/int64, the
// case mapBoundaryType cannot spell a conversion for. XXs is one
// level deeper — it answers how far the boundary copy reaches.
type Box struct {
	N    int64
	Name string
	Xs   []int64
	Is   []int
	XXs  [][]int64
}

// shared is the Go-owned instance every probe below touches — one
// backing object whose mutations must be visible through any Gom
// binding that truly aliases it.
var shared = &Box{
	N:    1,
	Name: "one",
	Xs:   []int64{10, 20, 30},
	Is:   []int{1, 2, 3},
	XXs:  [][]int64{{5, 6}, {7}},
}

// Shared returns the package-owned *Box — the same pointer every
// call, so a Gom Ptr box.Box aliases Go's own state.
func Shared() *Box { return shared }

// Snapshot returns *shared by value — a detached copy for the
// value-struct direction (H1).
func Snapshot() Box { return *shared }

// MutateN adds delta to shared.N — the Go-side write that decides
// whether a Gom field read produced an alias or a snapshot.
func MutateN(delta int64) { shared.N += delta }

// MutateX writes shared.Xs[i] = v in place — mutating the backing
// array a slice field read may or may not share.
func MutateX(i int, v int64) { shared.Xs[i] = v }

// MutateXX writes shared.XXs[i][j] = v — reaching an inner backing
// array a one-level boundary copy cannot insulate.
func MutateXX(i, j int, v int64) { shared.XXs[i][j] = v }

// Reset restores shared to its initial state so test cases stay
// independent of call order.
func Reset() {
	shared = &Box{
		N:    1,
		Name: "one",
		Xs:   []int64{10, 20, 30},
		Is:   []int{1, 2, 3},
		XXs:  [][]int64{{5, 6}, {7}},
	}
}
