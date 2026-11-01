// Package result is the Go-side representation of Gom's builtin
// Result a e type (docs/design.md section 4.6).
//
// Result is the flat representation with an unexported three-state
// tag (P0-4, docs/decisions/2026-11-01-value-representation.md):
//
//	type Result[T, E any] struct {
//		state ResultTag // unexported: 0 = invalid (zero value), 1 = Ok, 2 = Err
//		Value T
//		Error E
//	}
//
// The zero value is invalid — a Go caller's `var r Result` is a
// contract violation caught at match time via panic, not a silent
// success. Constructors are functions because the tag is unexported
// and because Ok(v) / Err(e) cannot infer the other type parameter
// anyway. Generated code constructs values with Ok/Err, discriminates
// with a Tag() switch whose default arm panics via rt.InvalidVariant,
// and reads payloads through the exported Value/Error fields.
package result

import "fmt"

// ResultTag is the discriminant of Result. The zero value is not a
// real variant: it marks the zero Result, which is invalid.
type ResultTag uint8

const (
	// ResultTagInvalid marks the zero value: not a real variant.
	ResultTagInvalid ResultTag = iota
	// ResultTagOk marks a success value.
	ResultTagOk
	// ResultTagErr marks a failure value.
	ResultTagErr
)

// Result is Gom's `Result a e`. Value holds the Ok payload and Error
// the Err payload; only the field matching the tag is meaningful.
type Result[T, E any] struct {
	state ResultTag
	Value T
	Error E
}

// Ok constructs a success Result.
func Ok[T, E any](v T) Result[T, E] {
	return Result[T, E]{state: ResultTagOk, Value: v}
}

// Err constructs a failure Result.
func Err[T, E any](e E) Result[T, E] {
	return Result[T, E]{state: ResultTagErr, Error: e}
}

// Tag exposes the discriminant for decision-tree style matching; it
// inlines to a field read.
func (r Result[T, E]) Tag() ResultTag { return r.state }

// IsOk is the Go-ergonomic check, `if r.IsOk() { use r.Value }`.
func (r Result[T, E]) IsOk() bool { return r.state == ResultTagOk }

// Unwrap is the Go-idiomatic destructure: `v, err := r.Unwrap()` gives
// (value-or-zero, error-or-zero). An invalid Result panics rather
// than manufacturing a success.
func (r Result[T, E]) Unwrap() (T, E) {
	switch r.state {
	case ResultTagOk:
		var zeroE E
		return r.Value, zeroE
	case ResultTagErr:
		var zeroT T
		return zeroT, r.Error
	default:
		panic(invalidVariant(r))
	}
}

// ToGo unwraps a Result[T, error] for a Go caller that wants
// (T, error). An invalid Result panics.
func ToGo[T any](r Result[T, error]) (T, error) {
	switch r.Tag() {
	case ResultTagOk:
		return r.Value, nil
	case ResultTagErr:
		var zero T
		return zero, r.Error
	default:
		panic(invalidVariant(r))
	}
}

// invalidVariant mirrors rt.InvalidVariant's message. This package
// cannot import rt: rt's boundary helpers depend on this package, so
// stdlib packages stay leaf dependencies.
func invalidVariant(v any) error {
	return fmt.Errorf("gom: invalid %s value %#v: not a declared variant", "result.Result", v)
}
