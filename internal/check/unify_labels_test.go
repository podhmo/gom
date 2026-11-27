package check

import (
	"testing"

	"github.com/podhmo/gom/internal/types"
)

func TestLendFuncLabelsDoesNotMutateSharedNestedType(t *testing.T) {
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	shared := types.Fn(types.Int, param(types.Int))
	sig := &types.Func{Params: []types.FuncParam{param(types.Int)}, Ret: shared}
	want := types.Fn(types.Int,
		param(types.Int),
		types.FuncParam{Label: "y", Type: types.Int},
	)

	got := lendFuncLabels(sig, want)
	inner, ok := types.Chase(shared).(*types.Func)
	if !ok || len(inner.Params) != 1 {
		t.Fatalf("shared type = %v, want a one-parameter function", shared)
	}
	if inner.Params[0].Label != "" {
		t.Fatalf("shared nested type was mutated with label %q", inner.Params[0].Label)
	}
	gotInner, ok := types.Chase(types.Chase(got).(*types.Func).Ret).(*types.Func)
	if !ok || len(gotInner.Params) != 1 {
		t.Fatalf("lent type = %v, want a nested one-parameter function", got)
	}
	if gotInner.Params[0].Label != "y" {
		t.Fatalf("lent label = %q, want y", gotInner.Params[0].Label)
	}
}
