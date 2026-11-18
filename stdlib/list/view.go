package list

// View is the read-only partial view of a List — the MoonBit
// `StringView` analogue (docs/problems/2026-11-04-shared-slice-view-type.md,
// decision 2026-11-18-list-view). Its backing is a slice of the
// source list's immutable backing, so sub-slicing and indexing are
// O(1) with no copy. Only `List`-derived views exist: a Go-owned
// slice's view would be shared mutable state, covered by `Go.Slice`'s
// own read-only surface instead (decision 2026-11-04-goconv-list-slice-copies).
type View[A any] []A

// ListView is `List.view`: the whole list as a view, O(1).
func ListView[A any](xs List[A]) View[A] { return View[A](xs) }

// ListSub is `List.slice`: `xs[lo:hi]` as a view — Go slice-bounds
// semantics, out-of-range panics.
func ListSub[A any](xs List[A], lo, hi int64) View[A] { return View[A](xs[lo:hi]) }

// ViewLen is `View.length`: nil views are 0.
func ViewLen[A any](v View[A]) int64 { return int64(len(v)) }

// ViewGet is `View.get`: (element, ok) — out-of-range and negative
// indices report not-ok, which the compiler surfaces as `None`.
func ViewGet[A any](v View[A], i int64) (A, bool) {
	var zero A
	if i < 0 || i >= int64(len(v)) {
		return zero, false
	}
	return v[i], true
}

// ViewSub is `View.slice`: `v[lo:hi]` — shares the backing.
func ViewSub[A any](v View[A], lo, hi int64) View[A] { return v[lo:hi] }

// ViewFold is `View.fold`: left fold, nil iterates zero times.
func ViewFold[A, B any](v View[A], f func(B, A) B, acc B) B {
	for _, x := range v {
		acc = f(acc, x)
	}
	return acc
}

// ViewToList is `View.to_list`: the viewed range as a `List`,
// sharing the backing — both sides are immutable, so the share is
// O(1) rather than a copy.
func ViewToList[A any](v View[A]) List[A] { return List[A](v) }
