package noncons

import (
	"testing"

	"github.com/podhmo/gom/stdlib/list"
)

func xsOf(n int) list.List[int64] {
	xs := list.List[int64]{}
	for i := int64(n); i >= 1; i-- {
		xs = append(list.List[int64]{i}, xs...)
	}
	return xs
}

func benchSizes() []int { return []int{500, 2000, 8000} }

// 比較基準（被覆形）: 線形 + ほぼ定数回の alloc を期待
func BenchmarkConsMap(b *testing.B) {
	for _, n := range benchSizes() {
		xs := xsOf(n)
		b.Run(intName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ConsMap(xs)
			}
		})
	}
}

// (i) 連鎖 append: k レベル × chunk 8 で総要素 8k、append 左辺コピーで O(k²)
func BenchmarkChainAppend(b *testing.B) {
	for _, k := range []int{125, 500, 2000} { // 総要素 1000/4000/16000
		b.Run(intName(k*8), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ChainAppend(int64(k))
			}
		})
	}
}

// (ii) fold 内 concat: 1 要素ごとに acc 全体をコピー → O(n²)
func BenchmarkFoldConcat(b *testing.B) {
	for _, n := range benchSizes() {
		xs := xsOf(n)
		b.Run(intName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = FoldConcat(xs)
			}
		})
	}
}

// (iii) 相互再帰: cons コピーで O(n²) + Go スタック消費
func BenchmarkMutual(b *testing.B) {
	for _, n := range benchSizes() {
		b.Run(intName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = MutA(int64(n))
			}
		})
	}
}

// (iv) 呼び出し結果の 2 度読み: 要件 4 除外形、O(n²)
func BenchmarkReadTwice(b *testing.B) {
	for _, n := range benchSizes() {
		xs := xsOf(n)
		b.Run(intName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = ReadTwice(xs)
			}
		})
	}
}

func intName(n int) string {
	return "n" + itoa(int64(n))
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
