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

// WidthUint — a root raw uint position wants the gom Uint spelling;
// the method boundary converts between uint and the inner uint64.
type WidthUint interface {
	Put(x uint) uint
}

// WidthUintPair — root raw uint64 positions want Uint64.
type WidthUintPair interface {
	Pair() (uint64, uint64, error)
}

// WidthUintElem — nested raw uint is inexpressible, like nested raw int.
type WidthUintElem interface {
	Take(xs []uint) uint
}

// ApplyUint exercises a raw uint callback parameter in the Go→Gom direction.
func ApplyUint(f func(uint) uint, x uint) uint { return f(x) }

// UintFn exercises a raw uint callback result in the Gom→Go direction.
func UintFn() func(uint) uint { return func(x uint) uint { return x + 1 } }
