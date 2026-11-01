package goerr

import (
	"errors"
	"testing"
)

// error -> GoErrorI and GoErrorI -> error are both implicit, because
// the method sets are identical.
func TestImplicitAssignability(t *testing.T) {
	var e error = errors.New("x")
	var g GoErrorI = e // error assigns into GoErrorI
	var back error = g // and back
	if back != e {
		t.Fatal("round trip changed the error")
	}
}

// The named interface costs nothing to call sites that take error:
// fmt.Errorf("%w", g), errors.Is(g, ...) all accept it implicitly.
func TestGoErrorIUsableAsError(t *testing.T) {
	var g GoErrorI = errors.New("inner")
	wrapped := wrap("op", g)
	if !errors.Is(wrapped, g) {
		t.Fatal("errors.Is failed through GoErrorI")
	}
}

func wrap(msg string, err error) error {
	return &wrapErr{msg: msg, err: err}
}

type wrapErr struct {
	msg string
	err error
}

func (w *wrapErr) Error() string { return w.msg + ": " + w.err.Error() }
func (w *wrapErr) Unwrap() error { return w.err }

// The alias form needs no conversion at all: GoErrorA IS error.
func TestAliasIsIdentical(t *testing.T) {
	var a GoErrorA = errors.New("x")
	var e error = a
	var a2 GoErrorA = e
	if a2 != a {
		t.Fatal("alias round trip changed the error")
	}
}
