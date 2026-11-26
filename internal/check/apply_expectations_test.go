package check

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

func TestApplyParamExpectationsDoesNotBindInferenceState(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	accT := &types.Con{Name: "Acc"}
	a := c.newMeta(sp, "the folded element")
	b := c.newMeta(sp, "the fold accumulator")
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	folderT := types.Fn(b, param(b), param(a))
	listT := &types.Con{Name: "List", Args: []types.Type{a}}
	params := []types.FuncParam{param(folderT), param(b), param(listT)}
	fnT := types.Fn(b, params...)
	args := []syntax.Arg{
		{Value: &syntax.Lambda{}},
		{Value: &syntax.RecordLit{}},
		{Value: &syntax.Ident{}},
	}

	before := len(c.metas)
	x := c.applyParamExpectations(fnT, params, args, accT, sp)
	if x == nil {
		t.Fatal("expected parameter views from the static result type")
	}
	if a.Bound() {
		t.Fatalf("lookahead bound the element meta to %v", types.Chase(a))
	}
	if b.Bound() {
		t.Fatalf("lookahead bound the accumulator meta to %v", types.Chase(b))
	}
	if len(c.metas) != before {
		t.Fatalf("lookahead left %d clone metas registered", len(c.metas)-before)
	}
	if len(c.diags) != 0 {
		t.Fatalf("lookahead emitted diagnostics: %v", c.diags)
	}
	got, ok := x.at(1)
	if !ok {
		t.Fatal("accumulator parameter did not gain the static result type")
	}
	if !types.Equal(got, accT) {
		t.Fatalf("accumulator expectation = %v, want %v", got, accT)
	}
}
