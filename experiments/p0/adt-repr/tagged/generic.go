package tagged

import "github.com/podhmo/gom/experiments/p0/adt-repr/rt"

// type Option a =
//
//	| Some a
//	| None
type Option[A any] struct {
	tag  OptionTag
	some A // Some's payload, stored inline
}

type OptionTag uint8

const (
	OptionTag_Invalid OptionTag = iota // zero value: not a real variant
	OptionTag_Some
	OptionTag_None
)

func Option_Some[A any](v A) Option[A] {
	return Option[A]{tag: OptionTag_Some, some: v}
}
func Option_None[A any]() Option[A] {
	return Option[A]{tag: OptionTag_None}
}

func (o Option[A]) Tag() OptionTag { return o.tag }
func (o Option[A]) AsSome() A      { return o.some }

// fn get_or_else (o: Option a) (fallback: a) -> a
func GetOrElse[A any](o Option[A], fallback A) A {
	switch o.Tag() {
	case OptionTag_Some:
		return o.AsSome()
	case OptionTag_None:
		return fallback
	default:
		panic(rt.InvalidVariant("tagged.Option", o))
	}
}

// type List a =
//
//	| Cons a (List a)
//	| Nil
type List[A any] struct {
	tag  ListTag
	cons *listCons[A] // Cons's payload; pointer because it contains List
}

type listCons[A any] struct {
	head A
	tail List[A]
}

type ListTag uint8

const (
	ListTag_Invalid ListTag = iota // zero value: not a real variant
	ListTag_Cons
	ListTag_Nil
)

func List_Cons[A any](head A, tail List[A]) List[A] {
	return List[A]{tag: ListTag_Cons, cons: &listCons[A]{head: head, tail: tail}}
}
func List_Nil[A any]() List[A] {
	return List[A]{tag: ListTag_Nil}
}

func (l List[A]) Tag() ListTag         { return l.tag }
func (l List[A]) AsCons() (A, List[A]) { return l.cons.head, l.cons.tail }

// fn sum (l: List Int) -> Int
func Sum(l List[int64]) int64 {
	var acc int64
	for {
		switch l.Tag() {
		case ListTag_Cons:
			head, tail := l.AsCons()
			acc += head
			l = tail
		case ListTag_Nil:
			return acc
		default:
			panic(rt.InvalidVariant("tagged.List", l))
		}
	}
}

// ListFromSlice builds Cons(x0, Cons(x1, ... Nil)).
func ListFromSlice(xs []int64) List[int64] {
	l := List_Nil[int64]()
	for i := len(xs) - 1; i >= 0; i-- {
		l = List_Cons(xs[i], l)
	}
	return l
}
