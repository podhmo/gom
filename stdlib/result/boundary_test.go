package result_test

// Boundary tests for the hand-written Result representation
// (stdlib/result/types.go): the Go-side helpers generated and
// user code share at the (T, error) boundary. They pin the contract
// docs/decisions/2026-11-01-value-representation.md declares —
// only Ok/Err are real variants, the zero Result is invalid and
// panics instead of silently succeeding.

import (
	"errors"
	"testing"

	"github.com/podhmo/gom/stdlib/result"
)

func TestFromGo(t *testing.T) {
	r := result.FromGo(42, nil)
	if !r.IsOk() || r.Tag() != result.ResultTagOk {
		t.Fatalf("FromGo(42, nil) tag = %v, want Ok", r.Tag())
	}
	if r.Value != 42 {
		t.Fatalf("FromGo(42, nil).Value = %v, want 42", r.Value)
	}

	boom := errors.New("boom")
	r = result.FromGo(42, boom)
	if r.IsOk() || r.Tag() != result.ResultTagErr {
		t.Fatalf("FromGo(42, boom) tag = %v, want Err", r.Tag())
	}
	if !errors.Is(r.Error, boom) {
		t.Fatalf("FromGo error = %v, want boom", r.Error)
	}
}

func TestFromErr(t *testing.T) {
	if !result.FromErr(nil).IsOk() {
		t.Fatal("FromErr(nil) should be Ok")
	}
	boom := errors.New("boom")
	r := result.FromErr(boom)
	if r.IsOk() || !errors.Is(r.Error, boom) {
		t.Fatalf("FromErr(boom) = %#v, want Err(boom)", r)
	}
}

func TestUnwrap(t *testing.T) {
	v, err := result.Ok[int, error](7).Unwrap()
	if v != 7 || err != nil {
		t.Fatalf("Ok(7).Unwrap() = (%v, %v), want (7, nil)", v, err)
	}
	boom := errors.New("boom")
	v, err = result.Err[int, error](boom).Unwrap()
	if v != 0 || !errors.Is(err, boom) {
		t.Fatalf("Err(boom).Unwrap() = (%v, %v), want (0, boom)", v, err)
	}
}

func TestToGo(t *testing.T) {
	v, err := result.ToGo(result.Ok[int, error](7))
	if v != 7 || err != nil {
		t.Fatalf("ToGo(Ok(7)) = (%v, %v), want (7, nil)", v, err)
	}
	boom := errors.New("boom")
	v, err = result.ToGo(result.Err[int, error](boom))
	if v != 0 || !errors.Is(err, boom) {
		t.Fatalf("ToGo(Err(boom)) = (%v, %v), want (0, boom)", v, err)
	}
}

// TestZeroResultIsInvalid pins the flat2 contract: the zero Result
// is not a variant — helpers panic instead of inventing a success
// out of uninitialized state.
func TestZeroResultIsInvalid(t *testing.T) {
	var zero result.Result[int, error]
	if zero.Tag() != result.ResultTagInvalid {
		t.Fatalf("zero Tag = %v, want ResultTagInvalid", zero.Tag())
	}
	if zero.IsOk() {
		t.Fatal("zero Result claims Ok")
	}
	assertPanics(t, "Unwrap on zero Result", func() { _, _ = zero.Unwrap() })
	assertPanics(t, "ToGo on zero Result", func() { _, _ = result.ToGo(zero) })
	assertPanics(t, "AssertOk on zero Result", func() { result.AssertOk(t, zero) })
}

// TestAssertOk exercises the happy path generated test blocks rely
// on. The Err path calls t.Errorf, which a real *testing.T cannot
// exercise without failing this very test — the generated suite
// covers it implicitly whenever a test returns Err.
func TestAssertOk(t *testing.T) {
	result.AssertOk(t, result.Ok[int, error](1))
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
