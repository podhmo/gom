package types

// Subst is a substitution over rigid type variables, keyed by Var.ID.
// It is the form later phases need: monomorphic code carries no Var,
// so Apply is the identity there; polymorphic instantiation (P2) maps
// each bound Var to its argument.
type Subst map[int]Type

// Apply returns the type with every substituted variable replaced.
// Bound metas are chased and their solutions rewritten; unbound metas
// are left alone (their solutions may introduce vars later — P2 owns
// that machinery).
func (s Subst) Apply(t Type) Type {
	switch t := t.(type) {
	case *Var:
		if r, ok := s[t.ID]; ok {
			return r
		}
		return t
	case *Meta:
		if !t.bound {
			return t
		}
		return s.Apply(t.sol)
	case *Con:
		args, changed := s.applyAllChanged(t.Args)
		if !changed {
			return t
		}
		return &Con{Qual: t.Qual, Name: t.Name, Args: args}
	case *Func:
		ps := make([]FuncParam, len(t.Params))
		for i, p := range t.Params {
			ps[i] = FuncParam{Label: p.Label, Type: s.Apply(p.Type)}
		}
		// Fn keeps the canonical flat form: the substitution may
		// instantiate Ret to a function type.
		return Fn(s.Apply(t.Ret), ps...)
	case *Tuple:
		elems, changed := s.applyAllChanged(t.Elems)
		if !changed {
			return t
		}
		return &Tuple{Elems: elems}
	case *Go:
		args, changed := s.applyAllChanged(t.Args)
		if !changed {
			return t
		}
		return &Go{Ref: t.Ref, Args: args}
	default:
		panic("unreachable")
	}
}

// applyAllChanged allocates only after the first replacement. Type nodes
// are immutable, so unchanged branches can be shared without copying.
func (s Subst) applyAllChanged(ts []Type) ([]Type, bool) {
	var out []Type
	for i, t := range ts {
		r := s.Apply(t)
		if out == nil && r != t {
			out = append([]Type(nil), ts...)
		}
		if out != nil {
			out[i] = r
		}
	}
	if out == nil {
		return ts, false
	}
	return out, true
}

// FreeVars returns the rigid type variables occurring in t, keyed by
// Var.ID. Bound metas are chased. It exists for generalization and
// occurs checks (P2).
func FreeVars(t Type) map[int]*Var {
	out := map[int]*Var{}
	collectFreeVars(t, out)
	return out
}

func collectFreeVars(t Type, out map[int]*Var) {
	switch t := t.(type) {
	case *Var:
		out[t.ID] = t
	case *Meta:
		if t.bound {
			collectFreeVars(t.sol, out)
		}
	case *Con:
		for _, a := range t.Args {
			collectFreeVars(a, out)
		}
	case *Func:
		for _, p := range t.Params {
			collectFreeVars(p.Type, out)
		}
		collectFreeVars(t.Ret, out)
	case *Tuple:
		for _, e := range t.Elems {
			collectFreeVars(e, out)
		}
	case *Go:
		for _, a := range t.Args {
			collectFreeVars(a, out)
		}
	default:
		panic("unreachable")
	}
}
