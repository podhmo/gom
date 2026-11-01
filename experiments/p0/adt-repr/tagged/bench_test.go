package tagged

import "testing"

var (
	sinkExpr   Expr
	sinkInt    int64
	sinkOption Option[int64]
	sinkBig    Option[[128]int64]
	sinkList   List[int64]
)

// ---- construction ----

// A variant with an inline payload is a plain struct store: no
// allocation even when it escapes into a global.
func BenchmarkConstructLit(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkExpr = Expr_Lit(int64(i))
	}
}

func BenchmarkConstructLitNoEscape(b *testing.B) {
	b.ReportAllocs()
	var acc int64
	for i := 0; i < b.N; i++ {
		acc += Eval(Expr_Lit(int64(i)))
	}
	sinkInt = acc
}

// The recursive Add payload must be allocated, but the children are
// copied into it: 1 allocation total instead of 3.
func BenchmarkConstructAdd(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkExpr = Expr_Add(Expr_Lit(int64(i)), Expr_Lit(2))
	}
}

func BenchmarkConstructSome(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkOption = Option_Some(int64(i))
	}
}

func BenchmarkConstructNone(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkOption = Option_None[int64]()
	}
}

func BenchmarkConstructCons(b *testing.B) {
	b.ReportAllocs()
	tail := ListFromSlice([]int64{1, 2, 3})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkList = List_Cons(int64(i), tail)
	}
}

// A large payload stays inline: no allocation, but every store copies
// the full payload.
func BenchmarkConstructSomeBig(b *testing.B) {
	b.ReportAllocs()
	var big [128]int64
	for i := 0; i < b.N; i++ {
		sinkBig = Option_Some[[128]int64](big)
	}
}

// Build a fresh balanced tree; Lit is free, Add and Neg allocate their
// payload pointer once each.
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
	s := Shape_Rect(2.0, 3.0)
	b.ReportAllocs()
	b.ResetTimer()
	var acc float64
	for i := 0; i < b.N; i++ {
		acc += Area(s)
	}
	sinkInt = int64(acc)
}

func BenchmarkGetOrElse(b *testing.B) {
	o := Option_Some[int64](7)
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

// The Option struct is tag+payload; passing it copies both words.
func BenchmarkPassOption(b *testing.B) {
	b.ReportAllocs()
	o := Option_Some[int64](7)
	for i := 0; i < b.N; i++ {
		sinkOption = idOpt(o)
	}
}

// Passing a large tagged Option copies the whole payload on every
// call, where the sealed version copies a two-word interface.
func BenchmarkPassBigOption(b *testing.B) {
	b.ReportAllocs()
	var big [128]int64
	o := Option_Some[[128]int64](big)
	for i := 0; i < b.N; i++ {
		sinkBig = idBig(o)
	}
}

// Same shape as the sealed version: the slice holds struct values, so
// elements are stored inline with no per-element allocation.
func BenchmarkConstructLitSlice(b *testing.B) {
	b.ReportAllocs()
	out := make([]Expr, 1024)
	for i := 0; i < b.N; i++ {
		out[i&1023] = Expr_Lit(int64(i))
	}
	sinkInt += int64(len(out))
}
