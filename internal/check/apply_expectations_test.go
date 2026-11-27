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
	folderT := types.Fn(b, types.FuncParam{Label: "acc", Type: b}, param(a))
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
	wantFolder := types.Fn(accT, types.FuncParam{Label: "acc", Type: accT}, param(a))
	if !types.Equal(folder, wantFolder) {
		t.Fatalf("folder expectation = %v, want %v", folder, wantFolder)
	}
	if len(c.metas) != before {
		t.Fatalf("merging an open speculative leaf left %d forwarding metas", len(c.metas)-before)
	}
}

func TestApplyParamExpectationsActivatesCallbackResultRecord(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	accT := &types.Con{Name: "Acc"}
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
	a := c.newMeta(sp, "the callback result")
	intT := &types.Con{Name: "Int"}
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	callbackT := types.Fn(a, param(intT))
	params := []types.FuncParam{param(callbackT)}
	fnT := types.Fn(a, params[0])
	args := []syntax.Arg{{Value: &syntax.Lambda{Body: &syntax.RecordLit{}}}}

	x := c.applyParamExpectations(fnT, params, args, accT, sp)
	if x == nil {
		t.Fatal("callback result record did not activate expectations")
	}
	if a.Bound() {
		t.Fatalf("lookahead bound the callback result meta to %v", types.Chase(a))
	}
	got, ok := x.at(0, callbackT)
	if !ok {
		t.Fatal("callback parameter did not gain the static result type")
	}
	want := types.Fn(accT, param(intT))
	if !types.Equal(got, want) {
		t.Fatalf("callback expectation = %v, want %v", got, want)
	}
}

func TestApplyParamExpectationsPipeReverseSliceBoundary(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	outT := &types.Con{Name: "Out"}
	c.tyCons[conKey{name: "Out"}] = &typeInfo{kind: kindRecord}
	a := c.newMeta(sp, "the mapped element")
	b := c.newMeta(sp, "the callback result")
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	callbackT := types.Fn(b, param(a))
	params := []types.FuncParam{param(callbackT), param(types.List(a))}
	fnT := types.Fn(types.GoSlice(b), params...)
	body := &syntax.TupleLit{Elems: []syntax.Expr{&syntax.Ident{}, &syntax.RecordLit{}}}
	args := []syntax.Arg{{Value: &syntax.Lambda{Body: body}}}
	pairT := types.Tup(types.GoAny, outT)
	pipeWant := types.Fn(types.List(pairT), param(c.untypedMeta()))

	x := c.applyParamExpectationsImpl(fnT, params, args, pipeWant, sp, true)
	if x == nil {
		t.Fatal("reverse slice boundary did not activate pipe expectations")
	}
	got, ok := x.at(0, callbackT)
	if !ok {
		t.Fatal("callback parameter did not gain the reverse boundary result type")
	}
	want := types.Fn(pairT, param(a))
	if !types.Equal(got, want) {
		t.Fatalf("callback expectation = %v, want %v", got, want)
	}
	if x := c.applyParamExpectations(fnT, params, args, pipeWant, sp); x != nil {
		t.Fatalf("ordinary application used the pipe-only boundary retry: %+v", x)
	}
}

