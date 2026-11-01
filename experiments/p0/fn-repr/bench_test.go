package fnrepr_test

// Benchmarks comparing the two function-value representations on the same
// hypothetical Gom module. Each sub-benchmark is annotated with the Gom
// expression it models.
//
// "opaque" variants take the function value from a package-level var, so
// the compiler cannot devirtualize the call — this models a function value
// that crossed a boundary (parameter, stored field, value from another
// package). "provable" variants bind and apply the value in the same
// scope, where Go's inliner and escape analysis can eliminate the
// closures entirely.
//
//	go test -bench=. -benchmem

import (
	"slices"
	"strconv"
	"testing"

	"github.com/podhmo/gom/experiments/p0/fn-repr/arity"
	"github.com/podhmo/gom/experiments/p0/fn-repr/curried"
)

var (
	sinkInt     int
	sinkSlice   []int
	sinkStrings []string
	sinkC3      func(int) func(int) func(int) int
	sinkC2      func(int) func(int) int
	sinkA3      func(int, int, int) int
	sinkA2      func(int, int) int
	benchInput  = func() []int {
		xs := make([]int, 1024)
		for i := range xs {
			xs[i] = i
		}
		return xs
	}()
)

// Opaque function values: the compiler cannot see what these hold.
var (
	unknownCurried3 = curried.Add3Val
	unknownArity3   = arity.Add3
	unknownCurried2 = curried.Add3_1(1)
	unknownArity2   = arity.Add3_1(1)
	unknownCLabel   = curried.Transfer_Amount(10)
	unknownALabel   = arity.Transfer_Amount(10)
)

// `add3 i 2 3` — saturated call to a known function.
func BenchmarkSaturatedCall(b *testing.B) {
	b.Run("curried", func(b *testing.B) {
		s := 0
		for i := 0; i < b.N; i++ {
			s += curried.Add3(i, 2, 3)
		}
		sinkInt = s
	})
	b.Run("arity", func(b *testing.B) {
		s := 0
		for i := 0; i < b.N; i++ {
			s += arity.Add3(i, 2, 3)
		}
		sinkInt = s
	})
}

// `add3` mentioned as a value — `let f = add3`. In the curried style the
// compiler either references one shared wrapper per instantiation or
// re-creates the wrapper at each mention.
func BenchmarkValueForm(b *testing.B) {
	b.Run("curried-shared", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkC3 = curried.Add3Val
		}
	})
	b.Run("curried-inline", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkC3 = curried.Curry3(curried.Add3)
		}
	})
	b.Run("arity", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkA3 = arity.Add3
		}
	})
}

// `f i 2 3` — an unknown function value applied to all arguments.
func BenchmarkValueFullApply(b *testing.B) {
	b.Run("curried-opaque", func(b *testing.B) {
		f := unknownCurried3
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i)(2)(3)
		}
		sinkInt = s
	})
	b.Run("arity-opaque", func(b *testing.B) {
		f := unknownArity3
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i, 2, 3)
		}
		sinkInt = s
	})
	b.Run("curried-provable", func(b *testing.B) {
		// the curried value form written out inline, in scope
		f := func(a int) func(int) func(int) int {
			return func(b int) func(int) int {
				return func(c int) int {
					return a + b + c
				}
			}
		}
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i)(2)(3)
		}
		sinkInt = s
	})
	b.Run("arity-provable", func(b *testing.B) {
		f := arity.Add3
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i, 2, 3)
		}
		sinkInt = s
	})
}

// The same unknown value applied strictly one argument at a time — the
// only path available in the curried style. In the arity style the IR can
// always fuse adjacent applications, but even if it did not, the staged
// form costs the same as the curried chain.
func BenchmarkValueStagedApply(b *testing.B) {
	b.Run("curried", func(b *testing.B) {
		f := unknownCurried3
		s := 0
		for i := 0; i < b.N; i++ {
			g := f(i) // f i
			h := g(2) // (f i) 2
			s += h(3) // (f i 2) 3
		}
		sinkInt = s
	})
	b.Run("arity-unfused", func(b *testing.B) {
		f := unknownArity3
		s := 0
		for i := 0; i < b.N; i++ {
			g := arity.Apply1of3(f, i) // f i
			h := arity.Apply1of2(g, 2) // (f i) 2
			s += h(3)                  // (f i 2) 3
		}
		sinkInt = s
	})
}

// `add3 i` — create a partial application (two arguments remain).
func BenchmarkPartialCreate(b *testing.B) {
	b.Run("curried", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkC2 = curried.Add3_1(i)
		}
	})
	b.Run("arity", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkA2 = arity.Add3_1(i)
		}
	})
}

