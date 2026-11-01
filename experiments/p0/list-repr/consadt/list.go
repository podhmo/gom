// Package consadt implements Gom's List[A] as an ordinary Gom ADT would
// be compiled under design.md 4.1: a sealed interface plus one exported
// struct per variant. It exists to measure what the cons representation
// costs if the compiler does NOT special-case the builtin List type.
//
//	type List[A any] interface{ isList() }
//	type List_Nil[A any]  struct{}
//	type List_Cons[A any] struct{ V0 A; V1 List[A] }
//
// Compared to the conslist package (opaque struct + *cell):
//   - The tail field V1 is an interface, so every cons cell carries a
//     16-byte interface word plus the pointer indirection of boxing the
//     child List_Cons value. Cells are one word larger and matching is a
//     type switch instead of a nil check.
//   - var l List[A] is a nil interface, i.e. the invalid variant: any
//     match on it must panic as a contract violation, exactly like every
//     other ADT. There is no free "zero value is empty list".
//   - V0 and V1 must stay exported so Go callers can destructure. This
//     turns out to be safe anyway: a type assertion copies the variant
//     struct out of its box, so writes to the result never reach the
//     shared cell. Immutability comes from Go's value semantics rather
//     than from hiding, at the price of a copy on every unbox.
package consadt

import (
	"fmt"
	"iter"
)

// List is the sealed-interface encoding of `type List a = | Nil | Cons a (List a)`.
type List[A any] interface{ isList() }

// List_Nil is the empty-list variant. Zero-sized: values cost no allocation.
type List_Nil[A any] struct{}

// List_Cons is the non-empty variant. V0 is the head, V1 the tail.
type List_Cons[A any] struct {
	V0 A
	V1 List[A]
}

func (List_Nil[A]) isList()  {}
func (List_Cons[A]) isList() {}

// Nil returns the empty list.
func Nil[A any]() List[A] { return List_Nil[A]{} }

// Of returns a list holding xs in order. O(len(xs)) allocations.
func Of[A any](xs ...A) List[A] {
	l := Nil[A]()
	for i := len(xs) - 1; i >= 0; i-- {
		l = Cons(xs[i], l)
	}
	return l
}

// Cons implements construction-side [x, ..rest] in O(1). The tail is
// boxed into the interface field, which costs one heap allocation per
// cell (the List_Cons struct escapes into V1's interface word).
func Cons[A any](x A, rest List[A]) List[A] {
	return List_Cons[A]{V0: x, V1: rest}
}

// Uncons destructures the [x, ..rest] pattern in O(1) via type switch.
// A nil or otherwise invalid receiver panics as a contract violation,
// matching design.md 4.1's rt.InvalidVariant rule.
func Uncons[A any](l List[A]) (head A, rest List[A], ok bool) {
	switch c := l.(type) {
	case List_Cons[A]:
		return c.V0, c.V1, true
	case List_Nil[A]:
		var zero A
		return zero, Nil[A](), false
	default:
		panic(fmt.Sprintf("invalid List variant %T", l))
	}
}

// IsEmpty reports whether the list is the Nil variant.
func IsEmpty[A any](l List[A]) bool {
	_, ok := l.(List_Nil[A])
	return ok
}

// Len returns the number of elements. O(n).
func Len[A any](l List[A]) int {
	n := 0
	for {
		switch c := l.(type) {
		case List_Cons[A]:
			n++
			l = c.V1
		case List_Nil[A]:
			return n
		default:
			panic(fmt.Sprintf("invalid List variant %T", l))
		}
	}
}

// All returns an iterator over the elements.
func All[A any](l List[A]) iter.Seq[A] {
	return func(yield func(A) bool) {
		for {
			switch c := l.(type) {
			case List_Cons[A]:
				if !yield(c.V0) {
					return
				}
				l = c.V1
			case List_Nil[A]:
				return
			default:
				panic(fmt.Sprintf("invalid List variant %T", l))
			}
		}
	}
}

// FromSlice converts a Go slice to a List. O(n) allocations.
func FromSlice[A any](xs []A) List[A] { return Of(xs...) }

// ToSlice copies the list into a fresh Go slice. O(n), one allocation.
// Note the explicit type arguments: List[A] is an interface whose type
// parameter appears nowhere in its method set, so A cannot be inferred
// from an already-generic List[A] argument — a wart of this encoding.
func ToSlice[A any](l List[A]) []A {
	out := make([]A, 0, Len[A](l))
	for x := range All[A](l) {
		out = append(out, x)
	}
	return out
}

// Map applies f to every element, returning a new list. Written
// recursively the way naive codegen for `match` would produce it: the
// recursion depth is O(n), which Go's growable stacks tolerate but the
// larger cell+frame cost is part of the measurement.
func Map[A, B any](l List[A], f func(A) B) List[B] {
	switch c := l.(type) {
	case List_Cons[A]:
		return List_Cons[B]{V0: f(c.V0), V1: Map(c.V1, f)}
	case List_Nil[A]:
		return List_Nil[B]{}
	default:
		panic(fmt.Sprintf("invalid List variant %T", l))
	}
}

// Fold reduces the list left to right, iteratively.
func Fold[A, B any](l List[A], z B, f func(B, A) B) B {
	acc := z
	for {
		switch c := l.(type) {
		case List_Cons[A]:
			acc = f(acc, c.V0)
			l = c.V1
		case List_Nil[A]:
			return acc
		default:
			panic(fmt.Sprintf("invalid List variant %T", l))
		}
	}
}