func TestApplyParamExpectationsIgnoresNonResultRecordInCallback(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	accT := &types.Con{Name: "Acc"}
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
	a := c.newMeta(sp, "the callback result")
	intT := &types.Con{Name: "Int"}
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	callbackT := types.Fn(a, param(intT))
	params := []types.FuncParam{param(callbackT)}
	fnT := types.Fn(a, params[0])
	body := &syntax.Block{Stmts: []syntax.Expr{&syntax.RecordLit{}, &syntax.Ident{}}}
	args := []syntax.Arg{{Value: &syntax.Lambda{Body: body}}}

	if x := c.applyParamExpectations(fnT, params, args, accT, sp); x != nil {
		t.Fatalf("non-result record activated expectations: %+v", x)
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

func TestApplyParamExpectationsPropagatesLabeledContext(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	acc := &types.Con{Name: "Acc"}
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
	a := c.newMeta(sp, "identity payload")
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	fnT := types.Fn(a, param(a))
	params := []types.FuncParam{param(a)}
	args := []syntax.Arg{{Value: &syntax.RecordLit{}}}
	labeled := types.Fn(acc, types.FuncParam{Label: "x", Type: acc})

	x := c.applyParamExpectations(fnT, params, args, labeled, sp)
	if x == nil {
		t.Fatal("expected parameter views from the static result type")
	}
	got, ok := x.at(0, a)
	if !ok {
		t.Fatal("labeled context was not propagated")
	}
	want := types.Fn(acc, types.FuncParam{Label: "x", Type: acc})
	if !types.Equal(got, want) {
		t.Fatalf("labeled context = %v, want %v", got, want)
	}
}

func TestApplyParamExpectationsReifyingOpenLeavesKeepsLabels(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	acc := &types.Con{Name: "Acc"}
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
	original := c.newMeta(sp, "the callback parameter")
	openReturn := c.newMeta(sp, "the speculative callback return")
	labeled := types.Fn(openReturn, types.FuncParam{Label: "x", Type: acc})
	x := &applyParamExpectations{
		c:           c,
		params:      []types.Type{labeled},
		informative: []bool{true},
		open:        map[*types.Meta]*types.Meta{},
		sp:          sp,
	}

	got, ok := x.at(0, original)
	if !ok {
		t.Fatal("labeled context was not propagated")
	}
	f, ok := types.Chase(got).(*types.Func)
	if !ok || len(f.Params) != 1 {
		t.Fatalf("labeled context = %v, want a one-parameter function", got)
	}
	if f.Params[0].Label != "x" {
		t.Fatalf("label lost from contextual view: %q", f.Params[0].Label)
	}
	if !types.Equal(f.Params[0].Type, acc) {
		t.Fatalf("parameter type = %v, want %v", f.Params[0].Type, acc)
	}
	if _, open := types.Chase(f.Ret).(*types.Meta); !open {
		t.Fatalf("open return was over-specified as %v", f.Ret)
	}
}

func TestApplyParamExpectationsMergedOriginalKeepsLabels(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	acc := &types.Con{Name: "Acc"}
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
	labeledCallback := types.Fn(acc, types.FuncParam{Label: "x", Type: acc})
	openOriginal := c.newMeta(sp, "open original field")
	openSpeculative := c.newMeta(sp, "open speculative callback")
	box := func(cb, payload types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{cb, payload}}
	}
	x := &applyParamExpectations{
		c:           c,
		params:      []types.Type{box(openSpeculative, acc)},
		informative: []bool{true},
		open:        map[*types.Meta]*types.Meta{},
		sp:          sp,
	}

	got, ok := x.at(0, box(labeledCallback, openOriginal))
	if !ok {
		t.Fatal("labeled original context was not propagated")
	}
	want := box(types.Fn(acc, types.FuncParam{Label: "x", Type: acc}), acc)
	if !types.Equal(got, want) {
		t.Fatalf("merged context = %v, want %v", got, want)
	}
}

func TestApplyParamExpectationsMergesWhenSpeculativeLabelIsDiscarded(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	acc := &types.Con{Name: "Acc"}
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	positionalCallback := types.Fn(acc, param(acc))
	labeledCallback := types.Fn(acc, types.FuncParam{Label: "x", Type: acc})
	openOriginal := c.newMeta(sp, "open original field")
	box := func(cb, payload types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{cb, payload}}
	}
	x := &applyParamExpectations{
		c:           c,
		params:      []types.Type{box(labeledCallback, acc)},
		informative: []bool{true},
		open:        map[*types.Meta]*types.Meta{},
		sp:          sp,
	}
	original := box(positionalCallback, openOriginal)

	got, ok := x.at(0, original)
	if !ok {
		t.Fatal("merge was rejected even though the positional original discarded the speculative label")
	}
	want := box(positionalCallback, acc)
	if !types.Equal(got, want) {
		t.Fatalf("merged context = %v, want %v", got, want)
	}
}

func TestApplyParamExpectationsLabelMergeKeepsParameterOrder(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	acc := &types.Con{Name: "Acc"}
	c.tyCons[conKey{name: "Acc"}] = &typeInfo{kind: kindRecord}
	param := func(t types.Type) types.FuncParam { return types.FuncParam{Type: t} }
	labeled := func(label string) types.FuncParam {
		return types.FuncParam{Label: label, Type: acc}
	}
	openOriginal := c.newMeta(sp, "open original parameter")
	openSpeculative := c.newMeta(sp, "open speculative payload")
	original := types.Fn(acc, param(openOriginal), labeled("x"))
	speculative := types.Fn(acc,
		param(&types.Con{Name: "List", Args: []types.Type{openSpeculative}}),
		labeled("y"),
	)
	x := &applyParamExpectations{
		c:           c,
		params:      []types.Type{speculative},
		informative: []bool{true},
		open:        map[*types.Meta]*types.Meta{},
		sp:          sp,
	}

	got, ok := x.at(0, original)
	if !ok {
		t.Fatal("labeled context was not propagated")
	}
	f, ok := types.Chase(got).(*types.Func)
	if !ok || len(f.Params) != 2 {
		t.Fatalf("labeled context = %v, want a two-parameter function", got)
	}
	if f.Params[0].Label != "" {
		t.Fatalf("first parameter kept label %q", f.Params[0].Label)
	}
	if f.Params[1].Label != "x" {
		t.Fatalf("second parameter label = %q, want x", f.Params[1].Label)
	}
	list, ok := types.Chase(f.Params[0].Type).(*types.Con)
	if !ok || list.Name != "List" || len(list.Args) != 1 {
		t.Fatalf("first parameter = %v, want List _", f.Params[0].Type)
	}
	if _, open := types.Chase(list.Args[0]).(*types.Meta); !open {
		t.Fatalf("speculative payload was over-specified as %v", list.Args[0])
	}
	if !types.Equal(f.Params[1].Type, acc) {
		t.Fatalf("second parameter = %v, want %v", f.Params[1].Type, acc)
	}
	if !types.Equal(f.Ret, acc) {
		t.Fatalf("return = %v, want %v", f.Ret, acc)
	}
}
