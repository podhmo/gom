// Package sealed is candidate A for the Result[T, E] representation: a
// sealed interface with one value struct per variant, the design.md
// 4.1 encoding specialized to two variants.
//
//	type Result t e =
//	  | Ok t
//	  | Err e
//
// The marker method's signature mentions both type parameters on
// purpose (isResult() (T, E), not isResult()). Go type inference cannot
// bind a parameter that appears only in the instantiated interface's
// name: unification goes through the method set's structure, and a
// phantom parameter is not part of it. The P0-1 experiment showed the
// phantom form silently misinferring at call sites; mentioning T and E
// makes inference work and makes different instantiations genuinely
// different types.
package sealed

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/podhmo/gom/experiments/p0/result-repr/goside"
	"github.com/podhmo/gom/experiments/p0/result-repr/payload"
	"github.com/podhmo/gom/experiments/p0/result-repr/rt"
)

type Result[T, E any] interface{ isResult() (T, E) }

type Result_Ok[T, E any] struct{ V0 T }
type Result_Err[T, E any] struct{ V0 E }

func (r Result_Ok[T, E]) isResult() (T, E)  { var z E; return r.V0, z }
func (r Result_Err[T, E]) isResult() (T, E) { var z T; return z, r.V0 }

// Gom constructors compile to plain composite literals:
//
//	Ok v   -> Result_Ok[T, E]{V0: v}
//	Err e  -> Result_Err[T, E]{V0: e}
//
// Exported constructor functions exist only for Go consumers' comfort;
// generated code does not need them.
func Ok[T, E any](v T) Result[T, E]  { return Result_Ok[T, E]{V0: v} }
func Err[T, E any](e E) Result[T, E] { return Result_Err[T, E]{V0: e} }

// ---- (T, error) -> Result boundary conversion ----

// ResultFromGo is the unfused helper form of the spec 8.3 mapping
// "(T, error) -> Result T GoError": it always materializes a Result.
// Generated code normally does not call a helper; it inlines the two
// branches (see ParsePort). The helper exists to measure what the
// fused form saves.
func ResultFromGo[T any](v T, err error) Result[T, error] {
	if err != nil {
		return Result_Err[T, error]{V0: err}
	}
	return Result_Ok[T, error]{V0: v}
}

// ResultToGo unwraps a Result for a Go caller that wants the idiomatic
// (T, error) shape. This is the Gom -> Go direction of the boundary.
func ResultToGo[T any](r Result[T, error]) (T, error) {
	switch r := r.(type) {
	case Result_Ok[T, error]:
		return r.V0, nil
	case Result_Err[T, error]:
		var zero T
		return zero, r.V0
	default:
		panic(rt.InvalidVariant("sealed.Result", r))
	}
}

// ---- match (decision tree) on a Result ----

// GetOrElse is `match r { Ok v -> v; Err _ -> fallback }` compiled the
// way design.md 4.2 prescribes: a type switch whose default arm panics
// with rt.InvalidVariant for values Gom cannot produce (a nil Result).
func GetOrElse[T, E any](r Result[T, E], fallback T) T {
	switch r := r.(type) {
	case Result_Ok[T, E]:
		return r.V0
	case Result_Err[T, E]:
		return fallback
	default:
		panic(rt.InvalidVariant("sealed.Result", r))
	}
}

// ---- try ----

// ParseSum hand-compiles:
//
//	fn parse_sum (a: String) (b: String) -> Result Int GoError = {
//	  let x = try strconv.Atoi(a)
//	  let y = try strconv.Atoi(b)
//	  Ok (x + y)
//	}
//
// `try` on a Go call fuses with the (T, error) -> Result conversion:
// the err check IS the Err arm, so no Result is ever materialized on
// the success path.
func ParseSum(a, b string) Result[int64, error] {
	x, err := strconv.Atoi(a)
	if err != nil {
		return Result_Err[int64, error]{V0: err}
	}
	y, err := strconv.Atoi(b)
	if err != nil {
		return Result_Err[int64, error]{V0: err}
	}
	return Result_Ok[int64, error]{V0: int64(x + y)}
}

// ---- |?> ----

// checkPortRange is a Gom function that already returns a Result:
//
//	fn check_port_range (v: Int64) -> Result Int64 GoError
func checkPortRange(v int64) Result[int64, error] {
	if v <= 0 || v > 65535 {
		return Result_Err[int64, error]{V0: fmt.Errorf("port out of range: %d", v)}
	}
	return Result_Ok[int64, error]{V0: v}
}

// ParsePort hand-compiles spec 9.2's example, following design.md 4.6:
//
//	fn parse_port (s: String) -> Result Int64 GoError =
//	  Ok (strings.TrimSpace s)
//	  |?> strconv.ParseInt(_, 10, 64)
//	  |?> check_port_range
//
// Each step is an err check plus an early return. The Go-call step
// never materializes a Result; the Gom-step's Result (r) is inspected
// in place and re-wrapped only on the Err path.
func ParsePort(s string) Result[int64, error] {
	t := strings.TrimSpace(s)
	v, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return Result_Err[int64, error]{V0: fmt.Errorf("strconv.ParseInt: %w", err)}
	}
	r := checkPortRange(v)
	switch r := r.(type) {
	case Result_Ok[int64, error]:
		return r
	case Result_Err[int64, error]:
		return Result_Err[int64, error]{V0: fmt.Errorf("check_port_range: %w", r.V0)}
	default:
		panic(rt.InvalidVariant("sealed.Result", r))
	}
}

// ParsePortNaive is the same chain compiled WITHOUT the design.md 4.6
// elision: every step goes through ResultFromGo / a Result.bind-style
// helper, materializing a Result at each step. It exists to measure
// what the elision buys.
func ParsePortNaive(s string) Result[int64, error] {
	t := strings.TrimSpace(s)
	r := ResultFromGo(goside.ParseInt(t, 10, 64))
	switch r := r.(type) {
	case Result_Err[int64, error]:
		return Result_Err[int64, error]{V0: fmt.Errorf("strconv.ParseInt: %w", r.V0)}
	case Result_Ok[int64, error]:
		r2 := checkPortRange(r.V0)
		switch r2 := r2.(type) {
		case Result_Err[int64, error]:
			return Result_Err[int64, error]{V0: fmt.Errorf("check_port_range: %w", r2.V0)}
		case Result_Ok[int64, error]:
			return r2
		default:
			panic(rt.InvalidVariant("sealed.Result", r2))
		}
	default:
		panic(rt.InvalidVariant("sealed.Result", r))
	}
}

// ---- large values ----

// LoadConfigBig hand-compiles a boundary call returning a ~1 KB value:
//
//	fn load_big () -> Result Big GoError =
//	  try goside.LoadBig(false) |> fn b -> Ok b   (shape only)
func LoadConfigBig(fail bool) Result[payload.Big, error] {
	b, err := goside.LoadBig(fail)
	if err != nil {
		return Result_Err[payload.Big, error]{V0: err}
	}
	return Result_Ok[payload.Big, error]{V0: b}
}

// Bind is `r |?> f` as a free function (what a non-inlined, non-elided
// |?> amounts to when f is opaque):
//
//	fn bind (r: Result a e) (f: a -> Result b e) -> Result b e
func Bind[T, U, E any](r Result[T, E], f func(T) Result[U, E]) Result[U, E] {
	switch r := r.(type) {
	case Result_Ok[T, E]:
		return f(r.V0)
	case Result_Err[T, E]:
		return Result_Err[U, E]{V0: r.V0}
	default:
		panic(rt.InvalidVariant("sealed.Result", r))
	}
}
