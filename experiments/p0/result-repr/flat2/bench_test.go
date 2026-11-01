package flat2

import (
	"errors"
	"testing"

	"github.com/podhmo/gom/experiments/p0/result-repr/payload"
)

var (
	sinkResult    Result[int64, error]
	sinkResultBig Result[payload.Big, error]
	sinkInt       int64
	errSentinel   = errors.New("bench")
)

func BenchmarkConstructOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = Ok[int64, error](int64(i))
	}
}

func BenchmarkConstructErr(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = Err[int64](errSentinel)
	}
}

func BenchmarkGetOrElseOk(b *testing.B) {
	r := Ok[int64, error](7)
	b.ReportAllocs()
	b.ResetTimer()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += GetOrElse(r, 9)
	}
	sinkInt = acc
}

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

func BenchmarkParseSumOk(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkResult = ParseSum("1", "2")
	}
}

func idBig(r Result[payload.Big, error]) Result[payload.Big, error] { return r }

func BenchmarkPassBig(b *testing.B) {
	r := Ok[payload.Big, error](payload.Big{})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkResultBig = idBig(r)
	}
}
