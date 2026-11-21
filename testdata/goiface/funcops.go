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
	// Guard mirrors the redirect-callback shape — two parameters and
	// a bare `error` result, like http.Client.CheckRedirect.
	Guard func(int64, string) error
}

// WithFloat keeps a float64 callback field — a non-Int numeric
// signature for the deferred-assignment default-ordering fixture.
type WithFloat struct {
	F func(float64) float64
	G func(float64) float64
}

// CallF invokes the stored float callback.
func (w *WithFloat) CallF(x float64) float64 { return w.F(x) }

// Rep returns a slice — the iterable-result member call (`Go.Slice
// Int`) a `for` deferred on a meta receiver consumes.
func (w *WithFloat) Rep(n int64) []int64 { return nil }

// Boxes returns struct values — a record-element iterable: a `for`
// body's `v.N` read resolves only once the deferred iterable binds
// its element.
func (w *WithFloat) Boxes(n int64) []SliceBox { return nil }

// Ratios returns a named-numeric iterable — `v + 1` in a deferred
// `for` body must see Ratio, not a premature Int default.
func (w *WithFloat) Ratios(n int64) []Ratio { return nil }

// Anys returns an `any`-element slice — the `Go.Slice Go.Any`
// element that `unify` would accept without binding the loop
// variable's type.
func (w *WithFloat) Anys(n int64) []any { return nil }

// Ch returns a channel — the non-list iterable a deferred `for`'s
// List default must not pre-empt.
func (w *WithFloat) Ch(n int64) chan int64 { return nil }

// Call invokes the stored callback.
func (w *WithFn) Call() int64 { return w.Fn(1) }

// FnBox keeps an over-arity func-typed field — beyond Go.FuncN's
// limit the field stays hidden.
type FnBox struct {
	Many func(int, int, int, int)
}

// Apply1 calls a one-parameter callback — the
// `func(int64) int64` shape.
func Apply1(f func(int64) int64) int64 { return f(0) }

// Tick is a zero-parameter producer — the `func() int64`
// signature a `Unit -> Int` member shape has on the Go side.
func Tick() int64 { return 7 }

// U is a one-parameter function whose parameter is `struct{}` —
// on the Gom side it reads `Unit -> Int`, and the adapter's call
// still passes the unit value `struct{}{}`.
func U(x struct{}) int64 { return 3 }

// Adder returns a two-parameter callback — a func-typed result
// (`func(int64, int64) int64`) feeding the `Go.fnN` conversion.
func Adder() func(int64, int64) int64 {
	return func(a, b int64) int64 { return a + b }
}

// IntAdder returns a callback over Go `int` — the int/int64
// boundary pair `Go.fnN`'s adapter converts in both directions.
func IntAdder() func(int, int) int {
	return func(a, b int) int { return a + b }
}

// Ticker returns a nullary callback — the `func() int64` shape.
func Ticker() func() int64 { return Tick }

// Failer returns an error-only callback — the `func() error`
// shape `Go.fnN` reads back as `Result Unit GoError`.
func Failer() func() error {
	return func() error { return errors.New("boom") }
}

// Looker returns a `(T, error)` callback — the
// `func(int64) (string, error)` shape.
func Looker() func(int64) (string, error) { return Give }

// ErrPair is a `func() (error, error)` source for `Go.fnN` — the
// method-bearing-interface payload whose derived view wraps in
// Option (mapping.go's nil-able payload rule).
func ErrPair() func() (error, error) {
	return func() (error, error) { return errors.New("first"), nil }
}

// ErrPairNil is the same `func() (error, error)` source returning
// `(nil, nil)` — `Go.fn0` must read it as `Ok None` through the
// `option.FromGo` payload rule.
func ErrPairNil() func() (error, error) {
	return func() (error, error) { return nil, nil }
}

// Ints is a `func([]int) int` source — `[]int` splits `int`/`int64`
// inside a container the boundary cannot convert element-wise, so
// `Go.fn1` on it rejects E0510.
func Ints() func([]int) int {
	return func(xs []int) int { return len(xs) }
}

// CancelFunc is a method-less named func type — the
// `context.CancelFunc` shape spec 8.8 reads as `Go.Func0`.
type CancelFunc func()

// Pending returns a pair like `context.WithCancel`'s — the named
// func value reads back as `Go.Func0` for `Go.fn0` to convert.
func Pending() (int64, CancelFunc) {
	return 0, func() {}
}

// TakeCancel consumes a named func parameter — a `Go.func0` value
// conforms to its FuncN spelling.
func TakeCancel(f CancelFunc) { f() }

// Handler is a method-bearing named func type — it keeps the named
// handle so `h.Fire` resolves (spec 8.8).
type Handler func(int64) int64

// Fire is the method the named handle keeps.
func (h Handler) Fire(n int64) int64 { return h(n) }

// NewHandler returns the method-bearing named func value.
func NewHandler() Handler { return func(n int64) int64 { return n + 1 } }
