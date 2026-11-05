package check

import (
	"testing"

	"github.com/podhmo/gom/internal/types"
)

// A declaration's parameters stand for already-checked arguments. This
// must preserve deferred constraints and must not bind a caller's rigid
// variables, even when several occurrences share a declaration.
func TestEqProbeDeclarationArguments(t *testing.T) {
	a := &types.Var{ID: 10, Name: "a"}
	box := &typeInfo{kind: kindRecord, params: []*types.Var{a},
		fields: []*fieldInfo{{typ: a}}}
	c := &checker{tyCons: map[conKey]*typeInfo{{name: "Box"}: box}}
	apply := func(arg types.Type) types.Type {
		return &types.Con{Name: "Box", Args: []types.Type{arg}}
	}
	m, n := &types.Meta{ID: 1}, &types.Meta{ID: 2}
	var sink []*types.Meta
	if !c.eqProbe(&types.Tuple{Elems: []types.Type{apply(m), apply(n)}}, &sink) {
		t.Fatal("unbound arguments should defer equality")
	}
	if len(sink) != 2 || sink[0] != m || sink[1] != n {
		t.Fatalf("deferred arguments = %v, want both metas", sink)
	}
	m.Bind(&types.Func{Params: []types.FuncParam{{Type: types.Int}}, Ret: types.Int})
	if c.eqProbe(apply(m), nil) {
		t.Fatal("function argument was accepted after inference")
	}
	if c.eqProbe(&types.Tuple{Elems: []types.Type{apply(types.Int), a}}, nil) {
		t.Fatal("declaration parameter binding escaped into the caller")
	}
}
