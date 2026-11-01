package check

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/types"
)

// The checker is monomorphic: types come from annotations, not from
// generalization. Metas still exist, but only for *local* inference —
// numeric-literal defaulting, resolving `+`'s overload, instantiating
// the type variables of an annotated signature or a builtin
// constructor at each use site. Every meta must be bound by the end of
// the check; an unbound meta is E0407 (a request for an annotation),
// not an inferred polymorphism.
//
// A meta may carry constraints — the overloaded-operator and literal
// rules of spec section 3.3 that cannot be expressed by unification
// alone:
//
//   - num: must be one of the built-in numeric types (arithmetic
//     operands and integer literals; the literal additionally gets a
//     default of Int).
//   - ord: must be comparable with < <= > >= (the numeric types plus
//     Char and String).
//   - eq: must be free of function types and type variables
//     (`==`/`!=`/`assert_eq`, spec 3.3 and 11.4).
//   - dflt: the type bound when the meta is still unsolved at the end
//     of the file (Int for integer literals). Metas without a default
//     report E0407 instead.

// metaRec is the bookkeeping attached to one unification variable:
// where it was born, what it stands for (for diagnostics), and its
// constraints. Records live on the canonical — still unbound — meta;
// binding a meta to another meta merges the records.
type metaRec struct {
	span diag.Span // where the meta was created (diagnostic site)
	desc string    // what the meta stands for ("the operand of +")

	num  bool       // constrained to a built-in numeric type
	ord  bool       // constrained to an ordered type
	eq   bool       // must not contain functions or type variables
	flo  bool       // constrained to a floating-point type (Float literals)
	intg bool       // constrained to an integral type (the `%` operator)
	dflt types.Type // default solution, or nil for "ask for an annotation"
}

// chase follows bound metas to their solutions — the checker's local
// copy of the operation types.Equal performs internally.
func chase(t types.Type) types.Type {
	for {
		m, ok := t.(*types.Meta)
		if !ok || !m.Bound() {
			return t
		}
		t = m.Solution()
	}
}

// newMeta creates an unbound unification variable recorded with the
// span and description diagnostics will cite.
func (c *checker) newMeta(sp diag.Span, desc string) *types.Meta {
	m := &types.Meta{ID: c.nextMeta}
	c.nextMeta++
	c.metas[m] = &metaRec{span: sp, desc: desc}
	return m
}

// recOf returns the constraint record of the unbound meta m, creating
// an empty one when the meta was not born inside this checker (all
// metas the checker makes go through newMeta, so this is defensive).
func (c *checker) recOf(m *types.Meta) *metaRec {
	if r, ok := c.metas[m]; ok {
		return r
	}
	r := &metaRec{}
	c.metas[m] = r
	return r
}

// mergeRec folds src's constraints into dst, keeping dst's
// provenance unless it has none.
func mergeRec(dst, src *metaRec) {
	dst.num = dst.num || src.num
	dst.ord = dst.ord || src.ord
	dst.eq = dst.eq || src.eq
	dst.flo = dst.flo || src.flo
	dst.intg = dst.intg || src.intg
	if dst.dflt == nil {
		dst.dflt = src.dflt
	}
	if dst.desc == "" {
		dst.desc = src.desc
		dst.span = src.span
	}
}

// constrain attaches flags to an unbound meta — or applies them
// immediately to a concrete type, which is where `x == y` on two
// already-known types reports its E0409. Returns false when the
// constraint failed on a concrete type.
func (c *checker) constrain(t types.Type, sp diag.Span, desc string, fn func(*metaRec)) bool {
	if m, ok := chase(t).(*types.Meta); ok {
		fn(c.recOf(m))
		return true
	}
	probe := &metaRec{span: sp, desc: desc}
	fn(probe)
	return c.satisfies(probe, t, sp)
}

// satisfies checks a concrete type against a constraint record —
// binding a constrained meta runs it, and so does flagging a concrete
// type directly.
func (c *checker) satisfies(r *metaRec, t types.Type, sp diag.Span) bool {
	t = chase(t)
	if r.num && !isNumeric(t) {
		c.mismatch(sp, "a numeric type", t)
		return false
	}
	if r.flo && !isFloat(t) {
		c.mismatch(sp, "a floating-point type", t)
		return false
	}
	if r.intg && !isIntegral(t) {
		c.mismatch(sp, "an integral type", t)
		return false
	}
	if r.ord && !isOrdered(t) {
		c.mismatch(sp, "an ordered type", t)
		return false
	}
	if r.eq && c.hasFuncOrVar(t) {
		d := diag.Error("E0409",
			fmt.Sprintf("`==` is not defined on %s: the type contains a function or a type variable", t)).
			At(sp).Set("type", t.String())
		c.diags.Add(d)
		return false
	}
	return true
}

