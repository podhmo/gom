// Package nonexhaustive is the deliberate-failure half of the
// go-check-sumtype canary (testdata/sumtype): the type switch in Area
// has no Shape_Rect case, so the checker must report the missing
// variant and exit non-zero. scripts/check-sumtype.sh asserts both.
// Do NOT add the missing case — this file exists to prove that adding
// a variant to a sealed interface breaks `make check`.
package nonexhaustive

// Shape mirrors exhaustive.Shape with the same sealed-interface style.

//sumtype:decl
type Shape interface{ isShape() }

// Shape_Circle is the Circle variant of [Shape].
type Shape_Circle struct{ Radius float64 }

// Shape_Rect is the Rect variant of [Shape]. Area deliberately lacks
// its case clause: this is the missing variant the canary detects.
type Shape_Rect struct{ W, H float64 }

// Shape_Empty is the Empty variant of [Shape].
type Shape_Empty struct{}

func (Shape_Circle) isShape() {}
func (Shape_Rect) isShape()   {}
func (Shape_Empty) isShape()  {}

// Area is intentionally non-exhaustive: Shape_Rect has no case.
// The conventional single panic(...) default keeps the check active.
func Area(s Shape) float64 {
	switch s := s.(type) {
	case Shape_Circle:
		return 3.14159 * s.Radius * s.Radius
	case Shape_Empty:
		return 0.0
	default:
		panic("unreachable")
	}
}
