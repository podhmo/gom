package rt

import (
	"fmt"
	"reflect"
	"testing"
)

// InvalidVariant returns the error used when a value of a Gom sum type
// does not hold any of its declared variants. Generated match
// expressions panic with this error rather than panicking with
// "unreachable", because Go callers can produce values that Gom code
// cannot: a nil interface, a zero struct, or a struct with a corrupted
// tag (docs/spec.md section 8.6).
func InvalidVariant(typeName string, v any) error {
	return fmt.Errorf("gom: invalid %s value %#v: not a declared variant", typeName, v)
}

// Todo is the body of the `todo` builtin: a hole that panics when
// reached. The type parameter lets it stand at any result type.
func Todo[T any]() T {
	panic("gom: todo")
}

// BoxSlice copies a Go slice into a fresh []any, boxing each element —
// the boundary conversion a `..xs` spread into a `...any` variadic
// needs (spec section 8.1 item 4): Go itself cannot splice a []T
// into ...any. The fresh array means the Go callee's writes through
// the slice never reach the operand's backing array, per spec
// section 8.5's copy rule.
func BoxSlice[A any](xs []A) []any {
	out := make([]any, len(xs))
	for i, v := range xs {
		out[i] = v
	}
	return out
}

// Equal is the fallback recipe for Gom's structural `==` (spec
// section 3.3): generated code routes operand types it has no
// per-type comparator for — named Go types, Task, Go.Any — through
// here. Lists, tuples, records and ADTs use compiler-generated
// per-type comparators instead (docs/design.md section 4.9), which
// is what removes reflect.DeepEqual's identical-backing-store
// shortcut from user-visible equality. Equal therefore only has to
// answer for values whose Gom-level recipe does not exist, and its
// reflect.DeepEqual reading is the documented approximation for
// those — including DeepEqual's nil-vs-empty and pointer-shortcut
// quirks, which can still leak through Go-view types.
func Equal(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	// A nil slice is the empty list per the representation contract
	// (stdlib/list: "the zero value (nil) is the empty list"), and
	// eval's element-wise comparison agrees — but DeepEqual
	// distinguishes nil from non-nil-but-empty, so give the top
	// level a second answer for the Go-view slices that still route
	// here. Nested nil-vs-empty is generated code's job: the
	// element recipe inside a composite comparator is never Equal.
	return bothEmptySlices(a, b)
}

// bothEmptySlices reports whether a and b are two empty slices of
// the same type — the nil-vs-empty reading Equal adds on top of
// reflect.DeepEqual.
func bothEmptySlices(a, b any) bool {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	if !av.IsValid() || !bv.IsValid() || av.Type() != bv.Type() {
		return false
	}
	return av.Kind() == reflect.Slice && av.Len() == 0 && bv.Len() == 0
}

// Assert is the body of the `assert cond` builtin used in test
// blocks.
func Assert(t *testing.T, cond bool) {
	t.Helper()
	if !cond {
		t.Error("gom: assertion failed")
	}
}

// AssertEq is the body of the `assert_eq actual expected` builtin
// used in test blocks. The eq argument is the already-computed
// verdict under the operands' own comparison recipe — the same
// plan `actual == expected` would evaluate (docs/design.md section
// 4.9) — so a failing assert_eq reports exactly what `==` would
// answer, including native comparisons' IEEE NaN verdict. actual and
// expected arrive already rendered to text by the compiler-generated
// display functions of docs/design.md section 4.8 (fmt's %v for
// Go-view values, spec section 11).
func AssertEq(t *testing.T, eq bool, actual, expected string) {
	t.Helper()
	if !eq {
		t.Errorf("gom: assertion failed:\n  actual:   %s\n  expected: %s", actual, expected)
	}
}

// ShowError renders a GoError value inside a generated display
// function — `error("text")`, the evaluator's VGoErr reading, with a
// nil guard for the Go-side zero value.
func ShowError(err error) string {
	if err == nil {
		return "nil"
	}
	return fmt.Sprintf("error(%q)", err.Error())
}
