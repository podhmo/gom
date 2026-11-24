package foreignpkg

import "github.com/podhmo/gom/stdlib/list"

// Mirrors what `gom build` emits for the .gomi declarations so
// generated callers (FuzzCompile, gocall) typecheck against the
// same shapes — a one-field record is a defined type over the
// field's own type, not a struct.

type Point struct {
	Fpx, Fpy float64
}

type Shape interface{ isShape() }

type Shape_Circle struct{ R float64 }

func (Shape_Circle) isShape() {}

type Shape_Dot struct{ P Point }

func (Shape_Dot) isShape() {}

type Blob list.List[int64]

type Chunk interface{ isChunk() }

type Chunk_Piece struct{ Xs list.List[int64] }

func (Chunk_Piece) isChunk() {}
