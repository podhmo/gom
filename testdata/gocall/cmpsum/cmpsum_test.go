package cmpsum

import (
	"testing"

	"github.com/podhmo/gom/stdlib/option"
)

// The generated `slices.Contains` calls must compile and compare
// without a runtime panic: every Shape payload is comparable, and
// the Option box's element is.
func TestComparableSumContains(t *testing.T) {
	xs := []Shape{Shape_Circle{R: 1.5}, Shape_Empty{}}
	if !Has(xs, Shape_Circle{R: 1.5}) {
		t.Fatal("Has(xs, Circle 1.5) = false, want true")
	}
	if Has(xs, Shape_Circle{R: 2.0}) {
		t.Fatal("Has(xs, Circle 2.0) = true, want false")
	}
	if !Has(xs, Shape_Empty{}) {
		t.Fatal("Has(xs, Empty) = false, want true")
	}
}

func TestComparableOptionContains(t *testing.T) {
	xs := []option.Option[Shape]{
		option.Option_Some[Shape]{V0: Shape_Empty{}},
		option.Option_None[Shape]{},
	}
	if !HasSome(xs, option.Option_Some[Shape]{V0: Shape_Empty{}}) {
		t.Fatal("HasSome(xs, Some Empty) = false, want true")
	}
	if !HasSome(xs, option.Option_None[Shape]{}) {
		t.Fatal("HasSome(xs, None) = false, want true")
	}
	if HasSome(xs, option.Option_Some[Shape]{V0: Shape_Circle{R: 1}}) {
		t.Fatal("HasSome(xs, Some Circle 1) = true, want false")
	}
}
