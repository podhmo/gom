package goiface

// N-ary (3+) result helpers exercising the wave20 N-ary folding
// (docs/decisions/2026-11-18-nary-go-results.md): plain N-tuples,
// a `(T, U, error)` triple that folds to `Result (Tuple) GoError`,
// a method-bearing interface element inside the Result payload,
// and a 9-result function at the representability cap.

import (
	"errors"
	"fmt"
	"net"
)

// Triple is the plain `(T, U, V)` shape — an N-ary tuple with no
// trailing presence channel.
func Triple(n int64) (int64, string, bool) {
	return n, fmt.Sprintf("%d", n), n > 0
}

// DivMod is the `(T, U, error)` shape — the Ok payload folds to a
// tuple under `Result (Int, Int) GoError`.
func DivMod(a, b int64) (int64, int64, error) {
	if b == 0 {
		return 0, 0, errors.New("division by zero")
	}
	return a / b, a % b, nil
}

// NetErrTriple is `(net.Error, int64, error)` — a method-bearing
// interface element inside the Result-payload tuple. The derived
// view Option-wraps it: `Result (Option NetError, Int) GoError`.
func NetErrTriple(n int64) (net.Error, int64, error) {
	if n < 0 {
		return nil, n, errors.New("negative")
	}
	return timeoutError{msg: "slow"}, n, nil
}

// NetErrTripleNil always returns `(nil, 0, nil)` — under the
// Option-wrapped element the nil reads as `None`.
func NetErrTripleNil() (net.Error, int64, error) { return nil, 0, nil }

// Eight is the cap boundary — 8 results stay representable.
func Eight(n int64) (int64, int64, int64, int64, int64, int64, int64, int64) {
	return n, n, n, n, n, n, n, n
}

// Nine exceeds the representability cap (8 results) and hides.
func Nine(n int64) (int64, int64, int64, int64, int64, int64, int64, int64, int64) {
	return n, n, n, n, n, n, n, n, n
}

// DivModFn returns the `(int64, int64) -> (int64, int64, error)`
// func value — `Go.fnN` over it reads back
// `Int -> Int -> Result (Int, Int) GoError`.
func DivModFn() func(int64, int64) (int64, int64, error) { return DivMod }

// TripleFn returns the N-ary func value — `Go.fnN` reads
// `Int -> (Int, String, Bool)`.
func TripleFn() func(int64) (int64, string, bool) { return Triple }
