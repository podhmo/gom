// Package flat2 is a refinement of candidate B (flat): the same
// {Value, Error, discriminant} layout, but the discriminant is an
// unexported uint8 tag with an explicit invalid state, instead of an
// exported bool.
//
//	type Result[T, E any] struct {
//		state tag  // unexported: 0 = invalid (zero value), 1 = Ok, 2 = Err
//		Value T
//		Error E
//	}
//
// The bool version (package flat) has no invalid state: its zero value
// reads as Err(zero E), and ResultToGo on it returns (zero T, nil) — a
// Go caller's `var r Result` silently becomes a *successful* (T,
// error). Making the discriminant a three-state tag restores the
// InvalidVariant arm at zero runtime cost (the field pads to a word
// either way). Payload fields stay exported for readability; a caller
// that writes corrupt field combinations after construction is
// outside the boundary contract, same as writing to a sealed rep's
// variant fields.
package flat2

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/podhmo/gom/experiments/p0/result-repr/rt"
)

type ResultTag uint8

const (
	ResultTag_Invalid ResultTag = iota // zero value: not a real variant
	ResultTag_Ok
	ResultTag_Err
)

type Result[T, E any] struct {
	state ResultTag
	Value T
	Error E
}

// Constructors are functions (not literals) because the tag is
// unexported — and because Ok(v) / Err(e) cannot infer the other type
// parameter anyway. Both inline to the literal cost.
func Ok[T, E any](v T) Result[T, E] {
	return Result[T, E]{state: ResultTag_Ok, Value: v}
}
func Err[T, E any](e E) Result[T, E] {
	return Result[T, E]{state: ResultTag_Err, Error: e}
}

// Tag exposes the discriminant for decision-tree style matching; it
// inlines to a field read.
func (r Result[T, E]) Tag() ResultTag { return r.state }

// IsOk is the Go-ergonomic check, `if r.IsOk() { use r.Value }`.
func (r Result[T, E]) IsOk() bool { return r.state == ResultTag_Ok }

// Unwrap is the Go-idiomatic destructure: `v, err := r.Unwrap()` gives
// (value-or-zero, error-or-zero), mirroring (T, error) exactly. An
// invalid Result panics rather than manufacturing a success.
func (r Result[T, E]) Unwrap() (T, E) {
	var zeroT T
	var zeroE E
	switch r.state {
	case ResultTag_Ok:
		return r.Value, zeroE
	case ResultTag_Err:
		return zeroT, r.Error
	default:
		panic(rt.InvalidVariant("flat2.Result", r))
	}
}

// ---- (T, error) -> Result boundary conversion ----

func ResultFromGo[T any](v T, err error) Result[T, error] {
	if err != nil {
		return Err[T](err)
	}
	return Ok[T, error](v)
}

// ResultToGo unwraps a Result for a Go caller that wants (T, error).
// Unlike the flat version, an invalid Result (incl. the zero value)
// panics instead of returning (zero, nil).
func ResultToGo[T any](r Result[T, error]) (T, error) {
	switch r.Tag() {
	case ResultTag_Ok:
		return r.Value, nil
	case ResultTag_Err:
		var zero T
		return zero, r.Error
	default:
		panic(rt.InvalidVariant("flat2.Result", r))
	}
}

// ---- match (decision tree) on a Result ----

// GetOrElse is `match r { Ok v -> v; Err _ -> fallback }` with a real
// default arm again.
func GetOrElse[T, E any](r Result[T, E], fallback T) T {
	switch r.Tag() {
	case ResultTag_Ok:
		return r.Value
	case ResultTag_Err:
		return fallback
	default:
		panic(rt.InvalidVariant("flat2.Result", r))
	}
}

// ---- try ----

func ParseSum(a, b string) Result[int64, error] {
	x, err := strconv.Atoi(a)
	if err != nil {
		return Err[int64](err)
	}
	y, err := strconv.Atoi(b)
	if err != nil {
		return Err[int64](err)
	}
	return Ok[int64, error](int64(x + y))
}

// ---- |?> ----

func checkPortRange(v int64) Result[int64, error] {
	if v <= 0 || v > 65535 {
		return Err[int64](fmt.Errorf("port out of range: %d", v))
	}
	return Ok[int64, error](v)
}

// ParsePort hand-compiles spec 9.2's example per design.md 4.6. The
// generated match on a Gom step's Result is a Tag() switch, not a
// type switch.
func ParsePort(s string) Result[int64, error] {
	t := strings.TrimSpace(s)
	v, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return Err[int64](fmt.Errorf("strconv.ParseInt: %w", err))
	}
	r := checkPortRange(v)
	switch r.Tag() {
	case ResultTag_Ok:
		return r
	case ResultTag_Err:
		return Err[int64](fmt.Errorf("check_port_range: %w", r.Error))
	default:
		panic(rt.InvalidVariant("flat2.Result", r))
	}
}
