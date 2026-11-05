package goiface

// `(error, error)`-family helpers exercising the Ok-payload nil
// boundary (spec sections 8.3/8.4): a `(error, error)` pair whose
// first result is legitimately nil-able, a `(*T, error)` pair for
// the declared `Result (Option (Ptr T)) GoError` view, and a
// `(nil, nil)` producer for the declared never-nil claim.

import "errors"

// TwoErrs is the `(error, error)` shape: a nil-able "payload"
// error in slot 0 — `(nil, nil)` is legitimate (no partial error,
// no fatal error) and reads as `Ok None` under the derived
// `Result (Option GoError) GoError` view.
func TwoErrs(n int64) (error, error) {
	switch {
	case n < 0:
		return nil, errors.New("fatal")
	case n == 0:
		return nil, nil
	default:
		return errors.New("soft"), nil
	}
}

// SoftPtr is a `(*T, error)` pair — the declared `Result (Option
// (Ptr T)) GoError` view reads a nil payload as `Ok None`.
func SoftPtr(n int64) (*int64, error) {
	switch {
	case n < 0:
		return nil, errors.New("negative")
	case n == 0:
		return nil, nil
	default:
		return &n, nil
	}
}

// BothNil is the `(error, error)` producer a declared `Result
// GoError GoError` view claims never happens — it returns
// `(nil, nil)` so the generated boundary check fires.
func BothNil() (error, error) { return nil, nil }

// StrictPair is BothNil as a unary function — the `|?>` step's
// fused EscapeCall exercises the same never-nil payload check.
func StrictPair(n int64) (error, error) { return nil, nil }
