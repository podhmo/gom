package iface

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/types"
)

// Check compares a package's public surface against the interface its
// .gomi declares (docs/spec.md section 7.6) and returns the E0603
// diagnostics for the members that disagree. complete reports whether
// impl covers every .gom file of the package — when it does not, an
// interface member with no implementation among the inputs is
// excused (it may live in a sibling file), while a member that IS
// implemented is still checked for agreement.
//
// The comparison is name-keyed: an interface member with no
// implementation of the same name is "not implemented"; an
// implementation member the interface does not name is private API
// and produces no diagnostic. Type equality is alpha-equality — spec
// section 7.6 allows differences in type-variable names only.
func Check(iface, impl *Package, complete bool) diag.List {
	var diags diag.List
	if iface.Name != "" && impl.Name != "" && iface.Name != impl.Name {
		diags.Add(diag.Error("E0603",
			fmt.Sprintf("interface declares package `%s` but the implementation is package `%s`", iface.Name, impl.Name)).
			At(iface.PkgSp).
			Set("interface_package", iface.Name).
			Set("implementation_package", impl.Name))
	}
	for _, m := range sortedMembers(iface) {
		w := impl.Members[m.Name]
		switch {
		case w == nil:
			if !complete {
				continue
			}
			diags.Add(diag.Error("E0603",
				fmt.Sprintf("interface member `%s` has no implementation", m.Name)).
				At(m.Sp).Set("member", m.Name))
		case m.Kind == MemberType && w.Kind == MemberType:
			diags = append(diags, checkTypeMember(m, w)...)
		case m.Kind == MemberFn:
			diags = append(diags, checkFnMember(m, w)...)
		case m.Kind == MemberLet:
			diags = append(diags, checkLetMember(m, w)...)
		default:
			// w's kind cannot match m's here except the two
			// handled above: a MemberType in the interface met by
			// a fn/let, or a MemberLet/fn met by a type — kind
			// mismatch.
			diags.Add(kindMismatch(m, w))
		}
	}
	return diags
}

// sortedMembers returns the package's members in name order so the
// diagnostic order is deterministic before the driver's final sort.
func sortedMembers(p *Package) []*Member {
	out := make([]*Member, 0, len(p.Members))
	for _, m := range p.Members {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b *Member) int {
		return cmp.Or(strings.Compare(a.Name, b.Name),
			cmp.Compare(a.Sp.Start, b.Sp.Start))
	})
	return out
}

// checkFnMember compares an interface `fn` signature: the
// implementation must supply a function whose flattened type equals
// the signature modulo type-variable renaming, with at least the
// declared arity. A let-bound function value satisfies it through
// its flattened arity — what `f p1 ... pN` means to callers is the
// same.
func checkFnMember(m, w *Member) diag.List {
	var diags diag.List
	if w.Kind != MemberFn && w.Kind != MemberLet {
		return diag.List{kindMismatch(m, w)}
	}
	arity := w.Arity
	if w.Kind == MemberLet {
		arity = fnArity(w.Type)
	}
	if arity < m.Arity {
		diags.Add(diag.Error("E0603",
			fmt.Sprintf("`%s` takes %d argument(s) in the interface but the implementation provides %d", m.Name, m.Arity, arity)).
			At(w.Sp).
			Set("member", m.Name).
			Set("interface_arity", m.Arity).
			Set("implementation_arity", arity).
			AddNote(fmt.Sprintf("interface signature: %s", m.Type)))
		return diags
	}
	if !alphaEqual(m.Type, w.Type) {
		diags.Add(typeMismatch(m, w))
	}
	return diags
}

// checkLetMember compares an interface `let` signature: the
// implementation's value type must equal the signature modulo
// renaming. Any implementation shape that produces a value of the
// right type satisfies it — a plain `let`, a generalized value-let,
// or a `fn` (a function is a value of its signature type).
func checkLetMember(m, w *Member) diag.List {
	if w.Kind != MemberFn && w.Kind != MemberLet {
		return diag.List{kindMismatch(m, w)}
	}
	if !alphaEqual(m.Type, w.Type) {
		return diag.List{typeMismatch(m, w)}
	}
	return nil
}

// checkTypeMember compares an interface `type` declaration: the
// implementation must declare a type of the same name with the same
// type-parameter count and, when the interface is concrete, a body
// of the same shape. An abstract `type T` in the interface is
// satisfied by any implementation body — that is the point of
// abstraction (spec section 7.3).
func checkTypeMember(m, w *Member) diag.List {
	var diags diag.List
	if m.TParams != w.TParams {
		diags.Add(diag.Error("E0603",
			fmt.Sprintf("type `%s` has %d type parameter(s) in the implementation but the interface declares %d", m.Name, w.TParams, m.TParams)).
			At(w.Sp).
			Set("member", m.Name).
			Set("interface_params", m.TParams).
			Set("implementation_params", w.TParams))
		return diags
	}
	switch b := m.Body.(type) {
	case *check.AbstractType:
		// Hidden representation: existence and arity are the contract.
		return nil
	case *check.SumType:
		ib, ok := w.Body.(*check.SumType)
		if !ok {
			return diag.List{kindMismatch(m, w)}
		}
		return checkSumBody(m, b, ib)
	case *check.RecordType:
		ib, ok := w.Body.(*check.RecordType)
		if !ok {
			return diag.List{kindMismatch(m, w)}
		}
		return checkFields(m, b.Fields, ib.Fields)
	case *check.AliasType:
		ib, ok := w.Body.(*check.AliasType)
		if !ok {
			return diag.List{kindMismatch(m, w)}
		}
		if !alphaEqual(b.Type, ib.Type) {
			diags.Add(typeMismatch(m, w))
		}
		return diags
	case nil:
		return nil
	default:
		panic("unreachable")
	}
}

