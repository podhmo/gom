package goiface

import "errors"

// Apply0 calls a nullary callback — the `func() int64` shape.
func Apply0(f func() int64) int64 { return f() }

// Apply2 calls a two-parameter callback — the `func(int64, int64)
// int64` shape.
func Apply2(f func(int64, int64) int64) int64 { return f(1, 2) }

// Check calls a callback and reports its error — the
// `func(int64) error` shape a `Result Unit GoError` callback
// produces.
func Check(f func(int64) error) error { return f(0) }

// LookAll calls a `(T, error)` callback — the
// `func(int64) (string, error)` shape.
func LookAll(f func(int64) (string, error)) (string, error) { return f(0) }

// Give is a `(string, error)` value to feed Lookup's callback.
func Give(n int64) (string, error) {
	if n == 0 {
		return "zero", nil
	}
	return "", errors.New("negative")
}

// FailOnce is an `error`-only value to feed Check's callback.
func FailOnce(n int64) error {
	if n == 0 {
		return nil
	}
	return errors.New("boom")
}

// TakeFuncPair calls a callback whose own parameter is a func —
// the `func(func(int64)) int64` nested shape.
func TakeFuncPair(f func(func(int64)) int64) int64 {
	return f(func(n int64) {})
}

// WithFn keeps a callback in a struct field — the params-vs-fields
// survey's field shape.
type WithFn struct {
	Fn func(int64) int64
}

// Call invokes the stored callback.
func (w *WithFn) Call() int64 { return w.Fn(1) }

// FnBox keeps an over-arity func-typed field — beyond Go.FuncN's
// limit the field stays hidden.
type FnBox struct {
	Many func(int, int, int, int)
}
