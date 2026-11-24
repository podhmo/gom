package tuple9

import "testing"

// A >8-ary tuple emits as an anonymous V-field struct: the Go test
// constructs it literally (Go→Gom) and Identity instantiates T with
// it inside generated code (Gom→Go).
type t9 = struct {
	V0 int64
	V1 int64
	V2 int64
	V3 int64
	V4 int64
	V5 int64
	V6 int64
	V7 int64
	V8 int64
}

func TestWideTupleDestructure(t *testing.T) {
	if got := Big(t9{V0: 1, V8: 9}); got != 10 {
		t.Fatalf("Big = %d, want 10", got)
	}
}

func TestWideTupleConstruct(t *testing.T) {
	v := Nine(struct{}{})
	if v.V0 != 1 || v.V8 != 9 {
		t.Fatalf("Nine() = %+v, want V0=1 V8=9", v)
	}
}

func TestWideTupleThroughGoGeneric(t *testing.T) {
	v := t9{V0: 1, V1: 2, V2: 3, V3: 4, V4: 5, V5: 6, V6: 7, V7: 8, V8: 9}
	if got := Echo(v); got != v {
		t.Fatalf("Echo = %+v, want %+v", got, v)
	}
}
