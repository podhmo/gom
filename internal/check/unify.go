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

	num   bool       // constrained to a built-in numeric type
	ord   bool       // constrained to an ordered type
	eq    bool       // must not contain functions or type variables
	flo   bool       // constrained to a floating-point type (Float literals)
	intg  bool       // constrained to an integral type (the `%` operator)
	quiet bool       // another diagnostic already covers this meta — no E0407
	dflt  types.Type // default solution, or nil for "ask for an annotation"
}

// newMeta creates an unbound unification variable recorded with the
// span and description diagnostics will cite.
func (c *checker) newMeta(sp diag.Span, desc string) *types.Meta {
	m := &types.Meta{ID: c.nextMeta}
	c.nextMeta++
	c.metas[m] = &metaRec{span: sp, desc: desc}
	return m
}

// quietMeta creates a meta that may stay unsolved without an E0407:
// it is a recovery artifact of a failure some other diagnostic
// already reports (an unresolved name, a hidden Go member, an
// over-applied call whose arity error was emitted). The flag travels
// with unification — binding a quiet meta into a type propagates it
// to the metas inside, and merging records ORs it — so the silence
// follows the value.
func (c *checker) quietMeta(sp diag.Span, desc string) *types.Meta {
	m := c.newMeta(sp, desc)
	c.metas[m].quiet = true
	return m
}

// markQuiet flags t's record quiet when t is an unbound meta; bound
// or concrete types are unaffected.
func (c *checker) markQuiet(t types.Type) {
	if m, ok := types.Chase(t).(*types.Meta); ok {
		c.recOf(m).quiet = true
	}
}

// isQuiet reports whether t is an unbound meta carrying the quiet
// flag — used to keep recovery metas derived from a silenced value
// from reporting E0407 of their own.
func (c *checker) isQuiet(t types.Type) bool {
	m, ok := types.Chase(t).(*types.Meta)
	return ok && c.recOf(m).quiet
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
	dst.quiet = dst.quiet || src.quiet
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
	if m, ok := types.Chase(t).(*types.Meta); ok {
		fn(c.recOf(m))
		return true
	}
	probe := &metaRec{span: sp, desc: desc}
	fn(probe)
	ok := c.satisfies(probe, t, sp)
	// The eq judgment must re-run when a still-unbound meta *inside*
	// the concrete type solves — `xs == xs` at `List ?n` is clean
	// today only because ?n is unknown. Flagging every meta the
	// (declaration-aware) walk crosses defers the same check to the
	// bind site and to finalize.
	if ok && probe.eq {
		var ms []*types.Meta
		c.eqProbe(t, &ms)
		for _, m := range ms {
			c.recOf(m).eq = true
		}
	}
	return ok
}

