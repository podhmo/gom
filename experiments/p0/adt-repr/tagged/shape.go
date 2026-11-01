// Package tagged implements Gom ADTs as a single struct holding a tag
// plus the payload fields of every variant, the alternative to the
// sealed-interface representation in docs/design.md section 4.1.
//
// All fields are unexported so that consumers cannot corrupt the tag or
// mix payloads across variants. Consumers construct values through the
// generated constructor functions (Shape_Circle(1.0)), dispatch on
// Shape.Tag(), and read payloads through the As* accessors.
package tagged

import "github.com/podhmo/gom/experiments/p0/adt-repr/rt"

// type Shape =
//
//	| Circle (radius: Float)
//	| Rect (w: Float) (h: Float)
//	| Empty
type Shape struct {
	tag ShapeTag
	// Only the payload fields of the variant named by tag are
	// meaningful; the rest stay at their zero values.
	circleRadius float64
	rectW        float64
	rectH        float64
}

type ShapeTag uint8

const (
	ShapeTag_Invalid ShapeTag = iota // zero value: not a real variant
	ShapeTag_Circle
	ShapeTag_Rect
	ShapeTag_Empty
)

// Constructors, one per variant.

func Shape_Circle(radius float64) Shape {
	return Shape{tag: ShapeTag_Circle, circleRadius: radius}
}
func Shape_Rect(w, h float64) Shape {
	return Shape{tag: ShapeTag_Rect, rectW: w, rectH: h}
}
func Shape_Empty() Shape {
	return Shape{tag: ShapeTag_Empty}
}

// Tag and destructuring accessors.

func (s Shape) Tag() ShapeTag              { return s.tag }
func (s Shape) AsCircle() float64          { return s.circleRadius }
func (s Shape) AsRect() (float64, float64) { return s.rectW, s.rectH }

// fn area (s: Shape) -> Float
func Area(s Shape) float64 {
	switch s.Tag() {
	case ShapeTag_Circle:
		r := s.AsCircle()
		return 3.14159 * r * r
	case ShapeTag_Rect:
		w, h := s.AsRect()
		return w * h
	case ShapeTag_Empty:
		return 0.0
	default:
		panic(rt.InvalidVariant("tagged.Shape", s))
	}
}
