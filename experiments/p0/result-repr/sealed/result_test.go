package sealed

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"testing"
)

func panicValue(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

// GetOrElse must infer both T and E from the Result argument alone.
// This is what the isResult() (T, E) marker signature buys; with a
// phantom marker (isResult()) the call below cannot infer E.
func TestInference(t *testing.T) {
	var r Result[int64, error] = Result_Ok[int64, error]{V0: 7}
	if got := GetOrElse(r, 9); got != 7 {
		t.Fatalf("GetOrElse = %d, want 7", got)
	}
	var r2 Result[int64, error] = Result_Err[int64, error]{V0: errors.New("x")}
	if got := GetOrElse(r2, 9); got != 9 {
		t.Fatalf("GetOrElse err = %d, want 9", got)
	}
}

// The zero value of the rep is a nil interface: an invalid variant.
// A generated match must panic with rt.InvalidVariant, not run an arm.
func TestZeroValueIsInvalid(t *testing.T) {
	var r Result[int64, error] // nil interface
	v := panicValue(func() { GetOrElse(r, 9) })
	if v == nil {
		t.Fatal("expected panic on nil Result")
	}
	err, ok := v.(error)
	if !ok || err == nil {
		t.Fatalf("panic value %#v is not an error", v)
	}
	t.Logf("panic: %v", err)
}

// A typed-nil *variant* pointer does not match the value-variant cases
// either; it lands in the same InvalidVariant default arm.
func TestTypedNilIsInvalid(t *testing.T) {
	var r Result[int64, error] = (*Result_Ok[int64, error])(nil)
	v := panicValue(func() { GetOrElse(r, 9) })
	if v == nil {
		t.Fatal("expected panic on typed-nil Result")
	}
	t.Logf("panic: %v", v)
}

// If codegen emitted a bare comma-ok assertion (`if e, failed :=
// r.(Result_Err); failed`) instead of a switch with a default arm, a
// nil Result would fall through to the Ok path and panic inside the
// arm — worse message, wrong contract. The switch form routes it to
// InvalidVariant; this test pins the switch behaviour.
func TestNaiveAssertMisroutesNil(t *testing.T) {
	var r Result[int64, error]
	failed := false
	if _, isErr := r.(Result_Err[int64, error]); isErr {
		failed = true
	}
	if failed {
		t.Fatal("nil asserted as Err")
	}
	// falls through to the "Ok" path: extracting V0 panics here
	v := panicValue(func() { _ = r.(Result_Ok[int64, error]).V0 })
	if v == nil {
		t.Fatal("expected panic extracting from nil Result")
	}
	t.Logf("naive extraction panic: %v", v)
}

func TestFromGoAndBack(t *testing.T) {
	r := ResultFromGo(int64(5), nil)
	v, err := ResultToGo(r)
	if err != nil || v != 5 {
		t.Fatalf("round trip ok: v=%d err=%v", v, err)
	}
	ein := errors.New("boom")
	r = ResultFromGo(int64(0), ein)
	v, err = ResultToGo(r)
	if v != 0 || !errors.Is(err, ein) {
		t.Fatalf("round trip err: v=%d err=%v", v, err)
	}
}

// The |?> auto-wrap path: the failed step wraps with %w exactly once,
// and errors.Is / errors.As see through the wrap.
func TestPipeWrap(t *testing.T) {
	r := ParsePort(" 8080 ")
	v, err := ResultToGo(r)
	if err != nil || v != 8080 {
		t.Fatalf("ParsePort ok: v=%d err=%v", v, err)
	}

	r = ParsePort("notanum")
	if _, ok := r.(Result_Err[int64, error]); !ok {
		t.Fatalf("ParsePort notanum: got %#v", r)
	}
	_, err = ResultToGo(r)
	if err == nil || err.Error() == "" {
		t.Fatal("expected wrapped error")
	}
	t.Logf("wrapped: %v", err)
	// strconv.ParseInt's *strconv.NumError must remain reachable
	// through the |?> %w wrap.
	var numErr *strconv.NumError
	if !errors.As(err, &numErr) {
		t.Fatal("errors.As lost *strconv.NumError through the wrap")
	}

	r = ParsePort("999999")
	_, err = ResultToGo(r)
	if err == nil {
		t.Fatal("expected range error")
	}
	t.Logf("wrapped range: %v", err)
}

// A |?> error wrapped with %w keeps errors.Is / errors.As working on
// the original error — this is the spec 9.2 contract.
func TestWrapPreservesErrorsIs(t *testing.T) {
	sentinel := fs.ErrNotExist
	wrapped := fmt.Errorf("load_config: %w", sentinel)
	if !errors.Is(wrapped, fs.ErrNotExist) {
		t.Fatal("errors.Is lost the sentinel")
	}
	var pathErr *fs.PathError
	if errors.As(wrapped, &pathErr) {
		t.Fatal("unexpected As match")
	}
}

// fmt.Errorf("%w", nil) does NOT produce nil: the flat rep's zero
// value, which reads as Err(nil), would produce this garbage string if
// it ever flowed through the |?> wrap path.
func TestWrapNil(t *testing.T) {
	e := fmt.Errorf("op: %w", error(nil))
	if e == nil {
		t.Fatal("fmt.Errorf %w nil returned nil")
	}
	t.Logf("%%w of nil renders as: %q (errors.Is(e, nil)=%v)", e.Error(), errors.Is(e, nil))
}

// Structural == is free: two separately built Ok values compare equal
// because interface equality deep-compares the boxed structs. Error
// payloads compare by whatever Go's interface equality gives — for the
// usual pointer-shaped errors, identity.
func TestEquality(t *testing.T) {
	a := Result[int64, error](Result_Ok[int64, error]{V0: 1})
	b := Result[int64, error](Result_Ok[int64, error]{V0: 1})
	if a != b {
		t.Fatal("Ok 1 != Ok 1")
	}
	e1 := errors.New("x")
	e2 := errors.New("x")
	r1 := Result[int64, error](Result_Err[int64, error]{V0: e1})
	r2 := Result[int64, error](Result_Err[int64, error]{V0: e2})
	if r1 == r2 {
		t.Fatal("distinct error values compared equal")
	}
	r3 := Result[int64, error](Result_Err[int64, error]{V0: e1})
	if r1 != r3 {
		t.Fatal("same error value compared unequal")
	}
}
