package consadt

import (
	"fmt"
	"testing"
)

var (
	sinkInt  int
	sinkList List[int]
	sinkAny  any
)

// BenchmarkConsBuild: prepend-heavy workload on the ADT encoding.
// Each cell costs one heap allocation of List_Cons (value + interface
// word for the boxed tail).
func BenchmarkConsBuild(b *testing.B) {
	for _, n := range []int{100, 1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var l List[int] = Nil[int]()
				for j := 0; j < n; j++ {
					l = Cons(j, l)
				}
				sinkList = l
			}
		})
	}
}

// BenchmarkCons: O(1) prepend, one boxing allocation per cell.
func BenchmarkCons(b *testing.B) {
	for _, k := range []int{0, 100, 10_000} {
		rest := buildADT(k)
		b.Run(fmt.Sprintf("k=%d", k), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkList = Cons(i, rest)
			}
		})
	}
}

// BenchmarkUncons: [x, ..rest] destructuring via type switch.
func BenchmarkUncons(b *testing.B) {
	l := buildADT(10_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, r, _ := Uncons[int](l)
		sinkList = r
	}
}

// BenchmarkMap uses the naive recursive form codegen would emit.
// Depth is O(n); n is capped at 100k so the grown stack stays sane.
func BenchmarkMap(b *testing.B) {
	for _, n := range []int{1_000, 100_000} {
		l := buildADT(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkList = Map(l, func(x int) int { return x * 2 })
			}
		})
	}
}

func BenchmarkFold(b *testing.B) {
	for _, n := range []int{1_000, 100_000, 1_000_000} {
		l := buildADT(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkInt = Fold(l, 0, func(a, x int) int { return a + x })
			}
		})
	}
}

// BenchmarkRange measures `for x := range All[int](l)` with the type switch
// inside the iterator.
func BenchmarkRange(b *testing.B) {
	l := buildADT(100_000)
	b.Run("iterseq", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s := 0
			for x := range All[int](l) {
				s += x
			}
			sinkInt = s
		}
	})
}

func BenchmarkFromSlice(b *testing.B) {
	for _, n := range []int{1_000, 100_000} {
		src := make([]int, n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkList = FromSlice(src)
			}
		})
	}
}

func BenchmarkToSlice(b *testing.B) {
	for _, n := range []int{1_000, 100_000} {
		l := buildADT(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkAny = ToSlice[int](l)
			}
		})
	}
}

func BenchmarkLen(b *testing.B) {
	l := buildADT(100_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkInt = Len[int](l)
	}
}

func buildADT(n int) List[int] {
	l := Nil[int]()
	for i := n - 1; i >= 0; i-- {
		l = Cons(i, l)
	}
	return l
}
