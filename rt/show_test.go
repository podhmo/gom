package rt_test

import (
	"strings"
	"testing"

	"github.com/podhmo/gom/rt"
	"github.com/podhmo/gom/stdlib/list"
	"github.com/podhmo/gom/stdlib/result"
)

type showPerson struct {
	Name string
	Age  int64
}

type showCircle struct{ R float64 }

type opaquePoint struct{ X, Y int }

type opaqueNamedSlice []float64

type viewLike[T any] []T

type chainNode struct{ Next any }

type chainEnd struct{}

func TestShowRuntimeComposites(t *testing.T) {
	rt.RegisterShowRecord(rt.TypeOf[showPerson](), false, "name", "age")
	rt.RegisterShowVariant(rt.TypeOf[showCircle](), "Circle", "r")

	got := rt.ShowRuntime(list.List[rt.Tuple2[int64, string]]{{V0: 1, V1: "x"}})
	if want := `[(1, "x")]`; got != want {
		t.Errorf("tuple list = %q, want %q", got, want)
	}
	got = rt.ShowRuntime(showPerson{Name: "n", Age: 3})
	if want := `{ name = "n", age = 3 }`; got != want {
		t.Errorf("record = %q, want %q", got, want)
	}
	got = rt.ShowRuntime([]showCircle{{R: 1.5}})
	if want := `[(Circle ~r: 1.5)]`; got != want {
		t.Errorf("variant list = %q, want %q", got, want)
	}
	got = rt.ShowRuntime(result.Ok[int64, error](4))
	if want := `Ok 4`; got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
	got = rt.ShowRuntime([]any{opaquePoint{X: 1, Y: 2}})
	if want := `[({1 2})]`; got != want {
		t.Errorf("opaque struct = %q, want %q", got, want)
	}
	got = rt.ShowRuntime(opaqueNamedSlice{1, 2})
	if want := `[1 2]`; got != want {
		t.Errorf("opaque named slice = %q, want %q", got, want)
	}
	got = rt.ShowRuntime([]any{opaqueNamedSlice{1, 2}})
	if want := `[([1 2])]`; got != want {
		t.Errorf("nested opaque named slice = %q, want %q", got, want)
	}
	got = rt.ShowRuntime(list.ListView(list.List[int64]{5, 6}))
	if want := `[5, 6]`; got != want {
		t.Errorf("list view = %q, want %q", got, want)
	}
	got = rt.ShowRuntime(struct {
		V0 int
		V1 string
	}{V0: 1, V1: "x"})
	if want := `(1, "x")`; got != want {
		t.Errorf("wide tuple shape = %q, want %q", got, want)
	}

	rt.RegisterShowVariant(rt.TypeOf[chainNode](), "Node", "next")
	rt.RegisterShowVariant(rt.TypeOf[chainEnd](), "End")
	var chain any = chainEnd{}
	for range 22 {
		chain = chainNode{Next: chain}
	}
	got = rt.ShowRuntime(chain)
	if want := "End"; !strings.Contains(got, want) {
		t.Errorf("22-link chain = %q, want %q", got, want)
	}
}
