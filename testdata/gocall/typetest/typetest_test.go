package typetest

import (
	"errors"
	"testing"
)

// Runtime check for `p : T` type-test arms (spec 4.1): the emitted
// `switch v := x.(type)` classifies Go-produced `any` values —
// concrete arms bind the narrowed value, `error` matches by the
// error contract, named interfaces match by method set, and nil
// falls through to the `_` catch-all.
type label struct{ s string }

func (l label) String() string { return l.s }

func TestTypeTestArms(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string arm binds narrowed value", "hello", "hello"},
		{"int arm", int64(7), "int"},
		{"error arm", errors.New("boom"), "error"},
		{"named interface arm by method set", label{s: "x"}, "stringer"},
		{"nil falls to catch-all", nil, "other"},
		{"unmatched type falls to catch-all", 2.5, "other"},
	}
	for _, c := range cases {
		if got := Describe(c.in); got != c.want {
			t.Fatalf("%s: Describe(%v) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestTypeTestGomRoundTrip(t *testing.T) {
	// n : Int boxes to any(int64(n)) at the goiface.Boxed boundary,
	// then the type switch classifies it back to the Int arm.
	if got := BoxedDescribe(9); got != "int" {
		t.Fatalf("BoxedDescribe(9) = %q, want \"int\"", got)
	}
}
