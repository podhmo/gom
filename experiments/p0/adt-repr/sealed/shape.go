// Package sealed implements Gom ADTs as a sealed interface plus one
// exported struct per variant: the representation proposed in
// docs/design.md section 4.1.
//
// Consumers construct variants with ordinary composite literals
// (Shape_Circle{Radius: 1.0}) and destructure them with a type switch.
// The unexported marker method keeps packages outside this one from
// adding new variants.
package sealed

import "github.com/podhmo/gom/experiments/p0/adt-repr/rt"

// type Shape =
//
//	| Circle (radius: Float)
//	| Rect (w: Float) (h: Float)
//	| Empty
type Shape interface{ isShape() }

type Shape_Circle struct{ Radius float64 }
type Shape_Rect struct{ W, H float64 }
type Shape_Empty struct{}

func (Shape_Circle) isShape() {}
func (Shape_Rect) isShape()   {}
func (Shape_Empty) isShape()  {}

// fn area (s: Shape) -> Float
func Area(s Shape) float64 {
	switch s := s.(type) {
	case Shape_Circle:
		return 3.14159 * s.Radius * s.Radius
	case Shape_Rect:
		return s.W * s.H
	case Shape_Empty:
		return 0.0
	default:
		panic(rt.InvalidVariant("sealed.Shape", s))
	}
}
