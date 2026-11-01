package flat2

import (
	"errors"
	"testing"
)

func panicValue(f func()) (v any) {
	defer func() { v = recover() }()
	f()
	return nil
}

func TestZeroValueIsInvalid(t *testing.T) {
	var r Result[int64, error]
	if r.Tag() != ResultTag_Invalid {
		t.Fatalf("zero tag = %v", r.Tag())
	}
	v := panicValue(func() { GetOrElse(r, 9) })
	if v == nil {
		t.Fatal("expected panic on zero Result")
	}
	t.Logf("panic: %v", v)
	v = panicValue(func() { ResultToGo(r) })
	if v == nil {
		t.Fatal("expected panic on zero Result in ToGo")
	}
	t.Logf("ToGo panic: %v", v)
}

// A Go caller CAN still write Result{Value: 1} (state stays 0) — but
// the invalid tag makes the misuse loud instead of silent.
func TestGoLiteralWithoutTagIsInvalid(t *testing.T) {
	r := Result[int64, error]{Value: 1} // no way to set the tag
	v := panicValue(func() { ResultToGo(r) })
	if v == nil {
		t.Fatal("expected panic on tag-less literal")
	}
	t.Logf("panic: %v", v)
}

// Err(nil) remains constructible — the same payload-level hole the
// sealed rep has with Result_Err{nil}. The tag fixes the rep-level
// hole only.
func TestErrNilHole(t *testing.T) {
	r := Err[int64](error(nil))
	v, err := ResultToGo(r)
	if v != 0 || err != nil {
		t.Fatalf("Err(nil) round trip = (%d, %v)", v, err)
	}
	t.Log("Err(nil) is legal in both reps; only the rep-level zero is guarded")
}

func TestInferenceAndErgonomics(t *testing.T) {
	r := Ok[int64, error](7)
	if !r.IsOk() {
		t.Fatal("IsOk false on Ok")
	}
	if got := GetOrElse(r, 9); got != 7 {
		t.Fatalf("GetOrElse = %d", got)
	}
	v, e := r.Unwrap()
	if v != 7 || e != nil {
		t.Fatalf("Unwrap = (%d, %v)", v, e)
	}
	s := Err[int64](errors.New("x"))
	if got := GetOrElse(s, 9); got != 9 {
		t.Fatalf("GetOrElse err = %d", got)
	}
	if s.IsOk() {
		t.Fatal("IsOk true on Err")
	}
}

func TestPipe(t *testing.T) {
	r := ParsePort(" 8080 ")
	v, err := ResultToGo(r)
	if err != nil || v != 8080 {
		t.Fatalf("ParsePort ok: %d %v", v, err)
	}
	r = ParsePort("notanum")
	if _, err = ResultToGo(r); err == nil {
		t.Fatal("expected error")
	}
	t.Logf("wrapped: %v", err)
}
