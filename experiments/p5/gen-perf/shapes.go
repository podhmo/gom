// Package genperf mirrors generated-code shapes that have no committed
// artifact to benchmark (P5-3). Each function names the golden file it
// mirrors; when codegen changes the shape, update the mirror by hand —
// it exists to put real numbers and `go build -gcflags=-m` output on
// the allocation/escape behavior of emitted wrappers.
//
// Run:
//
//	go test -bench=. -benchmem
//	go build -gcflags="-m -m" ./...
package genperf

import (
	"fmt"
	"strconv"

	"github.com/podhmo/gom/rt"
	"github.com/podhmo/gom/stdlib/result"
)

// --- curried alias wrappers ---------------------------------------------
//
// Mirror of testdata/golden/static_function_alias.golden.go:
//
//   fn add (x: Int) (y: Int) -> Int = x + y
//   let plus = add          // arity-2 value reference → curried adapter
//   let again = count       // arity-1 alias → named wrapper

func add(x, y int64) int64 { return x + y }

// plus mirrors the generated adapter: a nested arity-one eta expansion
// whose last arrow fires the whole declared chunk.
func plus(tmp6 int64) func(int64) int64 {
	tmp9 := func(tmp7 int64) func(int64) int64 {
		return func(tmp8 int64) int64 {
			return add(tmp7, tmp8)
		}
	}
	return tmp9(tmp6)
}

func count(n int64) int64 {
	if n == 0 {
		return 0
	}
	return again(n-1) + 1
}

// again mirrors the arity-1 named wrapper P2-2 emits for `let g = f`:
// it calls the element's root function directly.
func again(tmp5 int64) int64 { return count(tmp5) }

// --- |?> segment closure (IIFE) ------------------------------------------
//
// Mirror of testdata/golden/result_pipe.golden.go's `parse`: the whole
// pipe chain becomes an immediately-invoked Unit-parameter closure
// capturing the in-scope bindings.

func keep(n int64) result.Result[int64, error] { return result.Ok[int64, error](n) }

func parse(s string) result.Result[int64, error] {
	tmp6 := func(tmp1 struct{}) result.Result[int64, error] {
		tmp2, err := strconv.Atoi(s)
		if err != nil {
			return result.Err[int64, error](fmt.Errorf("strconv.Atoi: %w", err))
		}
		tmp3 := int64(tmp2)
		tmp4 := keep(tmp3)
		var tmp5 int64
		switch tmp4.Tag() {
		case result.ResultTagOk:
			tmp5 = tmp4.Value
		case result.ResultTagErr:
			return result.Err[int64, error](fmt.Errorf("keep: %w", tmp4.Error))
		default:
			panic(rt.InvalidVariant("result.Result", tmp4))
		}
		return result.Ok[int64, error](tmp5)
	}
	return tmp6(struct{}{})
}

// Export a tiny surface so the shapes stay linked and escapable
// exactly as generated.

var (
	Plus  = plus
	Again = again
	Parse = parse
	Add   = add
	Count = count
)
