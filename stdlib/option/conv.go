package option

import "github.com/podhmo/gom/stdlib/result"

// Conversions into this package — the upward half of the
// result < option < list layering (docs/decisions/
// 2026-11-18-stdlib-conv-placement.md). They are members of the
// *lower* module on the Gom surface (`Result.to_option`) but live
// here because the lower package cannot import upward without a
// Go cycle.

// ResultToOption is `Result.to_option`: Ok x → Some x, Err → None.
func ResultToOption[T, E any](r result.Result[T, E]) Option[T] {
	if r.IsOk() {
		v, _ := r.Unwrap()
		return Option_Some[T]{V0: v}
	}
	return Option_None[T]{}
}
