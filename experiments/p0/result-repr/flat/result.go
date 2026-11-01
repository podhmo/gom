// Package flat is candidate B for the Result[T, E] representation: a
// single struct carrying the Ok payload, the Err payload, and a bool
// discriminant.
//
//	type Result[T, E any] struct {
//		Value T
//		Error E
//		Ok    bool
//	}
//
// The fields are exported deliberately: spec 8.7 wants generated
// packages to be usable from Go, and hiding them behind accessors
// would make the rep strictly worse than the type switch of candidate
// A. The downside of exported fields is that a Go caller can build a
// corrupt value ({Ok: true, Error: set} or the zero value, which reads
// as Err(zero E)); match on this rep has no invalid-value arm.
package flat

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/podhmo/gom/experiments/p0/result-repr/goside"
	"github.com/podhmo/gom/experiments/p0/result-repr/payload"
)

type Result[T, E any] struct {
	Value T
	Error E
	Ok    bool
}

// Gom constructors compile to either these helpers or the equivalent
// composite literals; both forms are shown because inference differs:
// Ok(v) cannot infer E and Err(e) cannot infer T, so call sites need
// explicit type arguments (generated code always knows them).
func Ok[T, E any](v T) Result[T, E]  { return Result[T, E]{Value: v, Ok: true} }
func Err[T, E any](e E) Result[T, E] { return Result[T, E]{Error: e} }

// ---- (T, error) -> Result boundary conversion ----

// ResultFromGo is the unfused helper form of the spec 8.3 mapping.
func ResultFromGo[T any](v T, err error) Result[T, error] {
	if err != nil {
		return Result[T, error]{Error: err}
	}
	return Result[T, error]{Value: v, Ok: true}
}

// ResultToGo unwraps a Result for a Go caller that wants (T, error).
// Note there is no invalid arm here: every bit pattern of the struct
// reads as either Ok or Err, including the zero value.
func ResultToGo[T any](r Result[T, error]) (T, error) {
	if r.Ok {
		return r.Value, nil
	}
	return r.Value, r.Error
}

// ---- match (decision tree) on a Result ----

// GetOrElse is `match r { Ok v -> v; Err _ -> fallback }`. With a bool
// discriminant the decision tree is a plain if/else — and there is no
// default arm to hang rt.InvalidVariant on, so a corrupt or zero value
// silently takes the Err branch.
func GetOrElse[T, E any](r Result[T, E], fallback T) T {
	if r.Ok {
		return r.Value
	}
	return fallback
}

// ---- try ----

// ParseSum hand-compiles the same `try` example as the sealed package.
// The fused form is identical in shape; only the Result construction
// at the boundary differs.
func ParseSum(a, b string) Result[int64, error] {
	x, err := strconv.Atoi(a)
	if err != nil {
		return Result[int64, error]{Error: err}
	}
	y, err := strconv.Atoi(b)
	if err != nil {
		return Result[int64, error]{Error: err}
	}
	return Result[int64, error]{Value: int64(x + y), Ok: true}
}

// ---- |?> ----

// checkPortRange: fn check_port_range (v: Int64) -> Result Int64 GoError
func checkPortRange(v int64) Result[int64, error] {
	if v <= 0 || v > 65535 {
		return Result[int64, error]{Error: fmt.Errorf("port out of range: %d", v)}
	}
	return Result[int64, error]{Value: v, Ok: true}
}

// ParsePort hand-compiles spec 9.2's example, following design.md 4.6.
// The Go-call step never materializes a Result; the Gom step's Result
// is inspected in place via r.Ok.
func ParsePort(s string) Result[int64, error] {
	t := strings.TrimSpace(s)
	v, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return Result[int64, error]{Error: fmt.Errorf("strconv.ParseInt: %w", err)}
	}
	r := checkPortRange(v)
	if !r.Ok {
		return Result[int64, error]{Error: fmt.Errorf("check_port_range: %w", r.Error)}
	}
	return r
}

// ParsePortNaive is the same chain without the design.md 4.6 elision:
// every step materializes a Result through ResultFromGo.
func ParsePortNaive(s string) Result[int64, error] {
	t := strings.TrimSpace(s)
	r := ResultFromGo(goside.ParseInt(t, 10, 64))
	if !r.Ok {
		return Result[int64, error]{Error: fmt.Errorf("strconv.ParseInt: %w", r.Error)}
	}
	r2 := checkPortRange(r.Value)
	if !r2.Ok {
		return Result[int64, error]{Error: fmt.Errorf("check_port_range: %w", r2.Error)}
	}
	return r2
}

// ---- large values ----

// LoadConfigBig hand-compiles a boundary call returning a ~1 KB value.
func LoadConfigBig(fail bool) Result[payload.Big, error] {
	b, err := goside.LoadBig(fail)
	if err != nil {
		return Result[payload.Big, error]{Error: err}
	}
	return Result[payload.Big, error]{Value: b, Ok: true}
}

// Bind is `r |?> f` as a free function.
func Bind[T, U, E any](r Result[T, E], f func(T) Result[U, E]) Result[U, E] {
	if r.Ok {
		return f(r.Value)
	}
	return Result[U, E]{Error: r.Error}
}
