// Package-list benchmarks for the generated code (P5-3). These are
// hand-written beside the generated list.go the way boundary_test.go
// is — they measure the real bodies a compiled Gom program links
// against, so `go test -bench` needs no gom compile step.
//
// Workload axes follow
// docs/decisions/2026-11-05-generated-code-benchmark-harness.md:
// ops that only *read* the list run at 1e3/1e5 (the linear axis —
// per-element closure allocation shows in benchmem); ops that build
// the result by cons (`[x, ..rest]`) are the known O(n²) axis on the
// slice representation (P0-3) and stay at 256/2048 so benchtime
// measures the shape, not the asymptote.
package list

import (
	"fmt"
	"testing"
)

// sink keeps benchmark results live without letting a single shared
// field distort escape analysis per benchmark.
var (
	sinkList  List[int64]
	sinkFloat float64
	sinkInt   int64
)

func ints(n int) List[int64] {
	xs := make(List[int64], n)
	for i := range xs {
		xs[i] = int64(i)
	}
	return xs
}

var inc = func(x int64) int64 { return x + 1 }

// --- linear axis: read-only walks, sizes 1e3 / 1e5 ----------------

// BenchmarkFold feeds the arity-one curried folder Gom callers
// supply — `f(acc)(x)` per element is the allocation the function
// value representation pays on HOFs (P0-2 sketch, "catastrophic
// case").
func BenchmarkFold(b *testing.B) {
	addC := func(acc int64) func(int64) int64 {
		return func(x int64) int64 { return acc + x }
	}
	for _, n := range []int{1_000, 100_000} {
		xs := ints(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				sinkInt = Fold(addC, 0, xs)
			}
		})
	}
}

func BenchmarkSum(b *testing.B) {
	floats := make(List[float64], 100_000)
	for i := range floats {
		floats[i] = float64(i)
	}
	b.Run("n=100000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkFloat = Sum(floats)
		}
	})
}

func BenchmarkIter(b *testing.B) {
	xs := ints(100_000)
	noop := func(x int64) struct{} { return struct{}{} }
	b.Run("n=100000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Iter(noop, xs)
		}
	})
}

func BenchmarkFind(b *testing.B) {
	xs := ints(100_000)
	last := func(x int64) bool { return x == 99_999 }
	b.Run("n=100000/miss-mostly", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = Find(last, xs)
		}
	})
}

func BenchmarkLength(b *testing.B) {
	xs := ints(100_000)
	b.Run("n=100000", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkInt = Length(xs)
		}
	})
}

// --- quadratic axis: cons-built results, sizes 256 / 2048 ---------

func BenchmarkMap(b *testing.B) {
	for _, n := range []int{256, 2048} {
		xs := ints(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				sinkList = Map(inc, xs)
			}
		})
	}
}

func BenchmarkFilter(b *testing.B) {
	even := func(x int64) bool { return x%2 == 0 }
	for _, n := range []int{256, 2048} {
		xs := ints(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				sinkList = Filter(even, xs)
			}
		})
	}
}

func BenchmarkAppend(b *testing.B) {
	for _, n := range []int{256, 2048} {
		xs := ints(n)
		tail := ints(8)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				sinkList = Append(xs, tail)
			}
		})
	}
}

func BenchmarkRev(b *testing.B) {
	for _, n := range []int{256, 2048} {
		xs := ints(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				sinkList = Rev(xs)
			}
		})
	}
}

func BenchmarkRange(b *testing.B) {
	for _, n := range []int{256, 2048} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				sinkList = Range(0, int64(n))
			}
		})
	}
}

func BenchmarkConcat(b *testing.B) {
	for _, k := range []int{64, 512} {
		xss := make(List[List[int64]], k)
		for i := range xss {
			xss[i] = ints(8)
		}
		b.Run(fmt.Sprintf("k=%d", k), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				sinkList = Concat(xss)
			}
		})
	}
}
