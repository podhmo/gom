package rt

// Tuples are the Go representation of Gom tuple values `(a, b, ...)`.
// Elements are stored in fields V0, V1, ... so generated code can
// destructure them with plain field access (t.V1).

// Tuple2 is a 2-element tuple.
type Tuple2[A, B any] struct {
	V0 A
	V1 B
}

// Tuple3 is a 3-element tuple.
type Tuple3[A, B, C any] struct {
	V0 A
	V1 B
	V2 C
}

// Tuple4 is a 4-element tuple.
type Tuple4[A, B, C, D any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
}

// Tuple5 is a 5-element tuple.
type Tuple5[A, B, C, D, E any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
}

// Tuple6 is a 6-element tuple.
type Tuple6[A, B, C, D, E, F any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
	V5 F
}

// Tuple7 is a 7-element tuple.
type Tuple7[A, B, C, D, E, F, G any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
	V5 F
	V6 G
}

// Tuple8 is an 8-element tuple.
type Tuple8[A, B, C, D, E, F, G, H any] struct {
	V0 A
	V1 B
	V2 C
	V3 D
	V4 E
	V5 F
	V6 G
	V7 H
}
