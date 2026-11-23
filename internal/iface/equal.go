package iface

import (
	"slices"

	"github.com/podhmo/gom/internal/types"
)

// alphaEqual reports whether a and b are the same type up to
// renaming of type variables — the "only type-variable names may
// differ" rule of docs/spec.md section 7.6. The comparison mirrors
// types.Equal exactly: function types flatten (so `T1 -> T2 -> R`
// equals `T1 -> (T2 -> R)`), labeled parameters compare pairwise in
// declared order (the positional Go ABI makes run order part of the
// type), and bound metas are chased. The one
// difference is Var comparison: instead of identical IDs, the two
// sides' variables are matched by a bijection built left-to-right on
// first encounter, so `a -> a` equals `b -> b` but not `a -> b`.
func alphaEqual(a, b types.Type) bool {
	eq := alphaEq{l2r: map[int]int{}, r2l: map[int]int{}}
	return eq.eq(a, b)
}

// alphaEq carries the variable pairing of one comparison.
type alphaEq struct {
	l2r map[int]int // left  Var.ID → right Var.ID
	r2l map[int]int // right Var.ID → left Var.ID
}

func (e *alphaEq) eq(a, b types.Type) bool {
	switch a := types.Chase(a).(type) {
	case *types.Var:
		bv, ok := types.Chase(b).(*types.Var)
		return ok && e.eqVar(a, bv)
	case *types.Meta:
		// Unbound metas are identical only to themselves.
		bm, ok := types.Chase(b).(*types.Meta)
		return ok && a == bm
	case *types.Con:
		bc, ok := types.Chase(b).(*types.Con)
		if !ok || a.Qual != bc.Qual || a.Name != bc.Name || len(a.Args) != len(bc.Args) {
			return false
		}
		for i := range a.Args {
			if !e.eq(a.Args[i], bc.Args[i]) {
				return false
			}
		}
		return true
	case *types.Func:
		bf, ok := types.Chase(b).(*types.Func)
		if !ok {
			return false
		}
		ap, ar := flattenFunc(a)
		bp, br := flattenFunc(bf)
		if len(ap) != len(bp) || !e.eq(ar, br) {
			return false
		}
		for i := range ap {
			if ap[i].Label != bp[i].Label || !e.eq(ap[i].Type, bp[i].Type) {
				return false
			}
		}
		return true
	case *types.Tuple:
		bt, ok := types.Chase(b).(*types.Tuple)
		if !ok || len(a.Elems) != len(bt.Elems) {
			return false
		}
		for i := range a.Elems {
			if !e.eq(a.Elems[i], bt.Elems[i]) {
				return false
			}
		}
		return true
	case *types.Go:
		bg, ok := types.Chase(b).(*types.Go)
		if !ok || a.Ref != bg.Ref || len(a.Args) != len(bg.Args) {
			return false
		}
		for i := range a.Args {
			if !e.eq(a.Args[i], bg.Args[i]) {
				return false
			}
		}
		return true
	default:
		panic("unreachable")
	}
}

// eqVar pairs two type variables, keeping the pairing bijective: a
// Var on the left pairs with exactly one Var on the right, and two
// variables on the same side never merge.
func (e *alphaEq) eqVar(a, b *types.Var) bool {
	if c, ok := e.l2r[a.ID]; ok {
		return c == b.ID
	}
	if _, ok := e.r2l[b.ID]; ok {
		return false // b already pairs with another left var
	}
	e.l2r[a.ID] = b.ID
	e.r2l[b.ID] = a.ID
	return true
}

// flattenFunc unrolls a function type into its full parameter list
// and final result type, chasing bound metas in tail position — the
// same canonical form types.Equal uses.
func flattenFunc(f *types.Func) (params []types.FuncParam, ret types.Type) {
	ret = types.Chase(f.Ret)
	g, ok := ret.(*types.Func)
	if !ok {
		return f.Params, ret
	}
	params = slices.Clone(f.Params)
	for ok {
		params = append(params, g.Params...)
		ret = types.Chase(g.Ret)
		g, ok = ret.(*types.Func)
	}
	return params, ret
}
