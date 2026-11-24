// The Go side of the spec suite's foreign-package helper: gom
// imports resolve this directory through its .gomi, and generated
// callers typecheck against this file — written the way `gom build`
// would emit the same declarations (sealed interface + variant
// structs, records as structs).
package foreignpkg

import "github.com/podhmo/gom/stdlib/list"

type Point struct {
	X float64
	Y float64
}

type Shape interface{ isShape() }

type Shape_Circle struct {
	R float64
}

func (Shape_Circle) isShape() {}

type Shape_Dot struct {
	P Point
}

func (Shape_Dot) isShape() {}

type Blob struct {
	D list.List[int64]
}
