package types

import (
	"cmp"
	"slices"
)

// Equal reports structural type equality: the same constructor and
// recursively equal arguments.
//
// Function types compare on their flattened curried form — `A -> B ->
// R` and `A -> (B -> R)` are the same type, so nested tails are
// unrolled first (Fn normally keeps them flat; a bound Meta in tail
// position can still hide a function type).
//
// For function types, within each maximal run of consecutive labeled
// parameters the order does not matter — `~a: X -> ~b: Y -> R` equals
// `~b: Y -> ~a: X -> R`, but a labeled parameter and a positional one
// never swap, and labeled runs separated by a positional parameter are
// not merged (docs/spec.md section 2.1). Each run is normalized by
// sorting on the label before pairwise comparison.
//
// Bound metas are chased; unbound metas and vars compare by identity
// (pointer and ID respectively). This is the equality users observe
// through type errors; it is not a unification.
func Equal(a, b Type) bool {
	switch a := Chase(a).(type) {
	case *Var:
		bv, ok := Chase(b).(*Var)
		return ok && a.ID == bv.ID
	case *Meta:
		// Unbound metas are identical only to themselves.
		bm, ok := Chase(b).(*Meta)
		return ok && a == bm
	case *Con:
		bc, ok := Chase(b).(*Con)
		if !ok || a.Qual != bc.Qual || a.Name != bc.Name || len(a.Args) != len(bc.Args) {
			return false
		}
		for i := range a.Args {
			if !Equal(a.Args[i], bc.Args[i]) {
				return false
			}
		}
		return true
	case *Func:
		bf, ok := Chase(b).(*Func)
		if !ok {
			return false
		}
		ap, ar := flattenFunc(a)
		bp, br := flattenFunc(bf)
		if len(ap) != len(bp) || !Equal(ar, br) {
			return false
		}
		an := normalizeLabels(ap)
		bn := normalizeLabels(bp)
		for i := range an {
			if an[i].Label != bn[i].Label || !Equal(an[i].Type, bn[i].Type) {
				return false
			}
		}
		return true
	case *Tuple:
		bt, ok := Chase(b).(*Tuple)
		if !ok || len(a.Elems) != len(bt.Elems) {
			return false
		}
		for i := range a.Elems {
			if !Equal(a.Elems[i], bt.Elems[i]) {
				return false
			}
		}
		return true
	case *Go:
		bg, ok := Chase(b).(*Go)
		if !ok || a.Ref != bg.Ref || len(a.Args) != len(bg.Args) {
			return false
		}
		for i := range a.Args {
			if !Equal(a.Args[i], bg.Args[i]) {
				return false
			}
		}
		return true
	default:
		panic("unreachable")
	}
}

// flattenFunc unrolls a function type into its full parameter list
// and final result type, chasing bound metas in tail position. Fn
// already produces the flat canonical form, so this is a no-op for
// well-formed types; it matters for hand-built Func literals and for
// a bound Meta sitting in Ret position, which inference can
// legitimately introduce.
func flattenFunc(f *Func) (params []FuncParam, ret Type) {
	ret = Chase(f.Ret)
	g, ok := ret.(*Func)
	if !ok {
		return f.Params, ret
	}
	params = slices.Clone(f.Params)
	for ok {
		params = append(params, g.Params...)
		ret = Chase(g.Ret)
		g, ok = ret.(*Func)
	}
	return params, ret
}

// normalizeLabels sorts each maximal run of consecutive labeled
// parameters by label, leaving positional parameters and the gaps
// between runs in place (docs/spec.md section 2.1).
func normalizeLabels(ps []FuncParam) []FuncParam {
	out := slices.Clone(ps)
	for i := 0; i < len(out); {
		if out[i].Label == "" {
			i++
			continue
		}
		j := i
		for j < len(out) && out[j].Label != "" {
			j++
		}
		slices.SortFunc(out[i:j], func(a, b FuncParam) int {
			return cmp.Compare(a.Label, b.Label)
		})
		i = j
	}
	return out
}