// satisfies checks a concrete type against a constraint record —
// binding a constrained meta runs it, and so does flagging a concrete
// type directly.
func (c *checker) satisfies(r *metaRec, t types.Type, sp diag.Span) bool {
	sp = c.pos(sp)
	t = types.Chase(t)
	// A file-local `type Int` (or `Float`, `Char`, `String`, ...)
	// shadows the builtin the numeric/ordered constraints mean —
	// its Con spells the same name, so the constraint is rejected
	// before the name-based tables accept it (E0415). `eq` is not
	// here on purpose: structural equality is defined on declared
	// types too.
	if con, ok := t.(*types.Con); ok && con.Qual == "" && c.shadowedBuiltin(con.Name) &&
		(r.num || r.flo || r.intg || r.ord) {
		c.shadowedErr(sp, "the constraint on this type", con.Name)
		return false
	}
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
		td := types.Display(t, metaNamer())
		d := diag.Error("E0409",
			fmt.Sprintf("`==` is not defined on %s: the type contains a function or a type variable", td)).
			At(sp).Set("type", td)
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
// type variable — the two things `==` cannot compare in v1 (spec 3.3).
// Bound metas are chased; unbound metas do not count (they carry an
// eq flag instead and are re-checked when bound).
//
// The walk expands *local* named types the way recordCycle does: a
// record with a function field, an ADT whose payload carries one, or
// `Box (Int -> Int)` all reach the Func through the declaration.
// Arguments are checked before declarations, including phantom arguments.
// Consequently a declaration can be inspected once with its parameters
// treated as already checked, without constructing its instantiations.
// Recursive declarations form a finite graph even when their arguments grow.
func (c *checker) hasFuncOrVar(t types.Type) bool {
	w := &eqWalk{c: c}
	return w.has(t)
}

// eqProbe reports whether t is equality-clean and, if sink is
// non-nil, collects every unbound meta it crosses — the inner
// positions whose solutions must still satisfy eq.
func (c *checker) eqProbe(t types.Type, sink *[]*types.Meta) bool {
	w := &eqWalk{c: c, sink: sink}
	return !w.has(t)
}

// eqWalk is the shared traversal behind hasFuncOrVar and eqProbe.
// sink, when set, collects every unbound meta the walk crosses.
type eqWalk struct {
	c     *checker
	seen  map[*typeInfo]bool
	bound map[int]int
	sink  *[]*types.Meta
}

// has reports whether t contains a function type or a rigid variable.
func (w *eqWalk) has(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Var:
		return w.bound[t.ID] == 0
	case *types.Meta:
		if w.sink != nil {
			*w.sink = append(*w.sink, t)
		}
		return false
	case *types.Func:
		return true
	case *types.Tuple:
		return slices.ContainsFunc(t.Elems, w.has)
	case *types.Go:
		return slices.ContainsFunc(t.Args, w.has)
	case *types.Con:
		if slices.ContainsFunc(t.Args, w.has) {
			return true
		}
		if w.expandable(t) {
			ti := w.c.tyCons[conKey{qual: t.Qual, name: t.Name}]
			return w.expand(ti, func() bool {
				switch ti.kind {
				case kindRecord:
					for _, f := range ti.fields {
						if w.has(f.typ) {
							return true
						}
					}
				case kindSum:
					for _, v := range ti.variants {
						for _, f := range v.fields {
							if w.has(f.typ) {
								return true
							}
						}
					}
				case kindAlias:
					if ti.aliasRes {
						return w.has(ti.alias)
					}
				}
				return false
			})
		}
		return false
	default:
		panic("unreachable")
	}
}

// expandable reports whether the constructor names a *local* declared
// record, sum or alias — the shapes whose declarations contribute to
// the eq judgment. Builtin constructors (List, Option, Result, Task,
// Ref, Go slices/maps) contribute their arguments directly; foreign
// and abstract types stay opaque.
func (w *eqWalk) expandable(t *types.Con) bool {
	if t.Qual != "" {
		return false
	}
	ti, ok := w.c.tyCons[conKey{qual: t.Qual, name: t.Name}]
	return ok && ti.kind != kindAbstract
}

// expand visits a declaration's body without substituting its arguments.
// has has already checked every argument, so parameter occurrences cannot
// add a function, rigid variable, or unresolved meta to that judgment.
// Keep parameter binding scoped: callers' rigid variables must still fail.
func (w *eqWalk) expand(ti *typeInfo, f func() bool) bool {
	if w.seen[ti] {
		return false
	}
	if w.seen == nil {
		w.seen = map[*typeInfo]bool{}
		w.bound = map[int]int{}
	}
	w.seen[ti] = true
	for _, p := range ti.params {
		w.bound[p.ID]++
	}
	defer func() {
		for _, p := range ti.params {
			w.bound[p.ID]--
		}
	}()
	return f()
}

// goReprComparable reports whether t's emitted Go representation
// satisfies Go's `comparable` constraint — the checker's half of the
// generic-Go-call constraint check (spec section 8.5). The judgment
// is over generated code shapes, not Go semantics: a sum type is a
// sealed interface (comparable), a record a struct (comparable iff
// every field is), Option an interface, Result a flat struct, a
// declared Gom type variable an `any` parameter (not comparable).
// *types.Go handles delegate to the ComparableSource oracle —
// `types.Comparable` on the named type behind the handle. Opaque
// shapes the checker cannot see (abstract or foreign declared types)
// answer false rather than guess: a rejected call is recoverable, a
// generated file that fails `go build` is not.
func (c *checker) goReprComparable(t types.Type) bool {
	w := &cmpWalk{c: c}
	return w.has(t)
}

