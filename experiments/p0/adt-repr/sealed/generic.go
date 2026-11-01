package sealed

import "github.com/podhmo/gom/experiments/p0/adt-repr/rt"

// type Option a =
//
//	| Some a
//	| None
//
// The marker method's signature mentions A on purpose: Go type
// inference cannot bind a type parameter that appears only in the
// instantiated interface's name (interface{ isOption() }), because it
// unifies types through their structure, and A is not part of the
// method set's structure. With isOption() A, a call like
// GetOrElse(o, 9) infers A from o alone.
//
// (go-check-sumtype skips generic variants, so the annotation is
// documentation only here — docs/sketch/2026-11-02-sumtype-check-selection.md.)
//
//sumtype:decl
type Option[A any] interface{ isOption() A }

type Option_Some[A any] struct{ V0 A }
type Option_None[A any] struct{}

func (o Option_Some[A]) isOption() A { return o.V0 }
func (Option_None[A]) isOption() A   { var zero A; return zero }

// fn get_or_else (o: Option a) (fallback: a) -> a
func GetOrElse[A any](o Option[A], fallback A) A {
	switch o := o.(type) {
	case Option_Some[A]:
		return o.V0
	case Option_None[A]:
		return fallback
	default:
		panic(rt.InvalidVariant("sealed.Option", o))
	}
}

func IsSome[A any](o Option[A]) bool {
	switch o.(type) {
	case Option_Some[A]:
		return true
	case Option_None[A]:
		return false
	default:
		panic(rt.InvalidVariant("sealed.Option", o))
	}
}

// type List a =
//
//	| Cons a (List a)
//	| Nil
//
// (go-check-sumtype skips generic variants — see the note on Option.)
//
//sumtype:decl
type List[A any] interface{ isList() A }

type List_Cons[A any] struct {
	V0 A
	V1 List[A]
}
type List_Nil[A any] struct{}

func (l List_Cons[A]) isList() A { return l.V0 }
func (List_Nil[A]) isList() A    { var zero A; return zero }

// fn sum (l: List Int) -> Int
func Sum(l List[int64]) int64 {
	var acc int64
	for {
		switch v := l.(type) {
		case List_Cons[int64]:
			acc += v.V0
			l = v.V1
		case List_Nil[int64]:
			return acc
		default:
			panic(rt.InvalidVariant("sealed.List", l))
		}
	}
}

// ListFromSlice builds Cons(x0, Cons(x1, ... Nil)).
func ListFromSlice(xs []int64) List[int64] {
	var l List[int64] = List_Nil[int64]{}
	for i := len(xs) - 1; i >= 0; i-- {
		l = List_Cons[int64]{V0: xs[i], V1: l}
	}
	return l
}
