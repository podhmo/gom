package rt

import "testing"

func TestArrayOps(t *testing.T) {
	a := ArrayMake[int64](3)
	if got := ArrayLen(a); got != 3 {
		t.Fatalf("len = %d", got)
	}
	ArraySet(a, 1, 10)
	if got := ArrayGet(a, 1); got != 10 {
		t.Fatalf("get = %d", got)
	}
	if got := ArrayGet(a, 0); got != 0 {
		t.Fatalf("zero fill = %d", got)
	}
}

func TestArrayGetOutOfRangePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	ArrayGet(ArrayMake[int64](1), 1)
}
