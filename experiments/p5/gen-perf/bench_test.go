package genperf

import "testing"

var (
	sinkI64 int64
	sinkR   interface {
		Tag() int
	}
)

var (
	// opaque values keep Go from devirtualizing — this is what a call
	// through a stored/passed function value pays.
	opaquePlus  = plus
	opaqueAgain = again
	opaqueParse = parse
)

// The curried alias: `plus x y` as a value pays a captured-closure
// allocation per call — the arity-one value repr.
func BenchmarkCurriedAlias(b *testing.B) {
	b.Run("direct add", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkI64 = add(20, 22)
		}
	})
	b.Run("curried plus", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkI64 = opaquePlus(20)(22)
		}
	})
	// Provable (same-scope literal): Go should eliminate the closure.
	b.Run("curried plus local", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkI64 = plus(20)(22)
		}
	})
}

func BenchmarkArity1Alias(b *testing.B) {
	b.Run("count direct", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkI64 = count(8)
		}
	})
	b.Run("again opaque", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkI64 = opaqueAgain(8)
		}
	})
}

// The |?> IIFE captures s; immediately-invoked non-escaping closures
// should stay on the stack — benchmem shows whether they do.
func BenchmarkPipeIIFE(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r := opaqueParse("12345")
		sinkI64 = 0
		if r.Tag() == 1 {
			sinkI64 = 1
		}
	}
}
