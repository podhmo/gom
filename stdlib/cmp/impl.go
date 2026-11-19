package cmp

import (
	"slices"

	"github.com/podhmo/gom/stdlib/list"
)

// SortBy is the comparator form `sort` lowers onto — `@extern fn
// sort_by`. It sorts a fresh buffer ascending: x < y, equal, x > y
// under lt, so the ordering is O(n log n) — the pure-Gom insertion
// sort of experiments/w23/typeclass/dict would be quadratic here.
func SortBy[A any](lt func(A, A) bool, xs list.List[A]) list.List[A] {
	out := slices.Clone(xs)
	slices.SortFunc(out, func(x, y A) int {
		if lt(x, y) {
			return -1
		}
		if lt(y, x) {
			return 1
		}
		return 0
	})
	return out
}
