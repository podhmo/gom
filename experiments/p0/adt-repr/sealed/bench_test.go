package sealed

import "testing"

var (
	sinkExpr   Expr
	sinkInt    int64
	sinkOption Option[int64]
	sinkBig    Option[[128]int64]
	sinkList   List[int64]
)

// ---- construction ----

// A variant that escapes into a global must be boxed: 1 allocation.
func BenchmarkConstructLit(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkExpr = Expr_Lit{V0: int64(i)}
	}
}

// A variant that stays local could in principle be boxed on the
// stack, but here Eval's interface parameter flows into a recursive
// call and the box still escapes: ~8 B/op of garbage remains.
func BenchmarkConstructLitNoEscape(b *testing.B) {
	b.ReportAllocs()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += Eval(Expr_Lit{V0: int64(i)})
	}
	sinkInt = acc
}

// Add boxing + two boxed children.
func BenchmarkConstructAdd(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkExpr = Expr_Add{V0: Expr_Lit{V0: int64(i)}, V1: Expr_Lit{V0: 2}}
	}
}

func BenchmarkConstructSome(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkOption = Option_Some[int64]{V0: int64(i)}
	}
}

// A zero-size variant boxes to the runtime's zerobase: no allocation.
func BenchmarkConstructNone(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkOption = Option_None[int64]{}
	}
}

func BenchmarkConstructCons(b *testing.B) {
	b.ReportAllocs()
	tail := ListFromSlice([]int64{1, 2, 3})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkList = List_Cons[int64]{V0: int64(i), V1: tail}
	}
}

// Boxing a large payload costs one allocation of the payload's size.
func BenchmarkConstructSomeBig(b *testing.B) {
	b.ReportAllocs()
	var big [128]int64
	for i := 0; i < b.N; i++ {
		sinkBig = Option_Some[[128]int64]{V0: big}
	}
}

// Build a fresh balanced tree; Add/Neg/Lit each box once.
func BenchmarkBuildExprTree(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkExpr = BuildTree(8)
	}
}

func BenchmarkBuildList(b *testing.B) {
	b.ReportAllocs()
	xs := make([]int64, 1000)
	for i := 0; i < b.N; i++ {
		sinkList = ListFromSlice(xs)
	}
}

// ---- dispatch ----

func BenchmarkEval(b *testing.B) {
	e := BuildTree(10)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = Eval(e)
	}
}

func BenchmarkArea(b *testing.B) {
	s := Shape(Shape_Rect{W: 2.0, H: 3.0})
	b.ReportAllocs()
	b.ResetTimer()
	var acc float64
	for i := 0; i < b.N; i++ {
		acc += Area(s)
	}
	sinkInt = int64(acc)
}

func BenchmarkGetOrElse(b *testing.B) {
	o := Option[int64](Option_Some[int64]{V0: 7})
	b.ReportAllocs()
	b.ResetTimer()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += GetOrElse(o, 9)
	}
	sinkInt = acc
}

func BenchmarkSum(b *testing.B) {
	l := ListFromSlice(make([]int64, 1000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = Sum(l)
	}
}

// ---- value-passing cost ----

func idOpt(o Option[int64]) Option[int64]           { return o }
func idBig(o Option[[128]int64]) Option[[128]int64] { return o }

// Interface values are two words; passing one never copies the payload.
func BenchmarkPassOption(b *testing.B) {
	b.ReportAllocs()
	o := Option[int64](Option_Some[int64]{V0: 7})
	for i := 0; i < b.N; i++ {
		sinkOption = idOpt(o)
	}
}

func BenchmarkPassBigOption(b *testing.B) {
	b.ReportAllocs()
	var big Option[[128]int64] = Option_Some[[128]int64]{}
	for i := 0; i < b.N; i++ {
		sinkBig = idBig(big)
	}
}

// Storing each variant into a distinct slice slot forces one real box
// per value: no amortization into a single heap slot.
func BenchmarkConstructLitSlice(b *testing.B) {
	b.ReportAllocs()
	out := make([]Expr, 1024)
	for i := 0; i < b.N; i++ {
		out[i&1023] = Expr_Lit{V0: int64(i)}
	}
	sinkInt += int64(len(out))
}
