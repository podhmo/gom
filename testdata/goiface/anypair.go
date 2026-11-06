package goiface

import "errors"

// `(any, ...)`-family helpers exercising the `Go.Any` element of an
// extern's composite result views (spec sections 8.3/8.4): `any` is
// an interface — nil-able — so `Option Go.Any` over a comma-ok pair
// and `Result (Option Go.Any) GoError` over an `(any, error)` pair
// are admitted declarations, and a nil payload materializes as
// `None` at the boundary.

// AnyLookup is the `(any, bool)` comma-ok shape — the declared
// `Option Go.Any` view reads `ok == false` as `None`.
func AnyLookup(k string) (any, bool) {
	if k == "" {
		return nil, false
	}
	return k, true
}

// AnyPair is the `(any, error)` shape — the declared `Result
// (Option Go.Any) GoError` view reads a nil payload as `Ok None`.
func AnyPair(n int64) (any, error) {
	switch {
	case n < 0:
		return nil, errors.New("negative")
	case n == 0:
		return nil, nil
	default:
		return n, nil
	}
}

// TakesAnyFn is the `(func(any) int64) -> int64` shape — the
// extern view declares the parameter `Go.Any -> Int`; a Gom
// `String -> Int` value must not slip in as `func(any) int64`
// (Go func types are invariant, same rule as the type's own
// parameters).
func TakesAnyFn(f func(any) int64) int64 { return f("x") }