// isNumeric reports whether t is one of the built-in numeric types of
// spec section 3.1 the arithmetic operators overload over.
func isNumeric(t types.Type) bool {
	con, ok := t.(*types.Con)
	if !ok || con.Qual != "" {
		return false
	}
	switch con.Name {
	case "Int", "Int32", "Int64", "Float":
		return true
	default:
		return false
	}
}

// isFloat reports whether t is a floating-point type — the literal
// `3.14` may not be an Int. Only Float exists today; the flag leaves
// room for Float32/Float64 variants.
func isFloat(t types.Type) bool {
	con, ok := t.(*types.Con)
	return ok && con.Qual == "" && con.Name == "Float"
}

// isIntegral reports whether t is an integral type — the operands of
// `%`. Go rejects float `%`, and the Gom rule matches it.
func isIntegral(t types.Type) bool {
	con, ok := t.(*types.Con)
	if !ok || con.Qual != "" {
		return false
	}
	switch con.Name {
	case "Int", "Int32", "Int64":
		return true
	default:
		return false
	}
}

// isOrdered reports whether t supports < <= > >=.
func isOrdered(t types.Type) bool {
	if isNumeric(t) {
		return true
	}
	con, ok := t.(*types.Con)
	if !ok || con.Qual != "" {
		return false
	}
	switch con.Name {
	case "Char", "String":
		return true
	default:
		return false
	}
}

// hasFuncOrVar reports whether t contains a function type or a rigid
// type variable — the two things `==` cannot compare in v1. Bound
// metas are chased; unbound metas do not count (they carry an eq flag
// instead and are re-checked when bound).
func (c *checker) hasFuncOrVar(t types.Type) bool {
	switch t := chase(t).(type) {
	case *types.Var:
		return true
	case *types.Meta:
		return false
	case *types.Func:
		return true
	case *types.Con:
		for _, a := range t.Args {
			if c.hasFuncOrVar(a) {
				return true
			}
		}
		return false
	case *types.Tuple:
		for _, e := range t.Elems {
			if c.hasFuncOrVar(e) {
				return true
			}
		}
		return false
	case *types.Go:
		for _, a := range t.Args {
			if c.hasFuncOrVar(a) {
				return true
			}
		}
		return false
	default:
		panic("unreachable")
	}
}

// occurs reports whether the unbound meta m occurs inside t — a bind
// that would create an infinite type.
func occurs(m *types.Meta, t types.Type) bool {
	switch t := chase(t).(type) {
	case *types.Var:
		return false
	case *types.Meta:
		return t == m
	case *types.Con:
		for _, a := range t.Args {
			if occurs(m, a) {
				return true
			}
		}
		return false
	case *types.Func:
		ps, ret := flattenFunc(t)
		for _, p := range ps {
			if occurs(m, p.Type) {
				return true
			}
		}
		return occurs(m, ret)
	case *types.Tuple:
		for _, e := range t.Elems {
			if occurs(m, e) {
				return true
			}
		}
		return false
	case *types.Go:
		for _, a := range t.Args {
			if occurs(m, a) {
				return true
			}
		}
		return false
	default:
		panic("unreachable")
	}
}

// flattenFunc unrolls a function type into its full parameter list and
// final result type, chasing bound metas in tail position — the local
// copy of the operation types.Equal performs internally (the helper
// there is unexported).
func flattenFunc(t types.Type) (params []types.FuncParam, ret types.Type) {
	for {
		f, ok := chase(t).(*types.Func)
		if !ok {
			return params, t
		}
		params = append(params, f.Params...)
		t = f.Ret
	}
}

// bind solves an unbound meta after the occurs check and the
// constraint check. It reports and returns false on failure.
func (c *checker) bind(m *types.Meta, t types.Type, sp diag.Span) bool {
	r := c.recOf(m)
	if occurs(m, t) {
		d := diag.Error("E0401",
			fmt.Sprintf("type mismatch: %s occurs in %s (infinite type)", m, t)).
			At(sp).Set("var", m.String()).Set("in", t.String())
		c.diags.Add(d)
		return false
	}
	if r != nil && !c.satisfies(r, t, sp) {
		return false
	}
	// Constraints must outlive the meta itself: an eq-flagged meta
	// bound to `Result ?a ?b` keeps the restriction on ?a and ?b.
	if r != nil && (r.num || r.ord || r.eq) {
		c.propagateFlags(r, t)
	}
	m.Bind(t)
	return true
}

