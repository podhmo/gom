package option_test

// Boundary tests for the hand-written Option representation
// (stdlib/option/types.go): the comma-ok conversion generated code
// uses at the Go boundary, and the nil-Option pin — a nil interface
// is not None, it is the invalid zero value every match's default
// arm panics on.

import (
	"testing"

	"github.com/podhmo/gom/stdlib/option"
)

func TestFromGo(t *testing.T) {
	some := option.FromGo(7, true)
	sv, ok := some.(option.Option_Some[int])
	if !ok || sv.V0 != 7 {
		t.Fatalf("FromGo(7, true) = %#v, want Option_Some{V0: 7}", some)
	}
	none := option.FromGo(7, false)
	if _, ok := none.(option.Option_None[int]); !ok {
		t.Fatalf("FromGo(7, false) = %#v, want Option_None", none)
	}
}

// TestNilOptionPanics pins the zero-value contract: `var o
// Option[int]` is not None — the interface is nil, matches hit the
// invalid-variant default arm of generated code, and a helper like
// Get or IsSome panics instead of answering.
func TestNilOptionPanics(t *testing.T) {
	var o option.Option[int]
	if o != nil {
		t.Fatal("zero Option should be nil")
	}
	assertPanics(t, "Get on nil Option", func() { option.Get(o) })
	assertPanics(t, "IsSome on nil Option", func() { option.IsSome(o) })
	// None is the real empty variant and must never share nil's fate.
	assertNotPanics(t, "IsSome on None", func() {
		if option.IsSome(option.Option_None[int]{}) {
			t.Error("IsSome(None) = true, want false")
		}
	})
}

func assertPanics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", name)
		}
	}()
	f()
}

func assertNotPanics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v", name, r)
		}
	}()
	f()
}
