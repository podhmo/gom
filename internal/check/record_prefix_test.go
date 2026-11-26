package check

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/types"
)

func TestUseExpectedPrefixTypeFailedTrialLeavesMetasUntouched(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	external := c.newMeta(sp, "the surrounding expression")
	c.metas[external].num = true
	prefix := c.newMeta(sp, "the record type argument")
	box := func(arg types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{arg}}
	}
	tuple := func(x, y types.Type) types.Type {
		return &types.Tuple{Elems: []types.Type{x, y}}
	}

	before := len(c.metas)
	c.useExpectedPrefixType(box(tuple(prefix, prefix)), box(tuple(external, types.String)), sp)

	if external.Bound() {
		t.Fatalf("failed trial bound the surrounding inference meta to %v", types.Chase(external))
	}
	if prefix.Bound() {
		t.Fatalf("failed trial bound the record inference meta to %v", types.Chase(prefix))
	}
	if len(c.metas) != before {
		t.Fatalf("failed trial left %d clone metas registered", len(c.metas)-before)
	}
	if len(c.diags) != 0 {
		t.Fatalf("failed trial emitted diagnostics: %v", c.diags)
	}
}

func TestUseExpectedPrefixTypeDoesNotPropagateRecordConstraintToTypeArg(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	external := c.newMeta(sp, "the surrounding expression")
	c.metas[external].num = true
	tag := c.newMeta(sp, "the record type argument")
	count := func(arg types.Type) types.Type {
		return &types.Con{Name: "Count", Args: []types.Type{arg}}
	}

	c.useExpectedPrefixType(count(tag), external, sp)

	if external.Bound() {
		t.Fatalf("lookahead bound the surrounding inference meta to %v", types.Chase(external))
	}
	if tag.Bound() {
		t.Fatalf("lookahead propagated the record constraint into its type argument: %v", types.Chase(tag))
	}
	if len(c.diags) != 0 {
		t.Fatalf("lookahead emitted diagnostics: %v", c.diags)
	}
}

func TestUseExpectedPrefixTypeKeepsPartiallyConcreteExpectation(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	external := c.newMeta(sp, "the surrounding expression")
	prefix := c.newMeta(sp, "the record type argument")
	box := func(arg types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{arg}}
	}
	tuple := func(x, y types.Type) types.Type {
		return &types.Tuple{Elems: []types.Type{x, y}}
	}

	c.useExpectedPrefixType(box(prefix), box(tuple(types.GoAny, external)), sp)
	got, ok := types.Chase(prefix).(*types.Tuple)
	if !ok || len(got.Elems) != 2 {
		t.Fatalf("prefix solution = %v, want tuple", types.Chase(prefix))
	}
	if !types.Equal(got.Elems[0], types.GoAny) {
		t.Fatalf("first tuple element = %v, want Go.Any", got.Elems[0])
	}
	if types.Chase(got.Elems[1]) == types.Chase(external) {
		t.Fatalf("lookahead linked directly to the external meta")
	}
	openArg, ok := types.Chase(got.Elems[1]).(*types.Meta)
	if !ok {
		t.Fatalf("second tuple element = %v, want an open meta", got.Elems[1])
	}
	if c.metas[openArg].quiet || c.metas[openArg].silent {
		t.Fatalf("open replay meta must not suppress mismatch or annotation diagnostics")
	}
}

func TestUseExpectedPrefixTypePreservesSharedOpenArguments(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	external := c.newMeta(sp, "the surrounding expression")
	left := c.newMeta(sp, "the first record type argument")
	right := c.newMeta(sp, "the second record type argument")
	pair := func(x, y types.Type) types.Type {
		return &types.Con{Name: "Pair", Args: []types.Type{x, y}}
	}

	c.useExpectedPrefixType(pair(left, right), pair(external, external), sp)
	if !left.Bound() || !right.Bound() || types.Chase(left) != types.Chase(right) {
		t.Fatalf("shared open expectation was not preserved: left=%v right=%v", types.Chase(left), types.Chase(right))
	}
	if types.Chase(left) == types.Chase(external) {
		t.Fatalf("lookahead linked directly to the external meta")
	}
}

func TestUseExpectedPrefixTypePreservesNestedSharedOpenArguments(t *testing.T) {
	c := &checker{
		metas:  map[*types.Meta]*metaRec{},
		tyCons: map[conKey]*typeInfo{},
	}
	sp := diag.SpanIn("test.gom", 1, 2)
	external := c.newMeta(sp, "the surrounding expression")
	left := c.newMeta(sp, "the first record type argument")
	right := c.newMeta(sp, "the second record type argument")
	pair := func(x, y types.Type) types.Type {
		return &types.Con{Name: "Pair", Args: []types.Type{x, y}}
	}
	box := func(x types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{x}}
	}

	c.useExpectedPrefixType(pair(left, right), pair(external, box(external)), sp)
	if !left.Bound() || !right.Bound() {
		t.Fatalf("shared nested expectation was not replayed: left=%v right=%v", types.Chase(left), types.Chase(right))
	}
	rbox, ok := types.Chase(right).(*types.Con)
	if !ok || len(rbox.Args) != 1 {
		t.Fatalf("right solution = %v, want Box _", types.Chase(right))
	}
	if types.Chase(left) != types.Chase(rbox.Args[0]) {
		t.Fatalf("nested sharing was lost: left=%v nested=%v", types.Chase(left), types.Chase(rbox.Args[0]))
	}
}
