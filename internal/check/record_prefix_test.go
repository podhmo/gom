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
