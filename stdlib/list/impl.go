package list

import (
	"slices"

	"github.com/podhmo/gom/rt"
	"github.com/podhmo/gom/stdlib/option"
)

// Hand-written implementations backing the `@extern fn` signatures in
// list.gom plus the `Array`↔`List` hand-off members (decision
// docs/decisions/2026-11-18-cons-buffer-array-extern-impl.md).
//
// The .gom declarations carry the Gom-facing signatures; these
// functions carry the buffer-building bodies the slice List
// representation needs — each builds its result into a fresh []A
// buffer in one pass, replacing the O(n²) cons-per-step shape.

// ArrayFreeze hands an `Array a` (`[]A`) buffer to a `List a` —
// Gom's `Array.to_list`. Verbatim, no copy: once a list exists the
// caller must not write through the array again (the spec §8.7
// hand-off contract; violations are undiagnosed).
func ArrayFreeze[A any](xs []A) List[A] { return List[A](xs) }

// ArrayFromList copies a `List a` into a fresh `Array a` — Gom's
// `Array.from_list`. The copy is what makes the array writable:
// mutating it can never reach the immutable list it came from.
func ArrayFromList[A any](xs List[A]) []A { return slices.Clone(xs) }

// Map applies f to every element in order — `@extern fn map`.
func Map[A, B any](f func(A) B, xs List[A]) List[B] {
	out := make(List[B], len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

// Filter keeps the elements that satisfy f, in order — `@extern fn filter`.
func Filter[A any](f func(A) bool, xs List[A]) List[A] {
	out := make(List[A], 0, len(xs))
	for _, x := range xs {
		if f(x) {
			out = append(out, x)
		}
	}
	return out
}

// FilterMap keeps the payloads f returns as Some, in order —
// `@extern fn filter_map`. map+filter fused in one pass.
func FilterMap[A, B any](f func(A) option.Option[B], xs List[A]) List[B] {
	out := make(List[B], 0, len(xs))
	for _, x := range xs {
		switch o := f(x).(type) {
		case option.Option_Some[B]:
			out = append(out, o.V0)
		case option.Option_None[B]:
		default:
			// A nil Option is not None — it is the invalid
			// zero value every match panics on.
			panic(rt.InvalidVariant("Option", o))
		}
	}
	return out
}

// FindMap returns the first Some payload f produces, or None when
// f yields None for every element — `@extern fn find_map`.
func FindMap[A, B any](f func(A) option.Option[B], xs List[A]) option.Option[B] {
	for _, x := range xs {
		switch o := f(x).(type) {
		case option.Option_Some[B]:
			return o
		case option.Option_None[B]:
		default:
			panic(rt.InvalidVariant("Option", o))
		}
	}
	return option.Option_None[B]{}
}

// Append is xs ++ ys — `@extern fn append`.
func Append[A any](xs, ys List[A]) List[A] {
	// An empty side hands the other back verbatim — concatGo
	// appends per inner list, so the copy must not run per call.
	if len(xs) == 0 {
		return ys
	}
	if len(ys) == 0 {
		return xs
	}
	out := make(List[A], 0, len(xs)+len(ys))
	out = append(out, xs...)
	return append(out, ys...)
}

// Concat is xss[0] ++ xss[1] ++ ... — `@extern fn concat`. One
// pass over the precomputed total size: the .gom right-fold
// (`append xs (concat_go rest)`) kept O(n) copies but stacked a
// frame per sublist — O(k) depth, which a deep list-of-lists blows
// through. The buffer walk is O(1) stack, O(n) time, one
// allocation.
func Concat[A any](xss List[List[A]]) List[A] {
	n := 0
	for _, xs := range xss {
		n += len(xs)
	}
	out := make(List[A], 0, n)
	for _, xs := range xss {
		out = append(out, xs...)
	}
	return out
}

// Take is the first n elements of xs — `@extern fn take`. It
// shares the immutable backing the way `drop` does: List is
// immutable, so a prefix slice is as safe as a copy and costs no
// allocation. The full slice expression caps the result's
// capacity at n — without it an append (or a [:lo:hi:cap] view)
// on the taken list would reach elements the prefix excluded.
func Take[A any](n int64, xs List[A]) List[A] {
	if n <= 0 {
		return nil
	}
	if int64(len(xs)) < n {
		n = int64(len(xs))
	}
	return xs[:n:n]
}

// SortBy is xs sorted ascending under lt — `@extern fn sort_by`.
// A fresh buffer so the input's backing is untouched; same shape
// as cmp.SortBy (the `Ord`-dictionary form lives in cmp — list
// cannot depend on it, the package DAG runs list < cmp).
func SortBy[A any](lt func(A, A) bool, xs List[A]) List[A] {
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

// Rev reverses xs — `@extern fn rev`.
func Rev[A any](xs List[A]) List[A] {
	out := make(List[A], len(xs))
	for i, x := range xs {
		out[len(xs)-1-i] = x
	}
	return out
}

// Range is [lo, lo+1, ..., hi-1], empty when lo >= hi — `@extern fn range`.
func Range(lo, hi int64) List[int64] {
	if lo >= hi {
		return nil
	}
	out := make(List[int64], hi-lo)
	for i := range out {
		out[i] = lo + int64(i)
	}
	return out
}

// OptionToList is `Option.to_list`: Some x → [x], None → []. It
// lives in this package — the upward half of the
// result < option < list layering — while being surfaced as a
// member of the Option module (decision
// stdlib-conv-placement).
func OptionToList[A any](o option.Option[A]) List[A] {
	switch o := o.(type) {
	case option.Option_Some[A]:
		return List[A]{o.V0}
	case option.Option_None[A]:
		return nil
	default:
		// A nil Option is not None — it is the invalid zero
		// value every match's default arm panics on.
		panic(rt.InvalidVariant("Option", o))
	}
}
