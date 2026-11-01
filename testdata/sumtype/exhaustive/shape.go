// Package exhaustive is the positive control of the go-check-sumtype
// canary (testdata/sumtype): the type switch in Area covers every
// variant of the sealed Shape interface, so the checker must stay
// silent here. If this package ever fails the check, the tool or the
// annotation convention regressed — not the fixture.
package exhaustive

// Shape is a sealed interface in the same style as the internal AST
// (docs/design.md section 2.4).

//sumtype:decl
type Shape interface{ isShape() }

// Shape_Circle is the Circle variant of [Shape].
type Shape_Circle struct{ Radius float64 }

// Shape_Rect is the Rect variant of [Shape].
type Shape_Rect struct{ W, H float64 }

// Shape_Empty is the Empty variant of [Shape].
type Shape_Empty struct{}

func (Shape_Circle) isShape() {}
func (Shape_Rect) isShape()   {}
func (Shape_Empty) isShape()  {}

// Area is exhaustive: every variant has a case and default is the
// conventional single panic(...) statement, which keeps the
// exhaustiveness check active (docs/design.md section 2.4).
func Area(s Shape) float64 {
	switch s := s.(type) {
	case Shape_Circle:
		return 3.14159 * s.Radius * s.Radius
	case Shape_Rect:
		return s.W * s.H
	case Shape_Empty:
		return 0.0
	default:
		panic("unreachable")
	}
}
