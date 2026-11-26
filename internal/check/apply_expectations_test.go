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
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
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
	got, ok := x.at(1, b)
	if !ok {
		t.Fatal("accumulator parameter did not gain the static result type")
	}
	if !types.Equal(got, accT) {
		t.Fatalf("accumulator expectation = %v, want %v", got, accT)
	}

	before = len(c.metas)
	folder, ok := x.at(0, folderT)
	if !ok {
		t.Fatal("folder parameter did not gain the static result type")
	}
	wantFolder := types.Fn(accT, param(accT), param(a))
	if !types.Equal(folder, wantFolder) {
		t.Fatalf("folder expectation = %v, want %v", folder, wantFolder)
	}
	if len(c.metas) != before {
		t.Fatalf("merging an open speculative leaf left %d forwarding metas", len(c.metas)-before)
	}
}

func TestMergeContextualTypeFillsOpenArgs(t *testing.T) {
	c := &checker{metas: map[*types.Meta]*metaRec{}}
	sp := diag.SpanIn("test.gom", 1, 2)
	open := c.newMeta(sp, "open")
	acc := &types.Con{Name: "Acc"}
	box := func(arg types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{arg}}
	}

	got := c.mergeContextualType(box(open), box(acc), map[*types.Meta]*types.Meta{}, sp)
	if !types.Equal(got, box(acc)) {
		t.Fatalf("mergeContextualType filled = %v, want %v", got, box(acc))
	}
}

func TestMergeContextualTypeKeepsEarlierConcreteSolution(t *testing.T) {
	c := &checker{metas: map[*types.Meta]*metaRec{}}
	sp := diag.SpanIn("test.gom", 1, 2)
	open := c.newMeta(sp, "open")
	acc := &types.Con{Name: "Acc"}
	other := &types.Con{Name: "Other"}
	box := func(arg types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{arg}}
	}

	if got := c.mergeContextualType(box(acc), box(other), map[*types.Meta]*types.Meta{}, sp); !types.Equal(got, box(acc)) {
		t.Fatalf("concrete original = %v, want %v", got, box(acc))
	}
	if got := c.mergeContextualType(open, open, map[*types.Meta]*types.Meta{}, sp); got != open {
		t.Fatalf("open original = %v, want the original meta", got)
	}
}

func TestMergeContextualTypeExtendsCurriedSpeculativeTail(t *testing.T) {
	c := &checker{metas: map[*types.Meta]*metaRec{}}
	sp := diag.SpanIn("test.gom", 1, 2)
	openRet := c.newMeta(sp, "open return")
	intT := &types.Con{Name: "Int"}
	acc := &types.Con{Name: "Acc"}
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }

	original := types.Fn(openRet, param(intT))
	speculative := types.Fn(acc, param(intT), param(intT))
	got := c.mergeContextualType(original, speculative, map[*types.Meta]*types.Meta{}, sp)
	if !types.Equal(got, speculative) {
		t.Fatalf("curried merge = %v, want %v", got, speculative)
	}
}

func TestMergeContextualTypeKeepsStaticLabels(t *testing.T) {
	c := &checker{metas: map[*types.Meta]*metaRec{}}
	sp := diag.SpanIn("test.gom", 1, 2)
	open := c.newMeta(sp, "open")
	acc := &types.Con{Name: "Acc"}
	speculative := types.Fn(acc, types.FuncParam{Label: "x", Type: acc})

	got := c.mergeContextualType(open, speculative, map[*types.Meta]*types.Meta{}, sp)
	if !types.Equal(got, speculative) {
		t.Fatalf("label-preserving merge = %v, want %v", got, speculative)
	}
}

func TestMergeContextualTypePreservesLongerOriginalTail(t *testing.T) {
	c := &checker{metas: map[*types.Meta]*metaRec{}}
	sp := diag.SpanIn("test.gom", 1, 2)
	openParam := c.newMeta(sp, "open parameter")
	openSpeculativeRet := c.newMeta(sp, "open speculative return")
	intT := &types.Con{Name: "Int"}
	acc := &types.Con{Name: "Acc"}
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }

	original := types.Fn(acc, param(openParam), param(intT))
	speculative := types.Fn(openSpeculativeRet, param(acc))
	got := c.mergeContextualType(original, speculative, map[*types.Meta]*types.Meta{}, sp)
	want := types.Fn(acc, param(acc), param(intT))
	if !types.Equal(got, want) {
		t.Fatalf("shorter speculative merge = %v, want %v", got, want)
	}
}