// checkSumBody compares two sum-type bodies pairwise in declaration
// order — constructor order is part of the written interface.
func checkSumBody(m *Member, want, got *check.SumType) diag.List {
	var diags diag.List
	if len(want.Variants) != len(got.Variants) {
		diags.Add(diag.Error("E0603",
			fmt.Sprintf("type `%s` has %d variant(s) in the interface but %d in the implementation", m.Name, len(want.Variants), len(got.Variants))).
			At(got.Sp).
			Set("member", m.Name).
			Set("interface_variants", len(want.Variants)).
			Set("implementation_variants", len(got.Variants)))
		return diags
	}
	for i, wv := range want.Variants {
		gv := got.Variants[i]
		wn, gn := "", ""
		if wv.Sym != nil {
			wn = wv.Sym.Name
		}
		if gv.Sym != nil {
			gn = gv.Sym.Name
		}
		if wn != gn {
			diags.Add(diag.Error("E0603",
				fmt.Sprintf("type `%s` declares variant `%s` but the implementation has `%s`", m.Name, wn, gn)).
				At(gv.Sp).
				Set("member", m.Name).
				Set("interface_variant", wn).
				Set("implementation_variant", gn))
			continue
		}
		diags = append(diags, checkFields(m, wv.Fields, gv.Fields)...)
	}
	return diags
}

// checkFields compares the field lists of variants and records:
// count, param name (positional / named / labeled) and field type
// must all agree modulo renaming.
func checkFields(m *Member, want, got []check.TField) diag.List {
	var diags diag.List
	if len(want) != len(got) {
		return diag.List{diag.Error("E0603",
			fmt.Sprintf("`%s` has %d field(s) in the interface but %d in the implementation", m.Name, len(want), len(got))).
			At(m.Sp).
			Set("member", m.Name).
			Set("interface_fields", len(want)).
			Set("implementation_fields", len(got))}
	}
	for i, wf := range want {
		gf := got[i]
		if wn, gn := fieldParamName(wf), fieldParamName(gf); wn != gn {
			diags.Add(diag.Error("E0603",
				fmt.Sprintf("`%s`'s field %d is `%s` in the interface but `%s` in the implementation", m.Name, i, wn, gn)).
				At(gf.Sp).
				Set("member", m.Name).
				Set("interface_field", wn).
				Set("implementation_field", gn))
			continue
		}
		if !alphaEqual(wf.Type, gf.Type) {
			diags.Add(diag.Error("E0603",
				fmt.Sprintf("`%s`'s field `%s` has type `%s` in the implementation but the interface declares `%s`", m.Name, fieldParamName(wf), gf.Type, wf.Type)).
				At(gf.Sp).
				Set("member", m.Name).
				Set("interface_type", wf.Type.String()).
				Set("implementation_type", gf.Type.String()))
		}
	}
	return diags
}

// fieldParamName renders the call-site identity of a field: its
// label, name, or "" for positional payload.
func fieldParamName(f check.TField) string {
	if f.Label != "" {
		return "~" + f.Label
	}
	if f.Name != "" {
		return f.Name
	}
	return fmt.Sprintf("#%d", f.Index)
}

// kindMismatch reports an implementation member of a different kind
// than the interface declares.
func kindMismatch(m, w *Member) diag.Diagnostic {
	return diag.Error("E0603",
		fmt.Sprintf("`%s` is a %s in the implementation but the interface declares a %s", m.Name, memberKindName(w.Kind), memberKindName(m.Kind))).
		At(w.Sp).
		Set("member", m.Name).
		Set("interface_kind", memberKindName(m.Kind)).
		Set("implementation_kind", memberKindName(w.Kind))
}

// typeMismatch reports an implementation member whose type is not
// alpha-equal to the interface signature.
func typeMismatch(m, w *Member) diag.Diagnostic {
	return diag.Error("E0603",
		fmt.Sprintf("`%s` has type `%s` in the implementation but the interface declares `%s`", m.Name, w.Type, m.Type)).
		At(w.Sp).
		Set("member", m.Name).
		Set("interface_type", m.Type.String()).
		Set("implementation_type", w.Type.String())
}

// memberKindName renders a member kind for messages.
func memberKindName(k MemberKind) string {
	switch k {
	case MemberFn:
		return "function"
	case MemberLet:
		return "constant"
	case MemberType:
		return "type"
	default:
		return "member"
	}
}

// fnArity returns the arity of the flattened function type — the
// effective arity a let-bound function value offers.
func fnArity(t types.Type) int {
	n := 0
	for {
		f, ok := types.Chase(t).(*types.Func)
		if !ok {
			return n
		}
		n += len(f.Params)
		t = f.Ret
	}
}
