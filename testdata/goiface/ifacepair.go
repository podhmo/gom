package goiface

// `(I, error)`-family helpers exercising the nil-able
// interface-payload boundary (spec sections 8.3/8.4): pairs whose
// first result is a method-bearing interface — `net.Error` and a
// user interface — derive `Result (Option T) GoError`, while
// `(any, error)` and a named empty interface keep the verbatim
// `Result T GoError` view. Lone interface results admit the
// declared `Option T` view through externs.

import (
	"errors"
	"net"
)

// NetError aliases `net.Error` so an extern block can spell the
// named interface in a declared view (`Result (Option NetError)
// GoError`, `Result NetError GoError`).
type NetError = net.Error

// timeoutError is a concrete `net.Error` — `Timeout()` lets the
// Gom side observe the payload is the real interface value.
type timeoutError struct{ msg string }

func (e timeoutError) Error() string   { return e.msg }
func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Temporary() bool { return false }

// NetErr is the `(net.Error, error)` shape — a method-bearing
// interface payload whose nil the derived `Result (Option
// net.Error) GoError` view reads as `Ok None`.
func NetErr(n int64) (net.Error, error) {
	switch {
	case n < 0:
		return nil, errors.New("fatal")
	case n == 0:
		return nil, nil
	default:
		return timeoutError{msg: "timed out"}, nil
	}
}

// NetErrNil always returns `(nil, nil)` — the return a declared
// `Result net.Error GoError` view claims never happens, so the
// generated boundary check fires.
func NetErrNil() (net.Error, error) { return nil, nil }

// Tagger is a user-defined method-bearing interface — the same
// derived-Option family as `net.Error`, through a member of this
// package rather than the standard library.
type Tagger interface{ Tag() string }

type tag string

func (t tag) Tag() string { return string(t) }

// TagOr is `(Tagger, error)` — the user-interface analogue of
// NetErr; nil payloads read `Ok None` under the derived view.
func TagOr(n int64) (Tagger, error) {
	switch {
	case n < 0:
		return nil, errors.New("fatal")
	case n == 0:
		return nil, nil
	default:
		return tag("it"), nil
	}
}

// EmptyOr is `(Empty, error)` — `Empty` is fixture.go's named
// *empty* interface; its pair keeps the verbatim `Result Empty
// GoError` derived view (the nil is inert: no method can panic on
// it). Nil and value payloads both flow through `Ok` verbatim.
func EmptyOr(n int64) (Empty, error) {
	switch {
	case n < 0:
		return nil, errors.New("fatal")
	case n == 0:
		return nil, nil
	default:
		return n, nil
	}
}

// AnyOr is `(any, error)` — like EmptyOr, nil payload values are
// legitimate data under the verbatim `Result Go.Any GoError`
// derived view.
func AnyOr(n int64) (any, error) {
	switch {
	case n < 0:
		return nil, errors.New("fatal")
	case n == 0:
		return nil, nil
	default:
		return n, nil
	}
}

// LoneTagger returns the user interface `Tagger` — a lone
// method-bearing interface result externs may declare as
// `Option Tagger` (the `Option io.Reader` shape in spec 8.4's
// wording).
func LoneTagger(filled bool) Tagger {
	if !filled {
		return nil
	}
	return tag("lone")
}

// LoneEmpty returns fixture.go's named empty interface — a lone
// interface result externs may declare as `Option Empty`; nil
// reads as `None` through `v != nil`.
func LoneEmpty(filled bool) Empty {
	if !filled {
		return nil
	}
	return int64(7)
}
