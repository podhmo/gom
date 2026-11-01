package slicelist

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
// built by repeated x :: rest. Total work is O(n^2) for this rep.
func BenchmarkConsBuild(b *testing.B) {
	for _, n := range []int{100, 1_000, 10_000} {
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
// length k. O(k) per call for this rep.
func BenchmarkCons(b *testing.B) {
	for _, k := range []int{0, 100, 10_000} {
		rest := buildSlice(k)
		b.Run(fmt.Sprintf("k=%d", k), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkList = Cons(i, rest)
			}
		})
	}
}

// BenchmarkUncons destructures [x, ..rest]. O(1), no allocation, but the
// tail keeps the whole backing array alive (see retention package).
func BenchmarkUncons(b *testing.B) {
	l := buildSlice(10_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, r, _ := Uncons(l)
		sinkList = r
	}
}

func BenchmarkMap(b *testing.B) {
	for _, n := range []int{1_000, 100_000} {
		l := buildSlice(n)
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
		l := buildSlice(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkInt = Fold(l, 0, func(a, x int) int { return a + x })
			}
		})
	}
}

// BenchmarkRange compares native `for _, x := range l` (available only
// because List IS a slice) with the iter.Seq form the other reps need.
func BenchmarkRange(b *testing.B) {
	l := buildSlice(100_000)
	b.Run("native", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			s := 0
			for _, x := range l {
				s += x
			}
			sinkInt = s
		}
	})
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
		l := buildSlice(n)
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkAny = l.ToSlice()
			}
		})
	}
}

func BenchmarkLen(b *testing.B) {
	l := buildSlice(100_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkInt = len(l)
	}
}

func BenchmarkAppend(b *testing.B) {
	l := buildSlice(10_000)
	m := buildSlice(10_000)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkList = Append(l, m)
	}
}

func buildSlice(n int) List[int] {
	l := make(List[int], n)
	for i := range l {
		l[i] = i
	}
	return l
}
