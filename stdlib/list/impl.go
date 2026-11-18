package list

import "slices"

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
