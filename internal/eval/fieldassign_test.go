package eval

import (
	"reflect"
	"testing"
)

func TestGoFieldValueConvertsUintRepresentation(t *testing.T) {
	type host struct {
		N uint
	}
	ft := reflect.TypeOf(host{}).Field(0).Type
	got, ok := goFieldValue(&VInt{N: 1, U: true}, ft)
	if !ok {
		t.Fatalf("uint64-backed Uint did not convert to raw uint field")
	}
	if got.Type() != ft || got.Uint() != 1 {
		t.Fatalf("converted value = %v (%v), want uint(1)", got, got.Type())
	}
}
