package types

import "slices"

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
		// Labeled parameters compare in declared order — the
		// positional Go ABI makes the run order part of the type
		// identity (docs/spec.md section 2.1).
		for i := range ap {
			if ap[i].Label != bp[i].Label || !Equal(ap[i].Type, bp[i].Type) {
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

// StripLabels removes every parameter label — check's
// unlabeled→labeled introduction (spec section 2.1) admits an
// unlabeled produced function for an expected labeled shape, and
// labels erase to the same Go func type, so downstream
// type-equality checks (the Core validator's residual check)
// compare modulo labels.
func StripLabels(t Type) Type {
	switch t := Chase(t).(type) {
	case *Var, *Meta:
		return t
	case *Con:
		n := *t
		n.Args = stripLabelsArgs(t.Args)
		return &n
	case *Func:
		n := *t
		n.Params = make([]FuncParam, len(t.Params))
		for i, p := range t.Params {
			n.Params[i] = FuncParam{Type: StripLabels(p.Type)}
		}
		n.Ret = StripLabels(t.Ret)
		return &n
	case *Tuple:
		n := *t
		n.Elems = stripLabelsArgs(t.Elems)
		return &n
	case *Go:
		n := *t
		n.Args = stripLabelsArgs(t.Args)
		return &n
	default:
		panic("unreachable")
	}
}

func stripLabelsArgs(ts []Type) []Type {
	out := make([]Type, len(ts))
	for i, t := range ts {
		out[i] = StripLabels(t)
	}
	return out
}

// EqualLent reports whether a (expected, labels declared) and b
// (produced) are equal modulo the introduction's label lending:
// Equal's pairing plus — a labeled parameter of a may face an
// unlabeled parameter of b at the same position when b's
// remaining tail declares no labels of its own. This is the
// label-aware half of unifyFunc's admission without metas or
// diagnostics; check uses it where a value's produced type must
// match an annotation the introduction may have lent into.
func EqualLent(a, b Type) bool {
	switch a := Chase(a).(type) {
	case *Var:
		bv, ok := Chase(b).(*Var)
		return ok && a.ID == bv.ID
	case *Meta:
		bm, ok := Chase(b).(*Meta)
		return ok && a == bm
	case *Con:
		bc, ok := Chase(b).(*Con)
		if !ok || a.Qual != bc.Qual || a.Name != bc.Name || len(a.Args) != len(bc.Args) {
			return false
		}
		for i := range a.Args {
			if !EqualLent(a.Args[i], bc.Args[i]) {
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
		for len(ap) > 0 && len(bp) > 0 {
			x, y := ap[0], bp[0]
			if x.Label == "" && y.Label == "" {
				if !EqualLent(x.Type, y.Type) {
					return false
				}
				ap, bp = ap[1:], bp[1:]
				continue
			}
			if x.Label != "" && y.Label == "" && allPositional(bp) {
				// Introduction: the produced parameter borrows
				// the expected label by position.
				if !EqualLent(x.Type, y.Type) {
					return false
				}
				ap, bp = ap[1:], bp[1:]
				continue
			}
			// Both sides open a labeled run — pair members by
			// label name in the same relative order (the
			// positional Go ABI makes order significant).
			ra, rb := labeledRun(ap), labeledRun(bp)
			if len(ra) == 0 || len(rb) == 0 {
				return false
			}
			byLabel := make(map[string]int, len(rb))
			for k, p := range rb {
				if _, dup := byLabel[p.Label]; !dup {
					byLabel[p.Label] = k
				}
			}
			matched := make([]bool, len(rb))
			lastK := -1
			for _, p := range ra {
				k, ok := byLabel[p.Label]
				if !ok || matched[k] || k < lastK || !EqualLent(p.Type, rb[k].Type) {
					return false
				}
				lastK = k
				matched[k] = true
			}
			for k := range rb {
				if !matched[k] {
					return false
				}
			}
			ap, bp = ap[len(ra):], bp[len(rb):]
		}
		// A leftover tail folds into the result — `->` flattens.
		switch {
		case len(ap) > 0:
			return EqualLent(&Func{Params: ap, Ret: ar}, br)
		case len(bp) > 0:
			return EqualLent(ar, &Func{Params: bp, Ret: br})
		default:
			return EqualLent(ar, br)
		}
	case *Tuple:
		bt, ok := Chase(b).(*Tuple)
		if !ok || len(a.Elems) != len(bt.Elems) {
			return false
		}
		for i := range a.Elems {
			if !EqualLent(a.Elems[i], bt.Elems[i]) {
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
			if !EqualLent(a.Args[i], bg.Args[i]) {
				return false
			}
		}
		return true
	default:
		panic("unreachable")
	}
}

// allPositional reports whether every parameter is unlabeled.
func allPositional(ps []FuncParam) bool {
	for _, p := range ps {
		if p.Label != "" {
			return false
		}
	}
	return true
}

// labeledRun returns the maximal prefix of consecutive labeled
// parameters.
func labeledRun(ps []FuncParam) []FuncParam {
	n := 0
	for n < len(ps) && ps[n].Label != "" {
		n++
	}
	return ps[:n]
}
