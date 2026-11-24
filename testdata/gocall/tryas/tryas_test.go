package tryas

import (
	"testing"

	"github.com/podhmo/gom/stdlib/option"
	"github.com/podhmo/gom/testdata/goiface"
)

// Runtime check for `Go.try_as` (spec 8.3): the emitted
// `v, ok := x.(T); option.FromGo(v, ok)` — Go produces `any`, gom
// extracts. Some on a matching type, None on a mismatch.
func TestTryAsGoProducedAny(t *testing.T) {
	if got := AsInt(goiface.Boxed(7)); got != (option.Option_Some[int64]{V0: 7}) {
		t.Fatalf("AsInt(Boxed 7) = %#v, want Some 7", got)
	}
	if got := AsInt(goiface.BoxedStr("s")); got != (option.Option_None[int64]{}) {
		t.Fatalf("AsInt(BoxedStr) = %#v, want None", got)
	}
	if got := AsStr(goiface.BoxedStr("hi")); got != (option.Option_Some[string]{V0: "hi"}) {
		t.Fatalf("AsStr(BoxedStr) = %#v, want Some \"hi\"", got)
	}
}

func TestTryAsGomRoundTrip(t *testing.T) {
	// n : Int boxes to any(int64(n)) at the goiface.Boxed call
	// boundary, then asserts back through Go.try_as.
	if got := BoxedInt(9); got != (option.Option_Some[int64]{V0: 9}) {
		t.Fatalf("BoxedInt(9) = %#v, want Some 9", got)
	}
}
