package sealed

import (
	"errors"
	"testing"

	"github.com/podhmo/gom/experiments/p0/result-repr/goside"
	"github.com/podhmo/gom/experiments/p0/result-repr/payload"
)

var (
	sinkResult    Result[int64, error]
	sinkResultBig Result[payload.Big, error]
	sinkInt       int64
	sinkErr       error
	sinkBig       payload.Big
	errSentinel   = errors.New("bench")
)

// ---- construction ----

// Ok escaping into a global: one box for the variant struct.
func BenchmarkConstructOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = Result_Ok[int64, error]{V0: int64(i)}
	}
}

func BenchmarkConstructErr(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = Result_Err[int64, error]{V0: errSentinel}
	}
}

// Boxing a ~1 KB payload costs one allocation of the payload's size.
func BenchmarkConstructOkBig(b *testing.B) {
	b.ReportAllocs()
	var big payload.Big
	for i := 0; i < b.N; i++ {
		sinkResultBig = Result_Ok[payload.Big, error]{V0: big}
	}
}

// Storing each variant into a distinct slice slot forces one real box
// per value.
func BenchmarkConstructOkSlice(b *testing.B) {
	b.ReportAllocs()
	out := make([]Result[int64, error], 1024)
	for i := 0; i < b.N; i++ {
		out[i&1023] = Result_Ok[int64, error]{V0: int64(i)}
	}
	sinkInt += int64(len(out))
}

// ---- boundary conversion ----

// The fused (T, error) -> Result shape codegen emits inside |?> / try.
func BenchmarkFromGoPairOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, err := goside.ParseInt("42", 10, 64)
		var r Result[int64, error]
		if err != nil {
			r = Result_Err[int64, error]{V0: err}
		} else {
			r = Result_Ok[int64, error]{V0: v}
		}
		sinkResult = r
	}
}

func BenchmarkFromGoPairErr(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, err := goside.ParseInt("x", 10, 64)
		var r Result[int64, error]
		if err != nil {
			r = Result_Err[int64, error]{V0: err}
		} else {
			r = Result_Ok[int64, error]{V0: v}
		}
		sinkResult = r
	}
}

// Result -> (T, error): what a Go caller pays to unwrap.
func BenchmarkResultToGoOk(b *testing.B) {
	r := Result[int64, error](Result_Ok[int64, error]{V0: 7})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := ResultToGo(r)
		sinkInt = v
		sinkErr = err
	}
}

func BenchmarkResultToGoErr(b *testing.B) {
	r := Result[int64, error](Result_Err[int64, error]{V0: errSentinel})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := ResultToGo(r)
		sinkInt = v
		sinkErr = err
	}
}

// ---- match / destructure ----

func BenchmarkGetOrElseOk(b *testing.B) {
	r := Result[int64, error](Result_Ok[int64, error]{V0: 7})
	b.ReportAllocs()
	b.ResetTimer()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += GetOrElse(r, 9)
	}
	sinkInt = acc
}

func BenchmarkGetOrElseErr(b *testing.B) {
	r := Result[int64, error](Result_Err[int64, error]{V0: errSentinel})
	b.ReportAllocs()
	b.ResetTimer()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += GetOrElse(r, 9)
	}
	sinkInt = acc
}

// ---- pass by value ----

func idRes(r Result[int64, error]) Result[int64, error]             { return r }
func idBig(r Result[payload.Big, error]) Result[payload.Big, error] { return r }

// Interface values are two words; passing one never copies the payload.
func BenchmarkPassSmall(b *testing.B) {
	r := Result[int64, error](Result_Ok[int64, error]{V0: 7})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkResult = idRes(r)
	}
}

// 1 KB payload stays behind the interface handle: still two words.
func BenchmarkPassBig(b *testing.B) {
	r := Result[payload.Big, error](Result_Ok[payload.Big, error]{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkResultBig = idBig(r)
	}
}

// ---- |?> chains (design.md 4.6 fused codegen) ----

func BenchmarkParsePortOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParsePort("8080")
	}
}

// strconv fails: one %w wrap + one Err box.
func BenchmarkParsePortErrStep1(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParsePort("xx")
	}
}

// check_port_range fails: same cost shape, second step.
func BenchmarkParsePortErrStep2(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParsePort("999999")
	}
}

// Same chain without the elision: a Result materializes per step.
func BenchmarkParsePortNaiveOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParsePortNaive("8080")
	}
}

func BenchmarkParsePortNaiveErrStep1(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParsePortNaive("xx")
	}
}

// ---- try chain ----

func BenchmarkParseSumOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParseSum("1", "2")
	}
}

func BenchmarkParseSumErr(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParseSum("x", "2")
	}
}

// ---- large value across the boundary ----

func BenchmarkLoadBigOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResultBig = LoadConfigBig(false)
	}
}

func BenchmarkLoadBigErr(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResultBig = LoadConfigBig(true)
	}
}

// Destructuring a big Ok copies the whole payload out of its box.
func BenchmarkGetOrElseBig(b *testing.B) {
	r := Result[payload.Big, error](Result_Ok[payload.Big, error]{})
	var fb payload.Big
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBig = GetOrElse(r, fb)
	}
}