// propagateFlags folds the constraint flags of a record into every
// unbound meta occurring inside a solution being bound.
func (c *checker) propagateFlags(r *metaRec, t types.Type) {
	switch t := chase(t).(type) {
	case *types.Var:
	case *types.Meta:
		inner := c.recOf(t)
		inner.num = inner.num || r.num
		inner.ord = inner.ord || r.ord
		inner.eq = inner.eq || r.eq
		inner.flo = inner.flo || r.flo
		inner.intg = inner.intg || r.intg
		if inner.span.IsZero() {
			inner.span = r.span
		}
	case *types.Con:
		for _, a := range t.Args {
			c.propagateFlags(r, a)
		}
	case *types.Func:
		ps, ret := flattenFunc(t)
		for _, p := range ps {
			c.propagateFlags(r, p.Type)
		}
		c.propagateFlags(r, ret)
	case *types.Tuple:
		for _, e := range t.Elems {
			c.propagateFlags(r, e)
		}
	case *types.Go:
		for _, a := range t.Args {
			c.propagateFlags(r, a)
		}
	default:
		panic("unreachable")
	}
}

// unify makes two types equal, solving metas on either side. It
// reports a type mismatch (E0401) at sp and returns false on failure.
// The expected type is the first argument so diagnostics read
// "expected X, got Y".
func (c *checker) unify(want, got types.Type, sp diag.Span) bool {
	w, g := chase(want), chase(got)

	// Metas first: binding beats every structural rule.
	if m, ok := w.(*types.Meta); ok {
		if gm, ok := g.(*types.Meta); ok && gm == m {
			return true
		}
		if gm, ok := g.(*types.Meta); ok {
			// Keep the expected side canonical so descriptions
			// bias toward "what was required".
			mergeRec(c.recOf(m), c.recOf(gm))
			delete(c.metas, gm)
			gm.Bind(m)
			return true
		}
		return c.bind(m, g, sp)
	}
	if m, ok := g.(*types.Meta); ok {
		return c.bind(m, w, sp)
	}

	switch w := w.(type) {
	case *types.Var:
		if gv, ok := g.(*types.Var); ok && w.ID == gv.ID {
			return true
		}
		c.mismatch(sp, w, g)
		return false
	case *types.Con:
		gc, ok := g.(*types.Con)
		if !ok || w.Qual != gc.Qual || w.Name != gc.Name || len(w.Args) != len(gc.Args) {
			c.mismatch(sp, w, g)
			return false
		}
		okAll := true
		for i := range w.Args {
			if !c.unify(w.Args[i], gc.Args[i], sp) {
				okAll = false
			}
		}
		return okAll
	case *types.Func:
		gf, ok := g.(*types.Func)
		if !ok {
			c.mismatch(sp, w, g)
			return false
		}
		return c.unifyFunc(w, gf, sp)
	case *types.Tuple:
		gt, ok := g.(*types.Tuple)
		if !ok || len(w.Elems) != len(gt.Elems) {
			c.mismatch(sp, w, g)
			return false
		}
		okAll := true
		for i := range w.Elems {
			if !c.unify(w.Elems[i], gt.Elems[i], sp) {
				okAll = false
			}
		}
		return okAll
	case *types.Go:
		gg, ok := g.(*types.Go)
		if !ok || w.Ref != gg.Ref || len(w.Args) != len(gg.Args) {
			c.mismatch(sp, w, g)
			return false
		}
		okAll := true
		for i := range w.Args {
			if !c.unify(w.Args[i], gg.Args[i], sp) {
				okAll = false
			}
		}
		return okAll
	case *types.Meta:
		// Metas on the wanted side are bound above, before the
		// structural cases — none reach here.
		panic("unreachable")
	default:
		panic("unreachable")
	}
}

