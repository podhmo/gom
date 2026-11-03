// Package list is the Go-side representation of Gom's builtin
// List a type: a plain slice with an immutability contract (P0-3,
// docs/decisions/2026-11-01-value-representation.md).
//
//	type List[A any] []A
//
// The zero value (nil) is the empty list. A List is immutable: once a
// List value exists, its backing array must never be written to again.
// Operations either allocate a fresh array (Cons, FromSlice, Of) or
// return a sub-slice that only reads the shared array (Tail). At the
// Go boundary both directions copy — a Gom List must not change when
// the caller keeps mutating the slice it came from, and a Go callee
// must not scribble on a Gom list through the slice it received.
//
// This file is the hand-written representation the compiler's
// generated code references; the `List` module's functions
// (List.map, List.sum, ...) are implemented in Gom —
// stdlib/list/list.gom, generated to list.go — with the public
// surface declared by list.gomi.
package list

import "slices"

// List is Gom's List[A] on the slice representation.
type List[A any] []A

// Head returns the first element. It panics on the empty list, as a
// Gom match arm that binds [x, ..] would. (The Gom-side `List.head`
// is total — it returns an Option instead.)
func (l List[A]) Head() A { return l[0] }

// Tail returns l[1:] in O(1), sharing the backing array — the whole
// array object (including the dropped head slot) stays alive as long
// as the tail does.
func (l List[A]) Tail() List[A] { return l[1:] }

// FromSlice converts a Go slice to a List by copying, per the
// boundary rule: the result is unaffected by later writes to xs.
func FromSlice[A any](xs []A) List[A] { return slices.Clone(xs) }

// ToSlice copies the list into a fresh Go slice. Writes through the
// result do not affect the List.
func (l List[A]) ToSlice() []A { return slices.Clone(l) }
