package option

import (
	"github.com/podhmo/gom/rt"
	"github.com/podhmo/gom/stdlib/result"
)

// Conversions into this package — the upward half of the
// result < option < list layering (docs/decisions/
// 2026-11-18-stdlib-conv-placement.md). They are members of the
// *lower* module on the Gom surface (`Result.to_option`) but live
// here because the lower package cannot import upward without a
// Go cycle.

// ResultToOption is `Result.to_option`: Ok x → Some x, Err → None.
// The switch is complete on the tag — an invalid Result (the zero
// value) panics like every match's default arm instead of reading
// as a silent None.
func ResultToOption[T, E any](r result.Result[T, E]) Option[T] {
	switch r.Tag() {
	case result.ResultTagOk:
		return Option_Some[T]{V0: r.Value}
	case result.ResultTagErr:
		return Option_None[T]{}
	default:
		panic(rt.InvalidVariant("Result", r))
	}
}
