// Package goiface supplies named Go interfaces and channels for compiler fixtures.
package goiface

import "fmt"

type Empty interface{}

func Kind(value Empty) string       { return fmt.Sprintf("%T", value) }
func Channel() chan string          { return nil }
func ReceiveChannel() <-chan string { return nil }
func SendChannel() chan<- string    { return nil }

type Container struct{ Count int }

func NewContainer() *Container { return &Container{Count: 41} }

// SliceBox is the field-level counterpart of Sum's parameter split:
// exported slice fields read through the same boundary mapping —
// Xs ([]int64) is a per-read List copy, while Is ([]int) splits
// int/int64 inside the field type and stays hidden (E0501).
type SliceBox struct {
	N  int64
	Xs []int64
	// As is `[]any` — its Gom `List Go.Any` view must still
	// reconcile with a `Go.Slice Go.Any` use.
	As []any
	Is []int
}

func NewSliceBox() *SliceBox {
	return &SliceBox{N: 1, Xs: []int64{1, 2}, Is: []int{1, 2}}
}

// Ratio is a named float type — the `type X float64` shape
// named-numeric arithmetic covers (`1 / 2` under a Ratio binder is
// 0.5, not Go's integer `0`).
type Ratio float64

// Ticks is a named integer type — the `type X int64` shape, a
// lighter stand-in for time.Duration.
type Ticks int64

// Generic functions for the P3-3 generic-call fixtures. Second's
// declared type-parameter order (A, B) is the reverse of first
// occurrence (B, A), so the emitted `Second[A, B]` instantiation
// proves CallInfo.TypeArgs follows the declaration.
func Identity[T any](v T) T { return v }

func Second[A any, B any](b B, a A) A { return a }

func Count[E comparable](xs []E, v E) int64 {
	var n int64
	for _, x := range xs {
		if x == v {
			n++
		}
	}
	return n
}

func Lookup[K comparable, V any](m map[K]V, k K) (V, bool) {
	v, ok := m[k]
	return v, ok
}

// Index's S parameter carries a structural ~[]E constraint — outside
// the any/comparable callable set (spec section 8.5), so the symbol
// stays hidden and referencing it reports E0506.
func Index[S ~[]E, E comparable](s S, v E) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// Sum's []int and Clamp's ~int exercise the two hidden-member
// edges: a container whose element views split (int vs int64), and a
// constraint that is a proper subset of comparable — neither is
// representable/callable at the boundary (spec sections 8.3, 8.5).
func Sum(xs []int) int64 {
	var n int64
	for _, x := range xs {
		n += int64(x)
	}
	return n
}

func Clamp[T ~int](x, lo, hi T) T {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Clamp64 is Clamp over the representable width — `~int64` admits a
// defined type `type D int64` at the boundary, where `~int` does not.
func Clamp64[T ~int64](x, lo, hi T) T {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Choose's `int64 | string` union is all exact terms — a defined type
// `type D int64` satisfies none of them (the emitted argument is
// verbatim `D`, not `int64`).
func Choose[T int64 | string](x, y T) T { return y }

// Sum64 is Sum over []int64 — the representable slice spelling — so
// a `List Int` argument copies at the boundary (spec section 8.5),
// including through `xs |> Sum64` where the piped value conforms to
// the Go parameter.
func Sum64(xs []int64) int64 {
	var n int64
	for _, x := range xs {
		n += x
	}
	return n
}

// LenAny takes `[]any` — the any-element slice a `List Go.Any`
// view reconciles against.
func LenAny(xs []any) int64 { return int64(len(xs)) }
