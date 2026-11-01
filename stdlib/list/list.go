// Package list is the Go-side representation of Gom's builtin
// List a type: a plain slice with an immutability contract (P0-3,
// docs/decisions/2026-11-01-value-representation.md).
//
//	type List[A any] []A
//
// The zero value (nil) is the empty list. A List is immutable: once a
// List value exists, its backing array must never be written to again.
// Operations either allocate a fresh array (Cons, FromSlice, Map) or
// return a sub-slice that only reads the shared array (Tail). At the
// Go boundary both directions copy — a Gom List must not change when
// the caller keeps mutating the slice it came from, and a Go callee
// must not scribble on a Gom list through the slice it received.
package list

import "slices"

// List is Gom's List[A] on the slice representation.
type List[A any] []A

// Of returns a list holding xs in order.
func Of[A any](xs ...A) List[A] {
	return slices.Clone(xs)
}

// Cons builds a list whose head is x and whose tail is rest, in
// O(len(rest)): a fresh backing array of len(rest)+1 is allocated and
// rest is copied into it. rest is never modified.
func Cons[A any](x A, rest List[A]) List[A] {
	out := make(List[A], len(rest)+1)
	out[0] = x
	copy(out[1:], rest)
	return out
}

// Head returns the first element. It panics on the empty list, as a
// Gom match arm that binds [x, ..] would.
func (l List[A]) Head() A { return l[0] }

// Tail returns l[1:] in O(1), sharing the backing array — the whole
// array object (including the dropped head slot) stays alive as long
// as the tail does.
func (l List[A]) Tail() List[A] { return l[1:] }

// Append returns the concatenation of l and m in
// O(len(l)+len(m)), one allocation; neither argument is mutated.
func Append[A any](l, m List[A]) List[A] {
	out := make(List[A], 0, len(l)+len(m))
	out = append(out, l...)
	out = append(out, m...)
	return out
}

// FromSlice converts a Go slice to a List by copying, per the
// boundary rule: the result is unaffected by later writes to xs.
func FromSlice[A any](xs []A) List[A] { return slices.Clone(xs) }

// ToSlice copies the list into a fresh Go slice. Writes through the
// result do not affect the List.
func (l List[A]) ToSlice() []A { return slices.Clone(l) }

// Map applies f to every element, returning a new list. One
// allocation.
func Map[A, B any](l List[A], f func(A) B) List[B] {
	out := make(List[B], len(l))
	for i, x := range l {
		out[i] = f(x)
	}
	return out
}

// Fold reduces the list left to right: f(f(...f(z, l[0])...), l[n-1]).
func Fold[A, B any](l List[A], z B, f func(B, A) B) B {
	acc := z
	for _, x := range l {
		acc = f(acc, x)
	}
	return acc
}
