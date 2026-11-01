package eval

import (
	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// matchPat tries to bind pat to v in env, reporting whether it
// succeeded. A pattern that cannot match returns false without
// emitting a diagnostic (the caller tries the next arm); pattern
// shapes that reveal broken invariants fail with CodeInternal.
func (e *interp) matchPat(pat check.Pat, v Value, env *Env) bool {
	switch p := pat.(type) {
	case *check.WildPat:
		return true
	case *check.VarPat:
		env.Bind(p.Sym, v)
		return true
	case *check.LitPat:
		want := litValue(p.Kind, p.Text, pat.Span())
		return e.eqValue(want, v, pat.Span())
	case *check.TuplePat:
		t, ok := v.(*VTuple)
		if !ok || len(t.Elems) != len(p.Elems) {
			return false
		}
		for i, ep := range p.Elems {
			if !e.matchPat(ep, t.Elems[i], env) {
				return false
			}
		}
		return true
	case *check.ListPat:
		l, ok := v.(*VList)
		if !ok {
			return false
		}
		if p.Rest == nil && len(l.Elems) != len(p.Elems) {
			return false
		}
		if len(l.Elems) < len(p.Elems) {
			return false
		}
		for i, ep := range p.Elems {
			if !e.matchPat(ep, l.Elems[i], env) {
				return false
			}
		}
		if p.Rest != nil {
			rest := &VList{Elems: l.Elems[len(p.Elems):]}
			return e.matchPat(p.Rest, rest, env)
		}
		return true
	case *check.CtorPat:
		variant, ok := v.(*VVariant)
		if !ok {
			return false
		}
		if p.Ctor == nil {
			name := ""
			switch n := p.Name.(type) {
			case *syntax.Ident:
				name = n.Name
			case *syntax.Select:
				name = n.Sel.Name
			}
			failData(CodeInternal, "constructor pattern has no resolved symbol",
				pat.Span(), "name", name)
		}
		// Constructor identity is symbol identity; a bare-name value
		// (Ctor == nil, hand-built) compares by name.
		if variant.Ctor != p.Ctor && !(variant.Ctor == nil && variant.Name == p.Ctor.Name) {
			return false
		}
		if p.Fields != nil {
			for _, fp := range p.Fields {
				fv, ok := ctorField(variant, fp.Index, fp.Sym, fp.Name)
				if !ok {
					return false
				}
				if !e.matchPat(fp.Pat, fv, env) {
					return false
				}
			}
			return true
		}
		for i, a := range p.Args {
			idx := i
			if a.Label != "" {
				idx = e.ctorLabelIndex(p.Ctor, a.Label, pat.Span())
				if idx < 0 {
					failData(CodeInternal,
						"cannot map labeled ctor pattern argument", pat.Span(),
						"label", a.Label)
				}
			}
			fv, ok := variant.FieldByIndex(idx)
			if !ok {
				return false
			}
			if !e.matchPat(a.Pat, fv.V, env) {
				return false
			}
		}
		return true
	case *check.RecordPat:
		r, ok := v.(*VRecord)
		if !ok {
			return false
		}
		for _, fp := range p.Fields {
			var fv VField
			var found bool
			if fp.Index >= 0 {
				fv, found = r.FieldByIndex(fp.Index)
			}
			if !found {
				fv, found = r.FieldBySym(fp.Sym, fp.Name)
			}
			if !found {
				return false
			}
			if !e.matchPat(fp.Pat, fv.V, env) {
				return false
			}
		}
		return true
	case *check.OrPat:
		for _, alt := range p.Alts {
			// Alternatives bind into a scratch frame; the winning
			// alternative's bindings are merged on success so a
			// failed attempt leaves no partial bindings.
			scratch := env.Child()
			if e.matchPat(alt, v, scratch) {
				for sym, val := range scratch.vars {
					env.Bind(sym, val)
				}
				return true
			}
		}
		return false
	case *check.AsPat:
		if !e.matchPat(p.Pat, v, env) {
			return false
		}
		env.Bind(p.Sym, v)
		return true
	default:
		panic("unreachable")
	}
}

// ctorField finds variant's payload field by declared index, falling
// back to symbol/name lookup.
func ctorField(v *VVariant, index int, sym *resolve.Symbol, name string) (Value, bool) {
	if index >= 0 {
		if f, ok := v.FieldByIndex(index); ok {
			return f.V, true
		}
	}
	for _, f := range v.Fields {
		if f.Sym != nil && f.Sym == sym {
			return f.V, true
		}
	}
	for _, f := range v.Fields {
		if f.Name == name {
			return f.V, true
		}
	}
	return nil, false
}

// ctorLabelIndex maps a labeled ctor pattern argument to its payload
// index via the constructor's declared fields. Returns -1 when the
// constructor is unknown (no TypeDecl and the ctor Var was never
// evaluated) or has no such label.
func (e *interp) ctorLabelIndex(ctor *resolve.Symbol, label string, sp diag.Span) int {
	info := e.ctx.ctors[ctor]
	if info == nil {
		return -1
	}
	for _, f := range info.fields {
		if f.label == label {
			return f.index
		}
	}
	return -1
}

// eqValue is `==`: structural equality on values whose type contains
// no function (spec section 3.3). A function operand is a broken
// invariant — check reports it as E0409 before eval ever runs.
func (e *interp) eqValue(a, b Value, sp diag.Span) bool {
	if _, ok := a.(*VFunc); ok {
		failData(CodeInternal, "== applied to a function value", sp, "value", a.String())
	}
	if _, ok := b.(*VFunc); ok {
		failData(CodeInternal, "== applied to a function value", sp, "value", b.String())
	}
	return Equal(a, b)
}

// --- ctor registration (shared with eval.go) ---

// registerCtor records constructor metadata under its symbol. Called
// from TypeDecl registration and lazily from Var evaluation.
func (e *interp) registerCtor(info *ctorInfo) {
	if _, ok := e.ctx.ctors[info.sym]; !ok {
		e.ctx.ctors[info.sym] = info
	}
}

// ctorOf returns the registered info for a constructor symbol, or
// derives it lazily: pattern side from the pattern's scrutinee type,
// value side from the Var's own type. Returns nil when nothing is
// known.
func (e *interp) ctorOf(sym *resolve.Symbol, name string, typ types.Type) *ctorInfo {
	if info, ok := e.ctx.ctors[sym]; ok {
		return info
	}
	info := deriveCtor(sym, name, typ)
	if info != nil {
		e.registerCtor(info)
	}
	return info
}

// deriveCtor builds ctorInfo without a TypeDecl: a Func type supplies
// the payload (params in order, labels kept); a Con type is a
// nullary variant. Other shapes give nil.
func deriveCtor(sym *resolve.Symbol, name string, typ types.Type) *ctorInfo {
	switch t := typ.(type) {
	case *types.Con:
		return &ctorInfo{sym: sym, name: name, typ: t}
	case *types.Func:
		con, ok := t.Ret.(*types.Con)
		if !ok {
			return nil
		}
		fields := make([]fieldInfo, len(t.Params))
		for i, p := range t.Params {
			fields[i] = fieldInfo{label: p.Label, index: i}
		}
		return &ctorInfo{sym: sym, name: name, typ: con, fields: fields}
	default:
		return nil
	}
}