// cmpWalk is the comparable-representation traversal behind
// goReprComparable — it mirrors eqWalk's declared-type expansion.
type cmpWalk struct {
	c     *checker
	seen  []recordVisit
	steps int
}

// has reports whether t's Go representation is comparable.
func (w *cmpWalk) has(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Var:
		// A Gom type parameter instantiates as `any` — it cannot
		// satisfy `comparable` (spec section 8.5's boundary).
		return false
	case *types.Meta:
		// An unbound meta is undecided — finalize reports E0407.
		return true
	case *types.Func:
		return false
	case *types.Tuple:
		// rt.TupleN[A, ...] is a struct — comparable iff every
		// element is.
		for _, e := range t.Elems {
			if !w.has(e) {
				return false
			}
		}
		return true
	case *types.Go:
		if src, ok := w.c.src.(ComparableSource); ok {
			return src.GoComparable(t)
		}
		return false
	case *types.Con:
		if t.Qual == "Go" {
			switch t.Name {
			case "Slice", "Map":
				return false
			default:
				// Go.Int (int) and Go.Any (any) are comparable.
				return true
			}
		}
		if t.Qual == "" && !w.c.shadowedBuiltin(t.Name) {
			switch t.Name {
			case "Unit", "Bool", "Int", "Int32", "Int64", "Float",
				"Char", "String", "GoError":
				// bool, the int widths, float64, rune, string,
				// struct{} and the error interface — all
				// comparable Go representations.
				return true
			case "List", "Array":
				// list.List[A] is a named slice; Array renders []A.
				return false
			case "Result":
				// result.Result[T, E] is a struct — comparable iff
				// its payload fields are.
				for _, a := range t.Args {
					if !w.has(a) {
						return false
					}
				}
				return true
			case "Task":
				// rt.Task[T] is a struct — comparable iff T is.
				if len(t.Args) == 1 {
					return w.has(t.Args[0])
				}
				return true
			case "Option", "Ref", "Ptr", "Chan", "RecvChan", "SendChan":
				// option.Option[A] is an interface; Ref and Ptr are
				// pointers; channels are channels — all comparable.
				return true
			}
		}
		// A declared type: sealed-interface sums are comparable,
		// records are comparable iff every field is, aliases expand.
		if w.expandable(t) {
			ti := w.c.tyCons[conKey{qual: t.Qual, name: t.Name}]
			return w.expand(t, ti, func(sub types.Subst) bool {
				switch ti.kind {
				case kindSum:
					return true
				case kindRecord:
					for _, f := range ti.fields {
						if !w.has(sub.Apply(f.typ)) {
							return false
						}
					}
					return true
				case kindAlias:
					if ti.aliasRes {
						return w.has(sub.Apply(ti.alias))
					}
				}
				return false
			})
		}
		// An unqualified builtin the file shadows (tyCons has the
		// declaration — expandable caught it), an abstract type, or
		// a foreign nominal type: the repr is not visible here.
		return false
	default:
		panic("unreachable")
	}
}

// expandable reports whether the constructor names a *local* declared
// record, sum or alias — the shapes whose declarations contribute to
// the comparable judgment.
func (w *cmpWalk) expandable(t *types.Con) bool {
	if t.Qual != "" {
		return false
	}
	ti, ok := w.c.tyCons[conKey{qual: t.Qual, name: t.Name}]
	return ok && ti.kind != kindAbstract
}

// expand runs f under the declaration's type-parameter substitution,
// bounded like eqWalk.expand — a revisit or an exhausted budget
// answers false, the "cannot prove comparable" value.
func (w *cmpWalk) expand(t *types.Con, ti *typeInfo, f func(types.Subst) bool) bool {
	v := recordVisit{ti: ti, args: t.Args}
	for _, s := range w.seen {
		if s.ti == ti && sameTypes(s.args, v.args) {
			return false
		}
	}
	if w.steps >= recordCycleLimit {
		return false
	}
	w.seen = append(w.seen, v)
	w.steps++
	defer func() { w.seen = w.seen[:len(w.seen)-1] }()
	sub := types.Subst{}
	for i, p := range ti.params {
		if i < len(t.Args) {
			sub[p.ID] = t.Args[i]
		}
	}
	return f(sub)
}

