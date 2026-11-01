package rt

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/podhmo/gom/stdlib/option"
	"github.com/podhmo/gom/stdlib/result"
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

// ResultFromGo materializes a Result T GoError from a (T, error) Go
// call that is stored or returned rather than escaped (the naive
// boundary conversion — try / |?> fuse the check instead).
func ResultFromGo[T any](v T, err error) result.Result[T, error] {
	if err != nil {
		return result.Err[T, error](err)
	}
	return result.Ok[T, error](v)
}

// ResultFromErr materializes a Result Unit GoError from an error-only
// Go call.
func ResultFromErr(err error) result.Result[struct{}, error] {
	if err != nil {
		return result.Err[struct{}, error](err)
	}
	return result.Ok[struct{}, error](struct{}{})
}

// OptionFromGo materializes an Option T from the value and ok flag of
// a comma-ok Go call — or from a channel receive in a select arm.
func OptionFromGo[T any](v T, ok bool) option.Option[T] {
	if ok {
		return option.Option_Some[T]{V0: v}
	}
	return option.Option_None[T]{}
}

// Assert is the body of the `assert cond` builtin used in test
// blocks.
func Assert(t *testing.T, cond bool) {
	t.Helper()
	if !cond {
		t.Error("gom: assertion failed")
	}
}

// AssertEq is the body of the `assert_eq actual expected` builtin used
// in test blocks. Gom's `==` is structural equality, which covers
// lists and tuples that Go's comparable constraint cannot, so the
// comparison uses reflect.DeepEqual until compiler-generated per-type
// compare functions exist (docs/design.md section 4.8).
func AssertEq(t *testing.T, actual, expected any) {
	t.Helper()
	if !reflect.DeepEqual(actual, expected) {
		t.Errorf("gom: assertion failed:\n  actual:   %#v\n  expected: %#v", actual, expected)
	}
}

// AssertOk checks that a test block's Result-typed tail is Ok. A test
// `test "name" { ... }` whose body evaluates to Err fails the test.
func AssertOk[T, E any](t *testing.T, r result.Result[T, E]) {
	t.Helper()
	switch r.Tag() {
	case result.ResultTagOk:
	case result.ResultTagErr:
		t.Errorf("gom: test returned Err: %#v", r.Error)
	default:
		panic(InvalidVariant("result.Result", r))
	}
}