// unifyFunc unifies two function types on their flattened curried
// form. Positional parameters pair off in order; each maximal run of
// labeled parameters pairs by label, matching the type equality of
// spec section 2.1 (consecutive labeled runs compare up to
// reordering, runs separated by a positional parameter do not merge).
//
// The parameter lists need not have the same length: `->` is
// right-associative, so when one side's parameters are a consistent
// prefix of the other's, the longer side's remaining parameters fold
// into the result — `A -> ?r` unifies with `A -> B -> R` by binding
// `?r` to `B -> R`, which is the same type once flattened. Labeled
// parameters inside that absorbed tail keep their labels (the tail is
// just a function type), but a labeled parameter and a positional one
// never pair off directly, and label runs do not merge across a
// positional parameter: a run member whose label the other run lacks
// is left over, and leftovers on *both* sides — or a leftover that
// still has parameters behind it — are a genuine mismatch.
func (c *checker) unifyFunc(w, g *types.Func, sp diag.Span) bool {
	wp, wr := flattenFunc(w)
	gp, gr := flattenFunc(g)
	for len(wp) > 0 && len(gp) > 0 {
		a, b := wp[0], gp[0]
		if a.Label == "" && b.Label == "" {
			if !c.unify(a.Type, b.Type, sp) {
				return false
			}
			wp, gp = wp[1:], gp[1:]
			continue
		}
		// At least one side starts a labeled run. An empty run means
		// a labeled parameter faces a positional one — different
		// slots, hence different types.
		ra, rb := labeledRun(wp), labeledRun(gp)
		if len(ra) == 0 || len(rb) == 0 {
			c.mismatch(sp, w, g)
			return false
		}
		// Pair the runs by label; members without a partner stay in
		// their side's parameter stream as leftovers. Each iteration
		// pairs at least one member or reports, so the walk
		// terminates.
		byLabel := make(map[string]int, len(rb))
		for k, p := range rb {
			if _, dup := byLabel[p.Label]; !dup {
				byLabel[p.Label] = k
			}
		}
		matched := make([]bool, len(rb))
		var aLeft []types.FuncParam
		for _, p := range ra {
			k, ok := byLabel[p.Label]
			if !ok {
				aLeft = append(aLeft, p)
				continue
			}
			matched[k] = true
			if !c.unify(p.Type, rb[k].Type, sp) {
				return false
			}
		}
		var bLeft []types.FuncParam
		for k, p := range rb {
			if !matched[k] {
				bLeft = append(bLeft, p)
			}
		}
		if len(aLeft) > 0 && len(bLeft) > 0 {
			// Each side declares labeled parameters the other's run
			// does not have; a later run cannot pick them up because
			// runs separated by a positional parameter do not merge.
			c.mismatch(sp, w, g)
			return false
		}
		wp = append(aLeft, wp[len(ra):]...)
		gp = append(bLeft, gp[len(rb):]...)
	}
	// One side ran out of parameters first: its result unifies with
	// the other side's remaining tail — the same type, since `->`
	// flattens.
	switch {
	case len(wp) > 0:
		return c.unify(&types.Func{Params: wp, Ret: wr}, gr, sp)
	case len(gp) > 0:
		return c.unify(wr, &types.Func{Params: gp, Ret: gr}, sp)
	default:
		return c.unify(wr, gr, sp)
	}
}

// labeledRun returns the maximal prefix of consecutive labeled
// parameters.
func labeledRun(ps []types.FuncParam) []types.FuncParam {
	n := 0
	for n < len(ps) && ps[n].Label != "" {
		n++
	}
	return ps[:n]
}

// mismatch reports the generic unification failure.
func (c *checker) mismatch(sp diag.Span, want, got any) {
	var w string
	switch want := want.(type) {
	case types.Type:
		w = want.String()
	case string:
		w = want
	default:
		panic("unreachable")
	}
	var g string
	switch got := got.(type) {
	case types.Type:
		g = got.String()
	case string:
		g = got
	default:
		panic("unreachable")
	}
	d := diag.Error("E0401", fmt.Sprintf("type mismatch: expected %s, got %s", w, g)).
		At(sp).Set("expected", w).Set("actual", g)
	c.diags.Add(d)
}

// rigidVars collects the rigid type variables of t in first-occurrence
// order, deduplicated by ID — the order CallInfo.TypeArgs records an
// instantiation in.
func rigidVars(t types.Type) []*types.Var {
	var out []*types.Var
	seen := map[int]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		switch t := chase(t).(type) {
		case *types.Var:
			if !seen[t.ID] {
				seen[t.ID] = true
				out = append(out, t)
			}
		case *types.Meta:
		case *types.Con:
			for _, a := range t.Args {
				walk(a)
			}
		case *types.Func:
			ps, ret := flattenFunc(t)
			for _, p := range ps {
				walk(p.Type)
			}
			walk(ret)
		case *types.Tuple:
			for _, e := range t.Elems {
				walk(e)
			}
		case *types.Go:
			for _, a := range t.Args {
				walk(a)
			}
		default:
			panic("unreachable")
		}
	}
	walk(t)
	return out
}

