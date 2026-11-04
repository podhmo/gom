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