// occurs reports whether the unbound meta m occurs inside t — a bind
// that would create an infinite type.
func occurs(m *types.Meta, t types.Type) bool {
	switch t := types.Chase(t).(type) {
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
		f, ok := types.Chase(t).(*types.Func)
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
	sp = c.pos(sp)
	r := c.recOf(m)
	if occurs(m, t) {
		name := metaNamer()
		mv, in := types.Display(m, name), types.Display(t, name)
		d := diag.Error("E0401",
			fmt.Sprintf("type mismatch: %s occurs in %s (infinite type)", mv, in)).
			At(sp).Set("var", mv).Set("in", in)
		c.diags.Add(d)
		return false
	}
	if r != nil && !c.satisfies(r, t, sp) {
		return false
	}
	// Constraints must outlive the meta itself: an eq-flagged meta
	// bound to `Result ?a ?b` keeps the restriction on ?a and ?b, and
	// a quiet meta keeps the metas inside its solution quiet too.
	if r != nil && (r.num || r.ord || r.eq || r.flo || r.intg || r.quiet) {
		c.propagateFlags(r, t)
	}
	m.Bind(t)
	return true
}

// propagateFlags folds the constraint flags of a record into every
// unbound meta occurring inside a solution being bound.
func (c *checker) propagateFlags(r *metaRec, t types.Type) {
	switch t := types.Chase(t).(type) {
	case *types.Var:
	case *types.Meta:
		inner := c.recOf(t)
		inner.num = inner.num || r.num
		inner.ord = inner.ord || r.ord
		inner.eq = inner.eq || r.eq
		inner.flo = inner.flo || r.flo
		inner.intg = inner.intg || r.intg
		inner.quiet = inner.quiet || r.quiet
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
	w, g := types.Chase(want), types.Chase(got)

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
	// A `Go.Any` parameter accepts every argument — boxing a value
	// into Go's empty interface is implicit at call sites
	// (spec 8.1.7). The check runs before the got-meta bind so an
	// `any` slot never captures an inference variable.
	if isGoAny(w) {
		return true
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

// mismatch reports the generic unification failure. A complaint
// whose type mentions a quiet meta is a consequence of the
// diagnostic that quieted it — e.g. `Result ?a ?e` minted on top of
// a shadowing `type Result` — so it stays silent instead of
// cascading an E0401 behind the E0415 that owns the operand.
func (c *checker) mismatch(sp diag.Span, want, got any) {
	if c.mentionsQuiet(want) || c.mentionsQuiet(got) {
		return
	}
	sp = c.pos(sp)
	name := metaNamer()
	var w string
	switch want := want.(type) {
	case types.Type:
		w = types.Display(want, name)
	case string:
		w = want
	default:
		panic("unreachable")
	}
	var g string
	switch got := got.(type) {
	case types.Type:
		g = types.Display(got, name)
	case string:
		g = got
	default:
		panic("unreachable")
	}
	d := diag.Error("E0401", fmt.Sprintf("type mismatch: expected %s, got %s", w, g)).
		At(sp).Set("expected", w).Set("actual", g)
	c.diags.Add(d)
}

// metaNamer returns a types.Display namer spelling each unbound
// meta `?1`, `?2`, ... in first-occurrence order. The mint order an
// internal ID encodes is solver state — meaningless and unstable in
// a message — while renumbering keeps the `?` marker for "a type
// inference could not determine" and preserves which unknowns
// coincide: a meta appearing twice keeps one spelling, distinct
// metas keep distinct ones. One diagnostic shares one namer across
// every type it renders.
func metaNamer() func(*types.Meta) string {
	names := map[*types.Meta]string{}
	return func(m *types.Meta) string {
		s, ok := names[m]
		if !ok {
			s = fmt.Sprintf("?%d", len(names)+1)
			names[m] = s
		}
		return s
	}
}

// mentionsQuiet reports whether t is a type spelling at least one
// unbound quiet meta — the mark of a recovery type a diagnostic
// already owns. Non-type arguments (a descriptive `want` string)
// never count.
func (c *checker) mentionsQuiet(t any) bool {
	tt, ok := t.(types.Type)
	if !ok {
		return false
	}
	found := false
	metasIn(tt, func(m *types.Meta) {
		if c.isQuiet(m) {
			found = true
		}
	})
	return found
}

// rigidVars collects the rigid type variables of t in first-occurrence
// order, deduplicated by ID — the order CallInfo.TypeArgs records an
// instantiation in.
func rigidVars(t types.Type) []*types.Var {
	var out []*types.Var
	seen := map[int]bool{}
	var walk func(t types.Type)
	walk = func(t types.Type) {
		switch t := types.Chase(t).(type) {
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

// instantiateSig is instantiate over a Signature: when the signature
// declares its own TypeParams (a generic Go callee) the returned type
// arguments follow that declared order — which may differ from
// first-occurrence order — so CallInfo.TypeArgs lines up with the
// emitted `pkg.F[T1, T2]` instantiation.
func (c *checker) instantiateSig(sig Signature, sp diag.Span) (types.Type, []types.Type) {
	if len(sig.TypeParams) == 0 {
		return c.instantiate(sig.Type, sp)
	}
	sub := types.Subst{}
	args := make([]types.Type, len(sig.TypeParams))
	for i, v := range sig.TypeParams {
		m := c.newMeta(sp, fmt.Sprintf("the type argument for `%s`", v))
		sub[v.ID] = m
		args[i] = m
	}
	return sub.Apply(sig.Type), args
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
			c.satisfies(e.r, d, c.pos(e.r.span))
			e.m.Bind(d)
		}
	}
	// With the defaults bound, member selections deferred on an
	// unknown receiver type can retry: `0.f` now sees the literal's
	// Int. The retry may mint metas (an instantiated method
	// signature), so rebuild the table for the reporting pass — the
	// new entries carry no defaults, so the default pass need not
	// run again.
	c.revisitSelects()
	c.revisitInterfaces()
	// The same post-default moment settles the pending constant-zero
	// divisors: their operand types are now concrete (or bound to a
	// default), so revisitDivs can tell integer divisions — always
	// invalid — from Float ones, which need a constant dividend.
	c.revisitDivs()
	// Conversion operands deferred on an unsolved meta — a numeric
	// literal's Int default — are concrete now too: revisitConvs
	// gives the Go.conv validation its second look (spec 8.4).
	c.revisitConvs()
	// Literal ranges judge last: every literal meta has solved or
	// defaulted, so the static type at each literal's own position is
	// final (spec 3.3; E0417).
	c.revisitLitRanges()
	if len(c.metas) != len(ms) {
		ms = ms[:0]
		for m, r := range c.metas {
			ms = append(ms, metaEntry{m, r})
		}
		sortEntries(ms)
	}
	// Second pass: report what is left. A meta marked quiet is a
	// recovery artifact — the diagnostic that created it already ran.
	for _, e := range ms {
		m, r := e.m, e.r
		sp := c.pos(r.span)
		if !m.Bound() {
			if r.quiet {
				continue
			}
			desc := r.desc
			if desc == "" {
				desc = "this type"
			}
			d := diag.Error("E0407",
				fmt.Sprintf("type annotation required: %s could not be determined", desc)).
				At(sp).Set("type", types.Display(m, metaNamer()))
			c.diags.Add(d)
			continue
		}
		if r.eq && c.hasFuncOrVar(m) {
			td := types.Display(m.Solution(), metaNamer())
			d := diag.Error("E0409",
				fmt.Sprintf("`==` is not defined on %s: the type contains a function or a type variable", td)).
				At(sp).Set("type", td)
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
	w, g := types.Chase(want), types.Chase(got)
	if _, ok := w.(*types.Meta); ok {
		return c.unify(w, g, sp)
	}
	if _, ok := g.(*types.Meta); ok {
		return c.unify(w, g, sp)
	}
	return types.Equal(w, g)
}
