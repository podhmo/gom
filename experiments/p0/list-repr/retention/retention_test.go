// Package retention measures what the [x, ..rest] destructuring keeps
// alive under each List representation.
//
// Scenario (per the P0-3 task): build a large list, keep only a small
// suffix ("rest"), force a GC, and measure live heap. The slice
// representation retains the entire backing array — including the
// dropped prefix — because a sub-slice is a pointer into one shared
// array object. The cons representations retain only the cells that are
// still reachable.
package retention

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/podhmo/gom/experiments/p0/list-repr/consadt"
	"github.com/podhmo/gom/experiments/p0/list-repr/conslist"
	"github.com/podhmo/gom/experiments/p0/list-repr/slicelist"
)

func heapLive() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// TestTailRetention measures retained bytes after dropping all but the
// last keep elements of an n-element []int64 list.
func TestTailRetention(t *testing.T) {
	const (
		n    = 1 << 20 // 1,048,576 elements; 8 MiB as int64s
		keep = 8
	)
	baseline := heapLive()

	t.Run("slice", func(t *testing.T) {
		rest := func() slicelist.List[int64] {
			l := make(slicelist.List[int64], n)
			for i := range l {
				l[i] = int64(i)
			}
			return l[n-keep:] // [x, ..rest] all the way down
		}()
		got := heapLive() - baseline
		t.Logf("n=%d keep=%d: retained %d bytes (%.1f%% of the original array)",
			n, keep, got, 100*float64(got)/float64(n*8))
		if got < n*8*9/10 {
			t.Errorf("expected ~%d bytes retained by the shared backing array, got %d", n*8, got)
		}
		runtime.KeepAlive(rest)
	})

	t.Run("slice_suffix_copy", func(t *testing.T) {
		// The escape hatch: copying the small suffix into a fresh array
		// (FromSlice-style) releases the original backing array.
		rest := func() slicelist.List[int64] {
			l := make(slicelist.List[int64], n)
			for i := range l {
				l[i] = int64(i)
			}
			return slicelist.FromSlice(l[n-keep:])
		}()
		got := heapLive() - baseline
		t.Logf("n=%d keep=%d: retained %d bytes after copying the suffix", n, keep, got)
		if got > 1<<20 {
			t.Errorf("a copied suffix should release the backing array, retained %d bytes", got)
		}
		runtime.KeepAlive(rest)
	})

	t.Run("cons", func(t *testing.T) {
		rest := func() conslist.List[int64] {
			l := conslist.Nil[int64]()
			for i := n - 1; i >= 0; i-- {
				l = conslist.Cons(int64(i), l)
			}
			for i := 0; i < n-keep; i++ {
				l = l.Tail()
			}
			return l
		}()
		got := heapLive() - baseline
		t.Logf("n=%d keep=%d: retained %d bytes (keep cells only)", n, keep, got)
		if got > 1<<20 {
			t.Errorf("cons should retain only %d cells, got %d bytes", keep, got)
		}
		runtime.KeepAlive(rest)
	})

	t.Run("consadt", func(t *testing.T) {
		rest := func() consadt.List[int64] {
			var l consadt.List[int64] = consadt.Nil[int64]()
			for i := n - 1; i >= 0; i-- {
				l = consadt.Cons(int64(i), l)
			}
			for i := 0; i < n-keep; i++ {
				var ok bool
				_, l, ok = consadt.Uncons[int64](l)
				if !ok {
					t.Fatal("unexpected empty list")
				}
			}
			return l
		}()
		got := heapLive() - baseline
		t.Logf("n=%d keep=%d: retained %d bytes (keep cells only)", n, keep, got)
		if got > 1<<20 {
			t.Errorf("consadt should retain only %d cells, got %d bytes", keep, got)
		}
		runtime.KeepAlive(rest)
	})
}

// TestObjectGraphRetention shows the sharper form of the same hazard:
// when elements are pointers, the retained backing array keeps every
// pointed-to object alive, not just the slots.
func TestObjectGraphRetention(t *testing.T) {
	const (
		n    = 1 << 17 // 131,072 elements, each *block of 64 bytes -> ~8 MiB live data
		keep = 8
	)
	type block [64]byte
	baseline := heapLive()

	t.Run("slice", func(t *testing.T) {
		rest := func() slicelist.List[*block] {
			l := make(slicelist.List[*block], n)
			for i := range l {
				l[i] = new(block)
			}
			return l[n-keep:]
		}()
		got := heapLive() - baseline
		want := int64(n) * 64 // pointed-to objects alone
		t.Logf("n=%d keep=%d: retained %d bytes of pointed-to data", n, keep, got)
		if got < uint64(want)*9/10 {
			t.Errorf("expected >= %d retained bytes, got %d", want, got)
		}
		runtime.KeepAlive(rest)
	})

	t.Run("cons", func(t *testing.T) {
		rest := func() conslist.List[*block] {
			l := conslist.Nil[*block]()
			for i := 0; i < n; i++ {
				l = conslist.Cons(new(block), l)
			}
			for i := 0; i < n-keep; i++ {
				l = l.Tail()
			}
			return l
		}()
		got := heapLive() - baseline
		t.Logf("n=%d keep=%d: retained %d bytes of pointed-to data", n, keep, got)
		if got > 1<<20 {
			t.Errorf("cons should retain only %d cells+blocks, got %d bytes", keep, got)
		}
		runtime.KeepAlive(rest)
	})
}

// BenchmarkDropReportsRetainedBytes records the same measurement as a
// benchmark metric for the report table. The kept tail is stored in a
// package-level variable so it stays live during the measurement.
var (
	sliceSinkRetention slicelist.List[int64]
	consSinkRetention  conslist.List[int64]
)

func BenchmarkTailRetention(b *testing.B) {
	const (
		n    = 1 << 20
		keep = 8
	)
	b.Run("slice", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			sliceSinkRetention = func() slicelist.List[int64] {
				l := make(slicelist.List[int64], n)
				for j := range l {
					l[j] = int64(j)
				}
				return l[n-keep:]
			}()
		}
		b.StopTimer()
		consSinkRetention = conslist.Nil[int64]() // keep the metric to this rep's own tail
		b.ReportMetric(float64(heapLive()), "B-live-heap")
	})
	b.Run("cons", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			consSinkRetention = func() conslist.List[int64] {
				l := conslist.Nil[int64]()
				for j := n - 1; j >= 0; j-- {
					l = conslist.Cons(int64(j), l)
				}
				for j := 0; j < n-keep; j++ {
					l = l.Tail()
				}
				return l
			}()
		}
		b.StopTimer()
		sliceSinkRetention = nil // keep the metric to this rep's own tail
		b.ReportMetric(float64(heapLive()), "B-live-heap")
	})
}

// Example of how a prefix-drop keeps capacity on the slice rep.
func Example_tailSharesBacking() {
	const n = 8
	l := make(slicelist.List[int], n)
	for i := range l {
		l[i] = i
	}
	// drop everything but the last 2 elements
	rest := l[n-2:]
	fmt.Println(rest[0], rest[1], "cap:", cap(rest)) // cap reaches back to the big array
	// Output:
	// 6 7 cap: 2
}
