package goiface

// Single-method interfaces exercising the @method width-exact
// conformance check one position at a time.

// WidthPutSlice — a slice element emits through goType, where both
// Int and Int64 spell int64: either spelling conforms.
type WidthPutSlice interface {
	Put(xs []int64) int64
}

// WidthPair — a fallible tuple return spreads over the leading raw
// results: each element width checks positionally.
type WidthPair interface {
	Pair() (int64, int64, error)
}

// WidthIntElem — a nested raw int element: no container spelling
// emits []int, so the interface is not implementable and
// conformance must fail rather than pass a wrong method set.
type WidthIntElem interface {
	Take(xs []int) int
}