// `let g = add3 1 in g i 3` — a partial application applied to the
// remaining arguments.
func BenchmarkPartialApplyRest(b *testing.B) {
	b.Run("curried-opaque", func(b *testing.B) {
		g := unknownCurried2
		s := 0
		for i := 0; i < b.N; i++ {
			s += g(i)(3)
		}
		sinkInt = s
	})
	b.Run("arity-opaque", func(b *testing.B) {
		g := unknownArity2
		s := 0
		for i := 0; i < b.N; i++ {
			s += g(i, 3)
		}
		sinkInt = s
	})
	b.Run("curried-provable", func(b *testing.B) {
		g := curried.Add3_1(1)
		s := 0
		for i := 0; i < b.N; i++ {
			s += g(i)(3)
		}
		sinkInt = s
	})
	b.Run("arity-provable", func(b *testing.B) {
		g := arity.Add3_1(1)
		s := 0
		for i := 0; i < b.N; i++ {
			s += g(i, 3)
		}
		sinkInt = s
	})
}

// `fold add2 0 xs` over 1024 elements — the per-element cost of a
// 2-argument function parameter. curried-uncurry models converting a
// curried value for a specialized API; curried-flat models the design.md
// 4.4 specialization fed directly by a flat Go function.
func BenchmarkFold(b *testing.B) {
	b.Run("curried", func(b *testing.B) {
		f := curried.Add2Val
		s := 0
		for i := 0; i < b.N; i++ {
			s += curried.Fold(f, 0, benchInput)
		}
		sinkInt = s
	})
	b.Run("curried-uncurry", func(b *testing.B) {
		f := curried.Uncurry2(curried.Add2Val)
		s := 0
		for i := 0; i < b.N; i++ {
			s += curried.FoldFlat(f, 0, benchInput)
		}
		sinkInt = s
	})
	b.Run("curried-flat", func(b *testing.B) {
		f := func(a, x int) int { return a + x }
		s := 0
		for i := 0; i < b.N; i++ {
			s += curried.FoldFlat(f, 0, benchInput)
		}
		sinkInt = s
	})
	b.Run("arity", func(b *testing.B) {
		f := arity.Add2
		s := 0
		for i := 0; i < b.N; i++ {
			s += arity.Fold(f, 0, benchInput)
		}
		sinkInt = s
	})
}

// `map strconv.Itoa xs` — single-argument parameter, identical shape in
// both styles (sanity check; the allocations are Itoa's strings).
func BenchmarkMap(b *testing.B) {
	b.Run("curried", func(b *testing.B) {
		var out []string
		for i := 0; i < b.N; i++ {
			out = curried.Map(strconv.Itoa, benchInput)
		}
		sinkStrings = out
	})
	b.Run("arity", func(b *testing.B) {
		var out []string
		for i := 0; i < b.N; i++ {
			out = arity.Map(strconv.Itoa, benchInput)
		}
		sinkStrings = out
	})
}

// `zip_with f xs ys` over 1024 elements — per-element 2-argument callback.
func BenchmarkZipWith(b *testing.B) {
	b.Run("curried", func(b *testing.B) {
		f := curried.Add2Val
		var out []int
		for i := 0; i < b.N; i++ {
			out = curried.ZipWith(f, benchInput, benchInput)
		}
		sinkSlice = out
	})
	b.Run("arity", func(b *testing.B) {
		f := arity.Add2
		var out []int
		for i := 0; i < b.N; i++ {
			out = arity.ZipWith(f, benchInput, benchInput)
		}
		sinkSlice = out
	})
}

// `let f = transfer ~amount:10 in f i ~to:5` — labeled partial application
// with a reordering closure, applied to the remaining two arguments.
func BenchmarkLabeledPartialApply(b *testing.B) {
	b.Run("curried-opaque", func(b *testing.B) {
		f := unknownCLabel
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i)(5)
		}
		sinkInt = s
	})
	b.Run("arity-opaque", func(b *testing.B) {
		f := unknownALabel
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i, 5)
		}
		sinkInt = s
	})
	b.Run("curried-provable", func(b *testing.B) {
		f := curried.Transfer_Amount(10)
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i)(5)
		}
		sinkInt = s
	})
	b.Run("arity-provable", func(b *testing.B) {
		f := arity.Transfer_Amount(10)
		s := 0
		for i := 0; i < b.N; i++ {
			s += f(i, 5)
		}
		sinkInt = s
	})
}

// Sorting 1024 ints through slices.SortFunc — a Gom comparison value fed
// into a real Go API. The curried value must be uncurried first; each
// comparison then still runs the nested chain.
func BenchmarkSort(b *testing.B) {
	base := make([]int, 1024)
	for i := range base {
		base[i] = (i * 37) % 1024
	}
	buf := make([]int, len(base))

	b.Run("curried", func(b *testing.B) {
		cmp := curried.Uncurry2(curried.CmpDescVal)
		for i := 0; i < b.N; i++ {
			copy(buf, base)
			slices.SortFunc(buf, cmp)
		}
	})
	b.Run("arity", func(b *testing.B) {
		cmp := arity.CmpDesc
		for i := 0; i < b.N; i++ {
			copy(buf, base)
			slices.SortFunc(buf, cmp)
		}
	})
}
