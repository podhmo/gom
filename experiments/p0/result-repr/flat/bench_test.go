package flat

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

// Ok escaping into a global: the whole struct is copied, no boxing.
func BenchmarkConstructOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = Result[int64, error]{Value: int64(i), Ok: true}
	}
}

func BenchmarkConstructErr(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = Result[int64, error]{Error: errSentinel}
	}
}

// A ~1 KB payload is copied inline into the result struct — no
// allocation but a full 1 KB+ copy.
func BenchmarkConstructOkBig(b *testing.B) {
	b.ReportAllocs()
	var big payload.Big
	for i := 0; i < b.N; i++ {
		sinkResultBig = Result[payload.Big, error]{Value: big, Ok: true}
	}
}

func BenchmarkConstructOkSlice(b *testing.B) {
	b.ReportAllocs()
	out := make([]Result[int64, error], 1024)
	for i := 0; i < b.N; i++ {
		out[i&1023] = Result[int64, error]{Value: int64(i), Ok: true}
	}
	sinkInt += int64(len(out))
}

// ---- boundary conversion ----

func BenchmarkFromGoPairOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		v, err := goside.ParseInt("42", 10, 64)
		var r Result[int64, error]
		if err != nil {
			r = Result[int64, error]{Error: err}
		} else {
			r = Result[int64, error]{Value: v, Ok: true}
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
			r = Result[int64, error]{Error: err}
		} else {
			r = Result[int64, error]{Value: v, Ok: true}
		}
		sinkResult = r
	}
}

func BenchmarkResultToGoOk(b *testing.B) {
	r := Result[int64, error]{Value: 7, Ok: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, err := ResultToGo(r)
		sinkInt = v
		sinkErr = err
	}
}

func BenchmarkResultToGoErr(b *testing.B) {
	r := Result[int64, error]{Error: errSentinel}
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
	r := Result[int64, error]{Value: 7, Ok: true}
	b.ReportAllocs()
	b.ResetTimer()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += GetOrElse(r, 9)
	}
	sinkInt = acc
}

func BenchmarkGetOrElseErr(b *testing.B) {
	r := Result[int64, error]{Error: errSentinel}
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

func BenchmarkPassSmall(b *testing.B) {
	r := Result[int64, error]{Value: 7, Ok: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkResult = idRes(r)
	}
}

// Every pass copies the full 1 KB+ struct.
func BenchmarkPassBig(b *testing.B) {
	r := Result[payload.Big, error]{Ok: true}
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

func BenchmarkParsePortErrStep1(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParsePort("xx")
	}
}

func BenchmarkParsePortErrStep2(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParsePort("999999")
	}
}

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

func BenchmarkGetOrElseBig(b *testing.B) {
	r := Result[payload.Big, error]{Ok: true}
	var fb payload.Big
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkBig = GetOrElse(r, fb)
	}
}
