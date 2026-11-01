package conslist

import (
	"fmt"
	"testing"
)

var (
	sinkInt  int
	sinkList List[int]
	sinkAny  any
)

// BenchmarkConsBuild is the prepend-heavy workload: a list of n elements
// built by repeated x :: rest. O(n) total for this rep, one cell per
// element. The slice rep could not run the two largest sizes (O(n^2)).
func BenchmarkConsBuild(b *testing.B) {
	for _, n := range []int{100, 1_000, 10_000, 100_000, 1_000_000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var l List[int]
				for j := 0; j < n; j++ {
					l = Cons(j, l)
				}
				sinkList = l
			}
		})
	}
}

// BenchmarkCons measures a single prepend against a rest of fixed
// length k. O(1) regardless of k.
func BenchmarkCons(b *testing.B) {
	for _, k := range []int{0, 100, 10_000} {
		rest := buildCons(k)
		b.Run(fmt.Sprintf("k=%d", k), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkList = Cons(i, rest)
			}
		})
	}
}

// BenchmarkUncons destructures [x, ..rest]. O(1), no allocation, and the
// tail releases the dropped head cell rather than retaining the array.
func BenchmarkUncons(b *testing.B) {
	l := buildCons(10_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, r, _ := Uncons(l)
		sinkList = r
	}
}

func BenchmarkMap(b *testing.B) {
	for _, n := range []int{1_000, 100_000} {
		l := buildCons(n)
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
		l := buildCons(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkInt = Fold(l, 0, func(a, x int) int { return a + x })
			}
		})
	}
}

// BenchmarkRange measures `for x := range l.All()`, the iteration form
// Go callers get on this rep.
func BenchmarkRange(b *testing.B) {
	l := buildCons(100_000)
	b.Run("iterseq", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s := 0
			for x := range l.All() {
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
		l := buildCons(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkAny = l.ToSlice()
			}
		})
	}
}

// BenchmarkLen shows the O(n) cost of recovering what a slice header
// stores for free.
func BenchmarkLen(b *testing.B) {
	l := buildCons(100_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkInt = l.Len()
	}
}

func BenchmarkAppend(b *testing.B) {
	l := buildCons(10_000)
	m := buildCons(10_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkList = Append(l, m)
	}
}

func buildCons(n int) List[int] {
	l := Nil[int]()
	for i := n - 1; i >= 0; i-- {
		l = Cons(i, l)
	}
	return l
}
