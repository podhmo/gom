package flat

import (
	"errors"
	"fmt"
	"strconv"
	"testing"
)

func TestInference(t *testing.T) {
	var r Result[int64, error] = Ok[int64, error](7)
	if got := GetOrElse(r, 9); got != 7 {
		t.Fatalf("GetOrElse = %d, want 7", got)
	}
	if got := GetOrElse(Err[int64](errors.New("x")), 9); got != 9 {
		t.Fatalf("GetOrElse err = %d, want 9", got)
	}
}

// Constructor inference wart: Ok(v) cannot infer E and Err(e) cannot
// infer T — both need explicit type arguments. Generated code knows
// the types so this only inconveniences hand-writing Go callers.
func TestConstructorInference(t *testing.T) {
	var r Result[int64, error] = Ok[int64, error](3)         // Ok(3) alone cannot infer E
	var s Result[int64, error] = Err[int64](fmt.Errorf("x")) // Err(e) cannot infer T
	_, _ = r, s
}

// The zero value of the rep is {Value: zero, Error: zero, Ok: false} —
// it reads as Err(zero E). There is no invalid state for match to
// reject, so a Go caller's forgotten `var r Result` silently flows
// through every check as a failure carrying a nil error.
func TestZeroValueReadsAsErr(t *testing.T) {
	var r Result[int64, error]
	if r.Ok {
		t.Fatal("zero value claims Ok")
	}
	if r.Error != nil {
		t.Fatalf("zero value Error = %v, want nil", r.Error)
	}
	if got := GetOrElse(r, 9); got != 9 {
		t.Fatalf("zero value took Ok arm, got %d", got)
	}
	// The degenerate Err(nil) survives a round trip without complaint.
	v, err := ResultToGo(r)
	if v != 0 || err != nil {
		t.Fatalf("ResultToGo(zero) = (%d, %v)", v, err)
	}
}

// If an Err(nil) — constructible via the zero value or explicitly —
// reaches the |?> auto-wrap path, fmt.Errorf produces the infamous
// "%!w(<nil>)" string AND returns a non-nil error.
func TestErrNilThroughWrap(t *testing.T) {
	r := Err[int64](error(nil))
	wrapped := fmt.Errorf("load_config: %w", r.Error)
	if wrapped == nil {
		t.Fatal("wrap of nil error is nil")
	}
	t.Logf("Err(nil) wrapped: %q", wrapped.Error())
	if errors.Is(wrapped, nil) {
		t.Fatal("wrapped nil Is(nil)")
	}
}

// Exported fields let Go callers build values Gom cannot: Ok with an
// Error attached, or Err carrying a Value. Match cannot see these.
func TestCorruptValuesAreUnrejectable(t *testing.T) {
	corrupt := Result[int64, error]{Value: 1, Ok: true, Error: errors.New("stray")}
	if got := GetOrElse(corrupt, 9); got != 1 {
		t.Fatalf("corrupt value: got %d", got)
	}
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

func TestPipeWrap(t *testing.T) {
	r := ParsePort(" 8080 ")
	v, err := ResultToGo(r)
	if err != nil || v != 8080 {
		t.Fatalf("ParsePort ok: v=%d err=%v", v, err)
	}

	r = ParsePort("notanum")
	if r.Ok {
		t.Fatalf("ParsePort notanum: got %#v", r)
	}
	_, err = ResultToGo(r)
	if err == nil {
		t.Fatal("expected wrapped error")
	}
	t.Logf("wrapped: %v", err)
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

// == compares all three fields; error payloads get interface equality
// (identity for pointer-shaped errors), same as the sealed rep.
func TestEquality(t *testing.T) {
	a := Ok[int64, error](1)
	b := Ok[int64, error](1)
	if a != b {
		t.Fatal("Ok 1 != Ok 1")
	}
	e1 := errors.New("x")
	e2 := errors.New("x")
	r1 := Err[int64](e1)
	r2 := Err[int64](e2)
	if r1 == r2 {
		t.Fatal("distinct error values compared equal")
	}
	r3 := Err[int64](e1)
	if r1 != r3 {
		t.Fatal("same error value compared unequal")
	}
}