// instantiate replaces the rigid variables of a signature with fresh
// metas — one set per use site. It returns the instantiated type and
// the type arguments in signature order (the metas themselves; their
// solutions are fixed by later unification, which is also why
// CallInfo.TypeArgs can record them).
func (c *checker) instantiate(t types.Type, sp diag.Span) (types.Type, []types.Type) {
	vars := rigidVars(t)
	if len(vars) == 0 {
		return t, nil
	}
	sub := types.Subst{}
	args := make([]types.Type, len(vars))
	for i, v := range vars {
		m := c.newMeta(sp, fmt.Sprintf("the type argument for `%s`", v))
		sub[v.ID] = m
		args[i] = m
	}
	return sub.Apply(t), args
}

// finalize runs at the end of the check: integer-literal metas and
// other defaulted unknowns bind to their defaults, and every meta
// still unsolved reports E0407 — a request for a type annotation,
// because the monomorphic checker does not generalize (spec 3.3).
// After defaulting, eq-flagged metas whose solutions turn out to
// contain functions or type variables report E0409. Metas are visited
// in source order (then creation order) so the diagnostic list is
// deterministic.
// metaEntry pairs a meta with its record for ordered iteration.
type metaEntry struct {
	m *types.Meta
	r *metaRec
}

func (c *checker) finalize() {
	ms := make([]metaEntry, 0, len(c.metas))
	for m, r := range c.metas {
		ms = append(ms, metaEntry{m, r})
	}
	sortEntries(ms)

	// First pass: defaults. Binding one may chase-solve others. A
	// default that violates the meta's own constraints (a `%` operand
	// defaulting to Float) still binds — satisfies already reported
	// the constraint failure once, and an unbound leftover would only
	// add a redundant E0407.
	for _, e := range ms {
		if e.m.Bound() {
			continue
		}
		if d := defaultOf(e.r); d != nil {
			c.satisfies(e.r, d, e.r.span)
			e.m.Bind(d)
		}
	}
	// Second pass: report what is left.
	for _, e := range ms {
		m, r := e.m, e.r
		if !m.Bound() {
			desc := r.desc
			if desc == "" {
				desc = "this type"
			}
			d := diag.Error("E0407",
				fmt.Sprintf("type annotation required: %s could not be determined", desc)).
				At(r.span).Set("type", m.String())
			c.diags.Add(d)
			continue
		}
		if r.eq && c.hasFuncOrVar(m) {
			d := diag.Error("E0409",
				fmt.Sprintf("`==` is not defined on %s: the type contains a function or a type variable", m.Solution())).
				At(r.span).Set("type", m.Solution().String())
			c.diags.Add(d)
		}
	}
}

// sortEntries orders finalize's meta records by source position then
// creation order, making the emitted diagnostics deterministic.
func sortEntries(ms []metaEntry) {
	slices.SortFunc(ms, func(a, b metaEntry) int {
		if d := cmp.Compare(a.r.span.File, b.r.span.File); d != 0 {
			return d
		}
		if d := cmp.Compare(a.r.span.Start, b.r.span.Start); d != 0 {
			return d
		}
		return cmp.Compare(a.m.ID, b.m.ID)
	})
}

// defaultOf picks the type an unsolved meta defaults to. A
// float-constrained meta can only be Float — this matters for
// `1 + 2.0`, whose merged literal meta carries both the integer
// default and the float constraint. Other constrained metas keep
// their recorded default; a default that fails the meta's own
// constraints is dropped (the meta reports E0407 instead of
// defaulting to a type it may not have).
func defaultOf(r *metaRec) types.Type {
	if r.flo {
		return types.Float
	}
	return r.dflt
}

// unifyErr unifies two error types — the error channel of a `try`
// operand and of its escape boundary — without reporting. A mismatch
// between concrete types is left for the caller to describe (E0411);
// metas still bind (a failure there is an occurs report the caller
// should not mask).
func (c *checker) unifyErr(want, got types.Type, sp diag.Span) bool {
	w, g := chase(want), chase(got)
	if _, ok := w.(*types.Meta); ok {
		return c.unify(w, g, sp)
	}
	if _, ok := g.(*types.Meta); ok {
		return c.unify(w, g, sp)
	}
	return types.Equal(w, g)
}
