// Package typeclass is the hand-written Go that option (2) —
// typeclass-style implicit dictionaries — would emit for the Gom
// mock in mock.gom.txt. The generated shape is identical to
// explicit dictionary passing: a record value per instance, an
// extra parameter per constrained function. Only the call site
// differs — the compiler inserts the resolved dictionary.
package typeclass

// Ord is the dictionary the `Ord a` constraint means — same record
// as the (1) experiment's `Ord a`.
type Ord[A any] struct {
	Lt func(A) func(A) bool
}

// IntOrd is what `instance Ord Int = ...` emits: a package-level
// dictionary value the resolver inserts at use sites.
var IntOrd = Ord[int64]{
	Lt: func(a int64) func(int64) bool {
		return func(b int64) bool { return a < b }
	},
}

// Min is `fn min [Ord a] (xs: List a) -> Option a` after
// elaboration: the constraint becomes an explicit dictionary
// parameter, exactly like (1). (Option is simplified to (A, bool)
// here — the dict mechanics, not the Option repr, are the point.)
func Min[A any](ord Ord[A], xs []A) (A, bool) {
	if len(xs) == 0 {
		var z A
		return z, false
	}
	best := xs[0]
	for _, x := range xs[1:] {
		if ord.Lt(x)(best) {
			best = x
		}
	}
	return best, true
}

// CallSite shows what `min [3, 1, 2]` elaborates to — the compiler
// resolves `Ord int64` to `IntOrd` and inserts it. Compared to (1),
// only this line differs: the caller never names the dictionary.
func CallSite() (int64, bool) {
	return Min(IntOrd, []int64{3, 1, 2})
}
