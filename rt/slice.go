package rt

// Slice primitives backing the `Go.Slice` member table — the
// runtime half of spec section 8.5's read-only slice surface
// (decisions/2026-11-05-go-slice-ops). A `Go.Slice a` is Go's `[]T`
// itself: every member is a read — nothing here mutates the slice
// or its elements, so a `Go.Slice` value stays usable without the
// boundary copies `List` pays.

// SliceLen is `Go.Slice.length s` — 0 on a nil slice, like `len`.
func SliceLen[T any](s []T) int64 { return int64(len(s)) }

// SliceGet is `Go.Slice.get s i`: the (v, ok) pair generated code
// wraps into Some/None — ok=false when i is out of bounds (a
// negative index or one at or past the length).
func SliceGet[T any](s []T, i int64) (T, bool) {
	if i < 0 || i >= int64(len(s)) {
		var zero T
		return zero, false
	}
	return s[i], true
}

// SliceSub is `Go.Slice.slice s lo hi` — `s[lo:hi]`, a shared view
// of the same backing array, never a copy. Out-of-range or
// inverted bounds panic like Go's own slice expression.
func SliceSub[T any](s []T, lo, hi int64) []T { return s[lo:hi] }

// SliceFold is `Go.Slice.fold s f acc` — a left fold over the
// slice's elements: `acc = f(acc)(v)` in index order. The f
// parameter is the canonical representation of the Gom `b -> a ->
// b` — one Go func per arrow — so a curried Gom function value
// passes through verbatim. A nil slice folds to acc.
func SliceFold[T, B any](s []T, f func(B) func(T) B, acc B) B {
	for _, v := range s {
		acc = f(acc)(v)
	}
	return acc
}

// SliceToList is `Go.Slice.to_list s`. It returns s itself — the
// snapshot is the generated call site's job: emitted code wraps
// the result in `list.FromSlice`, which clones, so cloning here
// would pay a second O(n) copy for nothing.
func SliceToList[T any](s []T) []T { return s }
