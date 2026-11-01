// Package conslist implements Gom's builtin List[A] as a persistent
// singly linked list.
//
// Representation: an opaque struct wrapping *cell[A]; a nil head pointer
// is the empty list. Cells are immutable and are shared freely between
// lists, which is what makes the structure persistent: Prepend (Cons)
// and Uncons ([x, ..rest] destructuring) are both O(1) and neither
// disturbs any other list that shares the cells.
//
// # Ownership contract
//
// Because cells are unexported and never mutated, Go callers cannot
// corrupt a List: the aliasing hazards of the slice representation do
// not exist here. The zero value is a valid empty list. Passing a List
// through the Gom <-> Go boundary can never alias a Go []A either: the
// representations differ structurally, so FromSlice and ToSlice always
// copy (and cost n cell allocations in the FromSlice direction).
//
// # Memory retention
//
// Tail shares cells with the receiver by design: keeping a suffix keeps
// exactly its own cells alive and lets the dropped prefix be collected.
// This is the mirror image of the slice representation, where the same
// operation retains the whole backing array.
//
// Costs: indexing and Len are O(n), iteration is a pointer chase with
// one allocation's worth of cache locality per cell, and construction of
// an n-element list costs n small allocations (24 bytes each for a
// 64-bit element).
package conslist

import "iter"

// List is Gom's List[A] on a persistent cons-cell representation.
// The zero value is the empty list.
type List[A any] struct {
	head *cell[A]
}

type cell[A any] struct {
	value A
	next  *cell[A]
}

// Nil returns the empty list.
func Nil[A any]() List[A] { return List[A]{} }

// Of returns a list holding xs in order. O(len(xs)) allocations.
func Of[A any](xs ...A) List[A] {
	l := Nil[A]()
	for i := len(xs) - 1; i >= 0; i-- {
		l = Cons(xs[i], l)
	}
	return l
}

// Cons implements construction-side [x, ..rest] in O(1): one cell is
// allocated and linked to rest. rest is shared, not copied.
func Cons[A any](x A, rest List[A]) List[A] {
	return List[A]{head: &cell[A]{value: x, next: rest.head}}
}

// Uncons destructures the [x, ..rest] pattern in O(1). rest shares the
// receiver's cells; the dropped head cell becomes unreachable once the
// caller drops it.
func Uncons[A any](l List[A]) (head A, rest List[A], ok bool) {
	if l.head == nil {
		var zero A
		return zero, Nil[A](), false
	}
	return l.head.value, List[A]{head: l.head.next}, true
}

// IsEmpty reports whether the list is empty.
func (l List[A]) IsEmpty() bool { return l.head == nil }

// Head returns the first element. It panics on the empty list.
func (l List[A]) Head() A { return l.head.value }

// Tail returns the list without its first element in O(1). It panics on
// the empty list, mirroring a Gom match arm that binds [_, ..rest].
func (l List[A]) Tail() List[A] { return List[A]{head: l.head.next} }

// Len returns the number of elements. O(n): the representation stores
// no size, unlike a slice header.
func (l List[A]) Len() int {
	n := 0
	for c := l.head; c != nil; c = c.next {
		n++
	}
	return n
}

// At returns the i-th element. O(i).
func (l List[A]) At(i int) A {
	c := l.head
	for ; i > 0; i-- {
		c = c.next
	}
	return c.value
}

// FromSlice converts a Go slice to a List. O(n) time and n cell
// allocations; no aliasing is possible between the representations.
func FromSlice[A any](xs []A) List[A] { return Of(xs...) }

// ToSlice copies the list into a fresh Go slice. O(n), one allocation.
func (l List[A]) ToSlice() []A {
	out := make([]A, 0, l.Len())
	for c := l.head; c != nil; c = c.next {
		out = append(out, c.value)
	}
	return out
}

// All returns an iterator over the elements.
func (l List[A]) All() iter.Seq[A] {
	return func(yield func(A) bool) {
		for c := l.head; c != nil; c = c.next {
			if !yield(c.value) {
				return
			}
		}
	}
}

// Map applies f to every element, returning a new list. O(n) time, n
// cell allocations, single pass, order preserved.
func Map[A, B any](l List[A], f func(A) B) List[B] {
	var head, tail *cell[B]
	for c := l.head; c != nil; c = c.next {
		nc := &cell[B]{value: f(c.value)}
		if tail == nil {
			head = nc
		} else {
			tail.next = nc
		}
		tail = nc
	}
	return List[B]{head: head}
}

// Fold reduces the list left to right.
func Fold[A, B any](l List[A], z B, f func(B, A) B) B {
	acc := z
	for c := l.head; c != nil; c = c.next {
		acc = f(acc, c.value)
	}
	return acc
}

// Append returns the concatenation of l and m. O(len(l)) time and cells;
// m is shared with the result.
func Append[A any](l, m List[A]) List[A] {
	var head, tail *cell[A]
	for c := l.head; c != nil; c = c.next {
		nc := &cell[A]{value: c.value}
		if tail == nil {
			head = nc
		} else {
			tail.next = nc
		}
		tail = nc
	}
	if tail == nil {
		return m
	}
	tail.next = m.head
	return List[A]{head: head}
}

// Reverse returns the list in reverse order. O(n) time and cells.
func Reverse[A any](l List[A]) List[A] {
	out := Nil[A]()
	for c := l.head; c != nil; c = c.next {
		out = Cons(c.value, out)
	}
	return out
}
