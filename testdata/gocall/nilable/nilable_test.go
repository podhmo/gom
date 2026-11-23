package nilable

import (
	"testing"

	"github.com/podhmo/gom/stdlib/result"
)

// Go-side assertions for `Go.nil` (spec 8.7): every nil-able
// signature crosses the boundary as a real Go nil, and a nil
// passed in travels back unchanged.
func TestGoNilOut(t *testing.T) {
	if s := NilSlice(struct{}{}); s != nil {
		t.Fatalf("NilSlice(struct{}{}) = %#v, want nil", s)
	}
	if m := NilMap(struct{}{}); m != nil {
		t.Fatalf("NilMap(struct{}{}) = %#v, want nil", m)
	}
	if a := NilAny(struct{}{}); a != nil {
		t.Fatalf("NilAny(struct{}{}) = %#v, want nil", a)
	}
}

func TestGoNilIn(t *testing.T) {
	if s := EchoSlice(nil); s != nil {
		t.Fatalf("EchoSlice(nil) = %#v, want nil", s)
	}
}

func TestGoNilResult(t *testing.T) {
	r := NilResult(struct{}{})
	if r.Tag() != result.ResultTagOk {
		t.Fatalf("NilResult(struct{}{}).Tag() = %v, want Ok", r.Tag())
	}
	if r.Value != nil {
		t.Fatalf("NilResult(struct{}{}).Value = %#v, want nil", r.Value)
	}
}
