// Package slicelist implements Gom's builtin List[A] as a plain Go slice.
//
// Representation: type List[A] []A. The empty list is nil.
//
// # Ownership contract
//
// A List is immutable: once a List value exists, its backing array must
// never be written to again. Every operation that produces a "new" list
// either allocates a fresh backing array (Cons, Of, FromSlice, Map) or
// returns a sub-slice that only ever reads the shared array (Uncons, Tail).
// Nothing in the type system enforces this for Go callers: it is a
// documented contract, exactly like the one the Gom compiler would emit.
//
// # Aliasing
//
// Because List[A] IS []A, the conversions List[A](xs) and []A(l) are free
// and share storage. FromSlice and ToSlice copy, which is what the
// Gom <-> Go boundary needs: a List must not change when the caller keeps
// mutating the slice it came from, and a Go callee must not be able to
// scribble on a Gom list through the slice it received.
// WrapUnchecked and SliceUnchecked perform the aliasing conversions; they
// exist to demonstrate and measure the cost of that contract, not for
// generated code to use.
//
// [x, ..rest] and memory retention
//
// Uncons (the [x, ..rest] pattern) is O(1): rest shares the backing array.
// The price is retention: rest keeps the entire array object alive,
// including the dropped prefix. Nothing in a slice records how much was
// dropped, so no later heuristic can detect or repair the waste.
// TailCopy trades O(n) time for releasing the head's memory.
//
// Construction-side [x, ..rest] is Cons, which is O(n): prepending to a
// contiguous array always copies the whole tail. This is the fundamental
// asymmetry of the slice representation: destructuring is free, building
// is linear.
package slicelist

import (
	"iter"
	"slices"
)

// List is Gom's List[A] on a slice representation.
// The zero value (nil) is the empty list.
type List[A any] []A

// Nil returns the empty list.
func Nil[A any]() List[A] { return nil }

// Of returns a list holding xs in order.
func Of[A any](xs ...A) List[A] {
	return slices.Clone(xs)
}

// Cons implements construction-side [x, ..rest]: a list whose head is x
// and whose tail is rest. It is O(len(rest)): a fresh backing array of
// len(rest)+1 is allocated and rest is copied into it. rest is never
// modified and stays valid and unchanged.
func Cons[A any](x A, rest List[A]) List[A] {
	out := make(List[A], len(rest)+1)
	out[0] = x
	copy(out[1:], rest)
	return out
}

// Uncons destructures the [x, ..rest] pattern. It is O(1): rest shares
// the receiver's backing array, so the whole array object (including the
// dropped head slot) stays alive as long as rest does.
func Uncons[A any](l List[A]) (head A, rest List[A], ok bool) {
	if len(l) == 0 {
		var zero A
		return zero, nil, false
	}
	return l[0], l[1:], true
}

// Tail returns l[1:] in O(1), sharing the backing array. It panics on the
// empty list, mirroring a Gom match arm that binds [_, ..rest].
func (l List[A]) Tail() List[A] { return l[1:] }

// Head returns the first element.
func (l List[A]) Head() A { return l[0] }

// TailCopy is Tail that releases the backing array: it copies the tail
// into a fresh array so the original (and its dropped prefix) can be
// collected. O(n) time, one allocation.
func (l List[A]) TailCopy() List[A] { return slices.Clone(l[1:]) }

// FromSlice converts a Go slice to a List by copying, per the
// interop-boundary rule: the result is unaffected by later writes to xs.
func FromSlice[A any](xs []A) List[A] { return slices.Clone(xs) }

// WrapUnchecked converts xs to a List without copying. The result aliases
// xs: writes through xs are visible through the List and vice versa.
// It exists to demonstrate the aliasing the boundary copy prevents.
func WrapUnchecked[A any](xs []A) List[A] { return List[A](xs) }

// ToSlice copies the list into a fresh Go slice. Writes through the
// result do not affect the List.
func (l List[A]) ToSlice() []A { return slices.Clone(l) }

// SliceUnchecked exposes the backing array without copying. The result
// aliases the List; writing it corrupts the immutable-list contract.
func (l List[A]) SliceUnchecked() []A { return []A(l) }

// All returns an iterator over the elements. Note that the slice
// representation also supports the cheaper `for i, x := range l`
// directly; All exists for parity with the other representations.
func (l List[A]) All() iter.Seq[A] {
	return func(yield func(A) bool) {
		for _, x := range l {
			if !yield(x) {
				return
			}
		}
	}
}

// Map applies f to every element, returning a new list. One allocation.
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

// Append returns the concatenation of l and m. O(len(l)+len(m)), one
// allocation; neither argument is mutated or aliased.
func Append[A any](l, m List[A]) List[A] {
	out := make(List[A], 0, len(l)+len(m))
	out = append(out, l...)
	out = append(out, m...)
	return out
}
