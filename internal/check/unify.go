package check

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"slices"
	"strings"

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

	num    bool // constrained to a built-in numeric type
	ord    bool // constrained to an ordered type
	eq     bool // must not contain functions or type variables
	flo    bool // constrained to a floating-point type (Float literals)
	intg   bool // constrained to an integral type (the `%` operator)
	quiet  bool // another diagnostic already covers this meta — no E0407
	silent bool // a failed expectation covers its unsolvedness — no E0407,
	//   but unlike quiet a later mismatch that mentions it still reports
	dflt types.Type // default solution, or nil for "ask for an annotation"
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

// quietMetasIn silences every still-unbound meta whose creation site
// lies inside sp — the debris a dead speculative check left in the
// meta table. Metas minted elsewhere that only unified with the dead
// node's metas keep their own diagnostic behavior.
func (c *checker) quietMetasIn(sp diag.Span) {
	for m, r := range c.metas {
		if types.Chase(m) != m {
			continue
		}
		if r.span.File == sp.File && r.span.Start >= sp.Start && r.span.End <= sp.End {
			r.quiet = true
		}
	}
}

// markSilent flags t's record silent when t is an unbound meta — the
// weak form of markQuiet for metas inside a failed expectation: the
// mismatch the failed unify already reported covers the meta's
// unsolved state, so it must not pile on an E0407, but a later
// conform position that shares the want (a sibling if/match/select
// arm) still gets its own E0401. markQuiet is reserved for recovery
// metas a diagnostic owns outright — those silence complaints that
// mention them too.
func (c *checker) markSilent(t types.Type) {
	if m, ok := types.Chase(t).(*types.Meta); ok {
		c.recOf(m).silent = true
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
	dst.silent = dst.silent || src.silent
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
	// Named Go types participate in arithmetic/comparison by Go's
	// own rules (decision 2026-11-17-named-int-arith): the kind of
	// the underlying basic type — known only to the UnderlyingSource
	// oracle — decides admission, so `30 * time.Second` types and
	// `deadline - now` land on Duration without `Go.conv`.
	kind, isGoNamed := c.namedBasicKind(t)
	// A one-field record emits as the Go defined type `type X F`:
	// Go assigns untyped constants to X and admits X to the
	// arithmetic and comparison its underlying F allows, so the
	// constraint classes judge the peeled underlying — `Clamp64(u,
	// 0, 100)` types like Go does. The peeled field participates
	// itself: `{d: time.Duration}` answers Duration's basic kind,
	// and a field spelling a shadowed builtin (`{v: Int}` where the
	// file's `type Int` is a struct) is a user decl, not the
	// builtin its name spells.
	u := c.definedUnderlying(t)
	ukind, uIsGoNamed := c.namedBasicKind(u)
	uNum, uFlo, uIntg, uOrd := isNumeric(u), isFloat(u), isIntegral(u), isOrdered(u)
	if u != t {
		// The shadow judgment is scoped to the record's own file:
		// for a file-local record the field's `Int` resolves here,
		// where `type Int` makes the builtin unreachable; an
		// imported record spelled its fields in ITS package, where
		// the caller's `type Int` has no say.
		if wc, ok := types.Chase(t).(*types.Con); ok && wc.Qual == "" {
			if uc, ok := u.(*types.Con); ok && uc.Qual == "" && c.shadowedBuiltin(uc.Name) {
				uNum, uFlo, uIntg, uOrd = false, false, false, false
			}
		}
	}
	num := isNumeric(t) || uNum || (isGoNamed && (namedIntegral(kind) || namedFloat(kind))) ||
		(uIsGoNamed && (namedIntegral(ukind) || namedFloat(ukind)))
	flo := isFloat(t) || uFlo || (isGoNamed && namedFloat(kind)) ||
		(uIsGoNamed && namedFloat(ukind))
	intg := isIntegral(t) || uIntg || (isGoNamed && namedIntegral(kind)) ||
		(uIsGoNamed && namedIntegral(ukind))
	ord := isOrdered(t) || uOrd || (isGoNamed && (namedIntegral(kind) || namedFloat(kind))) ||
		(uIsGoNamed && (namedIntegral(ukind) || namedFloat(ukind)))
	if r.num && !num {
		c.mismatch(sp, "a numeric type", t)
		return false
	}
	if r.flo && !flo {
		c.mismatch(sp, "a floating-point type", t)
		return false
	}
	if r.intg && !intg {
		c.mismatch(sp, "an integral type", t)
		return false
	}
	if r.ord && !ord {
		c.mismatch(sp, "an ordered type", t)
		return false
	}
	// The eq judgment sees the emitted shape too: a phantom type
	// parameter (`type Count a = {n: Int}` — Go emits
	// `type Count[A] int64`, still comparable) peels away, while a
	// record field spelling a bare type parameter stays unpeeled
	// and flags as before.
	if r.eq && c.hasFuncOrVar(t) && (u == t || c.hasFuncOrVar(u)) {
		td := types.Display(t, metaNamer())
		d := diag.Error("E0409",
			fmt.Sprintf("`==` is not defined on %s: the type contains a function or a type variable", td)).
			At(sp).Set("type", td)
		c.diags.Add(d)
		return false
	}
	return true
}

// namedBasicKind returns the go/types basic kind of the underlying
// type of a named Go type — knowledge a *types.Go handle hides
// behind its NamedRef — via the UnderlyingSource oracle. ok is
// false for non-Go types, a missing oracle, or a non-basic
// underlying (struct, interface, ...).
func (c *checker) namedBasicKind(t types.Type) (gotypes.BasicKind, bool) {
	g, ok := types.Chase(t).(*types.Go)
	if !ok {
		return gotypes.Invalid, false
	}
	src, ok := c.src.(UnderlyingSource)
	if !ok {
		return gotypes.Invalid, false
	}
	return src.GoUnderlyingKind(g)
}

// namedIntegral reports whether k is an integral basic kind — the
// underlyings that make a named Go type admit `+ - * / %` and
// comparisons (`time.Duration` is int64).
func namedIntegral(k gotypes.BasicKind) bool {
	switch k {
	case gotypes.Int, gotypes.Int8, gotypes.Int16, gotypes.Int32, gotypes.Int64,
		gotypes.Uint, gotypes.Uint8, gotypes.Uint16, gotypes.Uint32, gotypes.Uint64,
		gotypes.Uintptr:
		return true
	default:
		return false
	}
}

// namedFloat reports whether k is a floating-point basic kind.
func namedFloat(k gotypes.BasicKind) bool {
	return k == gotypes.Float32 || k == gotypes.Float64
}

// isNumeric reports whether t is one of the built-in numeric types of
// spec section 3.1 the arithmetic operators overload over.
func isNumeric(t types.Type) bool {
	con, ok := t.(*types.Con)
	if !ok || con.Qual != "" {
		return false
	}
	switch con.Name {
	case "Int", "Int32", "Int64", "Uint", "Uint64", "Float":
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
	case "Int", "Int32", "Int64", "Uint", "Uint64":
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
	c      *checker
	seen   map[*typeInfo]bool
	bound  map[int]int
	sink   *[]*types.Meta
	peeled map[conKey]bool
}

// hasConKey reports whether t mentions the constructor key — the
// self-reference check that keeps a peel from growing the walk's
// type each step.
func hasConKey(t types.Type, key conKey) bool {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual == key.qual && t.Name == key.name {
			return true
		}
		return slices.ContainsFunc(t.Args, func(x types.Type) bool { return hasConKey(x, key) })
	case *types.Func:
		if hasConKey(t.Ret, key) {
			return true
		}
		return slices.ContainsFunc(t.Params, func(p types.FuncParam) bool { return hasConKey(p.Type, key) })
	case *types.Tuple:
		return slices.ContainsFunc(t.Elems, func(x types.Type) bool { return hasConKey(x, key) })
	case *types.Go:
		return slices.ContainsFunc(t.Args, func(x types.Type) bool { return hasConKey(x, key) })
	case *types.Var, *types.Meta:
		return false
	default:
		panic("unreachable")
	}
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
		// A defined-type record's emitted shape is its field —
		// `type Count a = {n: Int}` compares as `type Count[A]
		// int64`: the phantom `a` never reaches the emitted shape,
		// so the peel drops it from the walk the same way
		// satisfies' eq judgment does. A `{f: a}` record stays
		// unpeeled and its argument still flags. A peel whose field
		// still mentions the record (`{w: W (List a)}` →
		// `W (List Int)`) would grow the type each step — the
		// declaration walk's `seen` bounds it instead. `peeled`
		// bounds the longer cycle (`{f: List B}` / `{f: List A}`:
		// each peel names the other, no self-mention): a re-entry
		// falls back to the declaration walk.
		key := conKey{qual: t.Qual, name: t.Name}
		if !w.peeled[key] {
			if u := w.c.definedUnderlying(t); u != t && !hasConKey(u, key) {
				if w.peeled == nil {
					w.peeled = map[conKey]bool{}
				}
				w.peeled[key] = true
				defer delete(w.peeled, key)
				return w.has(u)
			}
		}
		// `Go.FuncN` is a Go func type spelled as a constructor —
		// Go funcs are not comparable, so `==` on one is E0409
		// the same as on a curried `Func`.
		if t.Qual == "Go" {
			if _, ok := goFuncTypeArity(t.Name); ok {
				return true
			}
		}
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
// sealed interface (comparable iff every variant's payload is —
// interface `==` on a non-comparable variant panics at runtime),
// a record a struct (comparable iff every field is), Option an
// interface over a struct box (comparable iff its element is),
// Result a flat struct, a declared Gom type variable an `any`
// parameter (not comparable). Opaque interfaces — GoError, Go.Any,
// named Go interfaces — keep Go's admission since the checker
// cannot enumerate their implementations (decision
// 2026-11-24-sum-comparable-panic).
// *types.Go handles delegate to the ComparableSource oracle —
// `types.Comparable` on the named type behind the handle. Opaque
// shapes the checker cannot see (abstract or foreign declared types)
// answer false rather than guess: a rejected call is recoverable, a
// generated file that fails `go build` is not.
func (c *checker) goReprComparable(t types.Type) bool {
	w := &cmpWalk{c: c, memo: map[cmpKey]bool{}}
	return w.has(t)
}

// cmpWalk is the comparable-representation traversal behind
// goReprComparable — it mirrors eqWalk's declared-type expansion.
type cmpWalk struct {
	c     *checker
	seen  []recordVisit
	steps int
	nodes int
	memo  map[cmpKey]bool
}

// cmpNodeBudget bounds total node visits — a recursion that swaps
// or compounds its arguments (`x: Bad (Tuple a a)`) grows the
// traversal faster than the expansion step limit notices.
const cmpNodeBudget = 4096

// cmpKey keys the memo of finite-shape verdicts: the rendered type,
// its spelling scope, and the live substitution signature. A tuple
// is the only shape whose children are re-walked without a Con
// indirection — repeated tuple fields would otherwise re-spend the
// node budget on every occurrence (a payload that nests one wide
// tuple four deep walks thousands of identical nodes).
type cmpKey struct {
	typ     string
	foreign bool
	sub     string
}

// subSig renders a substitution signature for the memo key — the
// same spelled tuple can judge differently under different
// bindings, so bindings join the key.
func subSig(sub map[int]subArg) string {
	if len(sub) == 0 {
		return ""
	}
	ids := make([]int, 0, len(sub))
	for id := range sub {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var b strings.Builder
	for _, id := range ids {
		a := sub[id]
		fmt.Fprintf(&b, "#%d=%s:%v;", id, a.typ, a.foreign)
	}
	return b.String()
}

// subArg is a lazily substituted type argument paired with the
// declaration scope it was spelled in — an argument written inside
// an imported declaration keeps that package's meaning for its
// unqualified names even after substitution.
type subArg struct {
	typ     types.Type
	foreign bool
}

// has reports whether t's Go representation is comparable.
func (w *cmpWalk) has(t types.Type) bool {
	return w.hasS(t, nil, false)
}

// hasS judges t with the declaration scope spelled out: sub
// substitutes a declaration's type parameters lazily, so an
// argument's own spelling keeps the scope of the site that wrote
// it, and foreign marks the fields of a declaration loaded through
// an import — its unqualified names resolve to the imported
// package's builtins, never to the caller's decls. A caller `type
// List` must not expand under a foreign field's `List` spelling:
// had the foreign package declared List itself, the field would
// carry the qualified spelling.
func (w *cmpWalk) hasS(t types.Type, sub map[int]subArg, foreign bool) bool {
	if w.nodes >= cmpNodeBudget {
		return false
	}
	w.nodes++
	switch t := types.Chase(t).(type) {
	case *types.Var:
		// A Gom type parameter instantiates as `any` — it cannot
		// satisfy `comparable` (spec section 8.5's boundary). An
		// argument the substitution supplies keeps the scope of the
		// site that spelled it.
		if a, ok := sub[t.ID]; ok {
			return w.hasS(a.typ, nil, a.foreign)
		}
		return false
	case *types.Meta:
		// An unbound meta is undecided — finalize reports E0407.
		return true
	case *types.Func:
		return false
	case *types.Tuple:
		// rt.TupleN[A, ...] is a struct — comparable iff every
		// element is. The verdict is memoized: a spelled tuple
		// repeated across fields is one subtree, and walking it once
		// per occurrence would exhaust the node budget on finite
		// types.
		k := cmpKey{typ: t.String(), foreign: foreign, sub: subSig(sub)}
		if r, ok := w.memo[k]; ok {
			return r
		}
		r := true
		for _, e := range t.Elems {
			if !w.hasS(e, sub, foreign) {
				r = false
				break
			}
		}
		w.memo[k] = r
		return r
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
				// Go.Int (int) and Go.Any (any) are comparable;
				// a Go.FuncN spelling is a func type, which Go
				// makes non-comparable.
				if _, isFn := goFuncTypeArity(t.Name); isFn {
					return false
				}
				return true
			}
		}
		// The caller's shadowed builtins apply only in caller
		// scope — a foreign declaration's unqualified spelling is
		// that package's builtin.
		if t.Qual == "" && (foreign || !w.c.shadowedBuiltin(t.Name)) {
			switch t.Name {
			case "Unit", "Bool", "Int", "Int32", "Int64", "Uint", "Uint64", "Float",
				"Char", "Byte", "String", "GoError":
				// bool, the int widths, byte, float64, rune,
				// string, struct{} and the error interface — all
				// comparable Go representations.
				return true
			case "List", "Array":
				// list.List[A] is a named slice; Array renders []A.
				return false
			case "Result":
				// result.Result[T, E] is a struct — comparable iff
				// its payload fields are.
				for _, a := range t.Args {
					if !w.hasS(a, sub, foreign) {
						return false
					}
				}
				return true
			case "Task":
				// rt.Task[T] is a struct{ chan, *taskBox[T] } —
				// comparing handles compares the channel and the
				// pointer, never the payload, so every Task is
				// comparable.
				return true
			case "Option":
				// option.Option[A] is an interface over the struct
				// box option.Option_Some[A] — comparable iff the
				// element is, or `==` on two Somes panics.
				if len(t.Args) == 1 {
					return w.hasS(t.Args[0], sub, foreign)
				}
				return true
			case "Ref", "Ptr", "Chan", "RecvChan", "SendChan", "Scope":
				// Ref and Ptr are pointers; channels are channels —
				// all comparable.
				return true
			}
		}
		// A declared type: sealed-interface sums are comparable iff
		// every variant's payload is, records iff every field is,
		// aliases expand.
		if w.expandable(t, foreign) {
			ti := w.c.tyCons[conKey{qual: t.Qual, name: t.Name}]
			return w.expand(t, sub, foreign, ti, func(nsub map[int]subArg, nforeign bool) bool {
				switch ti.kind {
				case kindSum:
					// The sealed-interface repr is comparable in Go,
					// but `==` on a variant whose payload is not
					// panics at runtime — fold the record rule over
					// every variant's fields (decision
					// 2026-11-24-sum-comparable-panic).
					for _, v := range ti.variants {
						for _, f := range v.fields {
							if !w.hasS(f.typ, nsub, nforeign) {
								return false
							}
						}
					}
					return true
				case kindRecord:
					for _, f := range ti.fields {
						if !w.hasS(f.typ, nsub, nforeign) {
							return false
						}
					}
					return true
				case kindAlias:
					if ti.aliasRes {
						return w.hasS(ti.alias, nsub, nforeign)
					}
				}
				return false
			})
		}
		// An unqualified builtin the file shadows (tyCons has the
		// declaration — expandable caught it), an abstract type, or
		// a foreign nominal type the import did not declare: the
		// repr is not visible here.
		return false
	default:
		panic("unreachable")
	}
}

// expandable reports whether the constructor names a declared
// record, sum or alias whose declaration contributes to the
// comparable judgment — a local declaration or an imported one
// registerForeignTypes loaded (its fields arrive requalified). An
// unqualified spelling inside a foreign declaration never names a
// caller's declaration.
func (w *cmpWalk) expandable(t *types.Con, foreign bool) bool {
	if t.Qual == "" && foreign {
		return false
	}
	ti, ok := w.c.tyCons[conKey{qual: t.Qual, name: t.Name}]
	return ok && ti.kind != kindAbstract
}

// expand runs f under the declaration's type-parameter
// substitution, bounded like eqWalk.expand — but the revisit answer
// is coinductive: `type T = | N (x: T)` is comparable exactly when
// its other fields are (the recursive field emits the same sealed
// interface, whose `==` safety is the question being decided), so a
// revisit answers true. Only an exhausted budget answers false —
// the "cannot prove comparable" value. outer substitutes t's
// arguments first, so a recursive field's raw `Var` keeps the
// instantiation the caller had (`Tree Int` reaches its `Tree a`
// children as `Tree Int`, not as the unbound `a`). Each new binding
// records the scope it was spelled in: argForeign is the scope of
// t's own spelling site, and a bare Var argument inherits its
// outer binding's scope — `x: Inner a` inside an imported decl
// keeps whatever scope `a` already had, while `Inner (List Int)`
// there records foreign. The fold also receives the declaration's
// scope: true when ti is an imported decl, so its unqualified
// field spellings keep their own package's meaning.
func (w *cmpWalk) expand(t *types.Con, outer map[int]subArg, argForeign bool, ti *typeInfo, f func(map[int]subArg, bool) bool) bool {
	osub := types.Subst{}
	for id, a := range outer {
		osub[id] = a.typ
	}
	args := make([]types.Type, len(t.Args))
	for i, a := range t.Args {
		args[i] = osub.Apply(a)
	}
	v := recordVisit{ti: ti, args: args}
	for _, s := range w.seen {
		if s.ti == ti && sameTypes(s.args, v.args) {
			return true
		}
	}
	if w.steps >= recordCycleLimit {
		return false
	}
	w.seen = append(w.seen, v)
	w.steps++
	defer func() { w.seen = w.seen[:len(w.seen)-1] }()
	sub := map[int]subArg{}
	for i, p := range ti.params {
		if i < len(args) {
			sc := argForeign
			if v, ok := t.Args[i].(*types.Var); ok {
				if b, ok := outer[v.ID]; ok {
					sc = b.foreign
				}
			}
			sub[p.ID] = subArg{typ: args[i], foreign: sc}
		}
	}
	return f(sub, ti.con.Qual != "")
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
	if r != nil && (r.num || r.ord || r.eq || r.flo || r.intg || r.quiet || r.silent) {
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
		inner.silent = inner.silent || r.silent
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

// bindWantMeta solves the wanted side's unbound meta against the
// (already chased) got side — the shared head of unify and unifySub.
// Two metas merge keeping the expected side canonical so descriptions
// bias toward "what was required"; a concrete got binds outright.
func (c *checker) bindWantMeta(m *types.Meta, g types.Type, sp diag.Span) bool {
	if gm, ok := g.(*types.Meta); ok && gm == m {
		return true
	}
	if gm, ok := g.(*types.Meta); ok {
		mergeRec(c.recOf(m), c.recOf(gm))
		delete(c.metas, gm)
		gm.Bind(m)
		return true
	}
	return c.bind(m, g, sp)
}

// unify makes two types equal, solving metas on either side. It
// reports a type mismatch (E0401) at sp and returns false on failure.
// The expected type is the first argument so diagnostics read
// "expected X, got Y".
//
// unify is the *value-slot* judgment — the expression reaching the
// slot carries its own type. Inside a composite type's parameters
// the judgment changes to unifySub's: a `Container Go.Any` slot is
// invariant, not accept-everything.
func (c *checker) unify(want, got types.Type, sp diag.Span) bool {
	w, g := types.Chase(want), types.Chase(got)

	// Metas first: binding beats every structural rule.
	if m, ok := w.(*types.Meta); ok {
		return c.bindWantMeta(m, g, sp)
	}
	// A `Go.Any` parameter accepts every argument — boxing a value
	// into Go's empty interface is implicit at call sites
	// (spec 8.1.7). The check runs before the got-meta bind so an
	// `any` slot never captures an inference variable — except a
	// `Go.nil ()` literal's own meta, for which `any` is the only
	// concrete reading (`rt.Nil[any]()`); leaving it unsolved just
	// demands an annotation that could not pin anything better.
	if isGoAny(w) {
		if m, ok := g.(*types.Meta); ok && c.isNilMeta(m) {
			return c.bind(m, w, sp)
		}
		return true
	}
	// A method-less named func type and its `Go.FuncN` spelling are
	// the same Go value at assignable positions — unify pairs them
	// (the handle is the annotation's spelling, FuncN the
	// signature-boundary one). Both sides expand only when the
	// other is a FuncN con — two different handles stay different
	// (`F` is not assignable to `G` in Go either).
	if isFuncNCon(g) {
		if fn, ok := c.funcNOfNamed(w); ok {
			return c.unifySub(fn, g, sp)
		}
	}
	if isFuncNCon(w) {
		if fn, ok := c.funcNOfNamed(g); ok {
			return c.unifySub(w, fn, sp)
		}
	}
	return c.unifySub(w, g, sp)
}

// isFuncNCon reports whether t is a `Go.FuncN` constructor type.
func isFuncNCon(t types.Type) bool {
	cn, ok := t.(*types.Con)
	if !ok || cn.Qual != "Go" {
		return false
	}
	_, ok = goFuncTypeArity(cn.Name)
	return ok
}

// unifySub unifies inside the parameters of a composite type — a Con
// or Tuple or Go argument, a Func parameter or result — where a
// `Go.Any` slot *selects the instantiation* rather than accepting
// every payload: `Option Go.Any` demands `Option[any]` (Go generics
// are invariant — `Option[int64]` is not `Option[any]`). An unsolved
// meta on the got side binds to `Go.Any` — a constructor under this
// expectation (`Ok v`, `Some v`, `None`, `[e]`) builds the
// `any`-element composite directly — while a concrete got must equal
// `Go.Any` itself. The `any`-in-element-position rule is what
// distinguishes an `any`-parameterized container from the
// value-level accept of spec 8.1.7, where `any(x)` boxes the whole
// value whatever x's type is.
func (c *checker) unifySub(want, got types.Type, sp diag.Span) bool {
	w, g := types.Chase(want), types.Chase(got)

	if m, ok := w.(*types.Meta); ok {
		return c.bindWantMeta(m, g, sp)
	}
	if isGoAny(w) {
		if isGoAny(g) {
			return true
		}
		if m, ok := g.(*types.Meta); ok {
			return c.bind(m, w, sp)
		}
		c.mismatch(sp, w, g)
		return false
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
			if !c.unifySub(w.Args[i], gc.Args[i], sp) {
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
			if !c.unifySub(w.Elems[i], gt.Elems[i], sp) {
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
			if !c.unifySub(w.Args[i], gg.Args[i], sp) {
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

// containsGoAny reports whether `Go.Any` occurs anywhere inside t —
// a `Go.Any` nested in an expected constructor argument still
// *selects* the ctor's instantiation: `Some (Some 42)` under
// `Option (Option Go.Any)` pins the outer ctor's meta to
// `Option Go.Any` so the inner apply's own pin chains through and
// the payload boxes into the `any` element (`checkApply`). The walk
// chases metas, so an already-bound meta's content counts too.
func containsGoAny(t types.Type) bool {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if isGoAny(t) {
			return true
		}
		for _, a := range t.Args {
			if containsGoAny(a) {
				return true
			}
		}
	case *types.Tuple:
		for _, el := range t.Elems {
			if containsGoAny(el) {
				return true
			}
		}
	case *types.Func:
		for _, p := range t.Params {
			if containsGoAny(p.Type) {
				return true
			}
		}
		return containsGoAny(t.Ret)
	case *types.Go:
		for _, a := range t.Args {
			if containsGoAny(a) {
				return true
			}
		}
	case *types.Meta, *types.Var:
		// Chased already: a Meta reaching here is unbound and
		// contains nothing; a Var is a rigid type variable.
		return false
	default:
		panic("unreachable")
	}
	return false
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
	// Parameters pair under the structural judgment — two function
	// *types* are equal only parameter-for-parameter. A `Go.Any`
	// parameter in the expected type does not accept a function
	// whose parameter is a different type: `any -> R` and
	// `String -> R` emit different Go func types and Go generics
	// are invariant. The "callee accepts this argument" reading —
	// an `any` parameter taking a `String` argument — is an
	// *application* check, not type equality; pipeCall, overApply,
	// `|?>` and the deferred replay perform it per parameter via
	// conformCallee/unify (spec 8.1.7).
	for len(wp) > 0 && len(gp) > 0 {
		a, b := wp[0], gp[0]
		if a.Label == "" && b.Label == "" {
			if !c.unifySub(a.Type, b.Type, sp) {
				return false
			}
			wp, gp = wp[1:], gp[1:]
			continue
		}
		// unlabeled→labeled introduction (labeled-arg-relaxation
		// 案A): when the expected side declares a labeled parameter
		// and the produced side has no labels anywhere in its tail,
		// the produced parameter borrows the expected label by
		// position — `Int -> Int` unifies with `~x: Int -> Int`.
		// A produced side that declares labels of its own keeps the
		// strict run-pairing below: lending `x` into
		// `Int -> ~y: Int -> Int` for `~x: Int -> ~y: Int -> Int`
		// would silently reorder the contract it already names.
		if a.Label != "" && b.Label == "" && !c.noLabelIntro &&
			allPositional(gp) {
			if !c.unifySub(a.Type, b.Type, sp) {
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
		// their side's parameter stream as leftovers. Matched
		// partners must also appear in the same order — the Go ABI
		// is positional, so `~a ~b` produced for `~b ~a` expected
		// would swap the arguments at the call. Each iteration
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
		lastK, ordered := -1, true
		for _, p := range ra {
			k, ok := byLabel[p.Label]
			if !ok {
				aLeft = append(aLeft, p)
				continue
			}
			matched[k] = true
			if k <= lastK {
				ordered = false
			}
			lastK = k
			if !c.unifySub(p.Type, rb[k].Type, sp) {
				return false
			}
		}
		if !ordered {
			// Same labels in a different run order — the positional
			// Go call would swap the arguments, so types.Equal (and
			// Core validation downstream) rejects these as different
			// types. Unify must agree rather than accept and leave
			// the rejection to an internal error.
			c.mismatch(sp, w, g)
			return false
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
		return c.unifySub(&types.Func{Params: wp, Ret: wr}, gr, sp)
	case len(gp) > 0:
		return c.unifySub(wr, &types.Func{Params: gp, Ret: gr}, sp)
	default:
		return c.unifySub(wr, gr, sp)
	}
}

// lentLabels reports whether the expected type lent a label to the
// produced one — positions where want names a parameter that got
// leaves unlabeled, at any curried level. Only meaningful after a
// successful unify: a labeled↔unlabeled pairing can only have
// passed through the introduction rule.
func lentLabels(want, got types.Type) bool {
	wf, _ := types.Chase(want).(*types.Func)
	gf, _ := types.Chase(got).(*types.Func)
	if wf == nil || gf == nil {
		return false
	}
	for i, wp := range wf.Params {
		if i >= len(gf.Params) {
			break
		}
		if wp.Label != "" && gf.Params[i].Label == "" {
			return true
		}
	}
	return lentLabels(wf.Ret, gf.Ret)
}

// lendFuncLabels copies the labels the expected type lent through
// introduction onto a private copy of the produced function signature —
// annotation to binding, in positional order across curried levels. The
// input may share its nested function type with another binding, so the
// label lending itself must not mutate that shape.
func lendFuncLabels(dst, src types.Type) types.Type {
	sp, _ := flattenFunc(src)
	out := copyFuncSpine(dst)
	pos := 0
	for cursor := out; ; {
		df, ok := types.Chase(cursor).(*types.Func)
		if !ok {
			return out
		}
		for i := range df.Params {
			if pos >= len(sp) {
				return out
			}
			if df.Params[i].Label == "" {
				df.Params[i].Label = sp[pos].Label
			}
			pos++
		}
		cursor = df.Ret
	}
}

func copyFuncSpine(t types.Type) types.Type {
	f, ok := types.Chase(t).(*types.Func)
	if !ok {
		return t
	}
	params := append([]types.FuncParam(nil), f.Params...)
	return &types.Func{Params: params, Ret: copyFuncSpine(f.Ret)}
}

// allPositional reports whether every parameter in ps is unlabeled —
// the produced side borrows expected labels only when it names none
// of its own (unlabeled→labeled introduction).
func allPositional(ps []types.FuncParam) bool {
	for _, p := range ps {
		if p.Label != "" {
			return false
		}
	}
	return true
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
	// Constraint failures name a requirement ("a numeric type") rather
	// than the annotated type, so only concrete type expectations are
	// provenance-labelled.
	if _, ok := want.(types.Type); ok && !c.expectedFrom.IsZero() {
		d = d.AddLabel(c.expectedFrom, "expected type declared here")
	}
	// A function type still carrying labeled parameters is the shape
	// of a partial application that left labels unfilled — name them
	// so the diagnostic points at the missing arguments rather than
	// only at the residual type. The actual side is the common case
	// (`expected Unit, got ~amount: Int -> Unit`); the expected side
	// carries labels when extra positional arguments settled labeled
	// parameters out of order. When both sides declare the same
	// labels the value is no partial call at all — it is an order
	// mismatch, and the positional Go call would swap the arguments.
	gl, wl := unfilledLabels(got), unfilledLabels(want)
	if len(gl) > 0 && len(wl) > 0 && sameLabels(gl, wl) {
		if !slices.Equal(gl, wl) {
			d = d.AddNote(fmt.Sprintf("the labeled parameters %s are in a different order — the generated Go call is positional, so the arguments would swap", joinLabels(gl)))
		} else {
			// The label sequences agree, so the failure is the
			// interleave: a label moved across a positional
			// parameter. Saying "different order" would point at
			// the wrong axis.
			d = d.AddNote(fmt.Sprintf("the labeled parameters %s sit at a different position among the positional parameters — the generated Go call is positional, so the arguments would swap", joinLabels(gl)))
		}
		d = d.Set("unfilled_labels", gl)
	} else if len(gl) > 0 {
		d = d.AddNote(fmt.Sprintf("the value is a partially applied call still awaiting %s", joinLabels(gl))).
			Set("unfilled_labels", gl)
	} else if ls := wl; len(ls) > 0 {
		d = d.AddNote(fmt.Sprintf("the expected function type still declares %s — positional arguments may have filled labeled parameters out of order", joinLabels(ls))).
			Set("unfilled_labels", ls)
	}
	c.diags.Add(d)
}

// unfilledLabels lists the labeled parameters of t when it is a
// function type — the labels a partial application still owes.
// A signature returning a function keeps that function nested in
// Ret (fnSig does not flatten), so the walk continues through the
// Ret chain. Non-type arguments never qualify.
func unfilledLabels(t any) []string {
	tt, ok := t.(types.Type)
	if !ok {
		return nil
	}
	f, ok := types.Chase(tt).(*types.Func)
	if !ok {
		return nil
	}
	var ls []string
	for {
		for _, p := range f.Params {
			if p.Label != "" {
				ls = append(ls, p.Label)
			}
		}
		nf, ok := types.Chase(f.Ret).(*types.Func)
		if !ok {
			return ls
		}
		f = nf
	}
}

// sameLabels reports whether two label lists hold the same labels
// in any order — the shape of an order mismatch rather than a
// partial application.
func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as, bs := slices.Clone(a), slices.Clone(b)
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}

// joinLabels renders `~x:`, `~y:` for a diagnostic note.
func joinLabels(ls []string) string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = "~" + l + ":"
	}
	return strings.Join(out, ", ")
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
	// A structural constraint widens the parameter's own surface to
	// its repr shape: `s S` where `S ~[]E` checks arguments as
	// `Go.Slice ?E`. A `List` argument then takes its boundary copy
	// — required, since a Go callee may mutate `s` and must not
	// reach the List's backing — and a constraint-only operand
	// (`S ~[]E` binding `E`) solves through the ordinary argument
	// conformance, Go's constraint-type-inference step.
	for i := range sig.TypeParams {
		if i >= len(sig.GoConstraints) {
			break
		}
		m, ok := args[i].(*types.Meta)
		if !ok {
			continue
		}
		switch gc := sig.GoConstraints[i]; gc.Kind {
		case GoConstraintSlice:
			// The operand must not be the constrained parameter
			// itself (`S ~[]S`) — binding the meta to a shape that
			// contains itself makes every later traversal loop.
			if gc.Elem >= 0 && gc.Elem < len(args) && gc.Elem != i {
				m.Bind(types.GoSlice(args[gc.Elem]))
			}
		case GoConstraintMap:
			if gc.Key >= 0 && gc.Key < len(args) && gc.Key != i &&
				gc.Val >= 0 && gc.Val < len(args) && gc.Val != i {
				m.Bind(types.GoMap(args[gc.Key], args[gc.Val]))
			}
		case GoConstraintAny, GoConstraintComparable, GoConstraintUnion:
		default:
			panic("unreachable")
		}
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

// applyDefaults binds every unbound meta that has a default —
// literal roots and the like — reporting the constraint the
// default satisfies. Returns how many bound.
func (c *checker) applyDefaults() int {
	var ms []metaEntry
	for m, r := range c.metas {
		ms = append(ms, metaEntry{m, r})
	}
	sortEntries(ms)
	bound := 0
	for _, e := range ms {
		if e.m.Bound() {
			continue
		}
		if d := defaultOf(e.r); d != nil {
			c.satisfies(e.r, d, c.pos(e.r.span))
			e.m.Bind(d)
			bound++
		}
	}
	return bound
}

func (c *checker) finalize() {
	// First pass: defaults, interleaved with the deferred-selection
	// retry. Binding one may chase-solve others. A default that
	// violates the meta's own constraints (a `%` operand defaulting
	// to Float) still binds — satisfies already reported the
	// constraint failure once, and an unbound leftover would only add
	// a redundant E0407.
	//
	// With the defaults bound, member selections deferred on an
	// unknown receiver type can retry: `0.f` sees the literal's Int.
	// Defaults must wait for a stable round, though: a literal's
	// real constraint can still be sitting in deferred work (a
	// field-write RHS re-checked under its field's signature), and
	// binding Int before that re-check runs pins the meta early —
	// the later `Int64` constraint then rejects a `let n = 1` the
	// eager path would have accepted. So each round drains members,
	// replays and fills first; only a round that changed nothing
	// defaults, and the loop repeats because a default can enable
	// the next round's resolutions.
	for {
		nMetas := len(c.metas)
		c.revisitSelects()
		replayed := c.replayResolvedApplies()
		// A deferred RHS may itself pin the receiver it sits on
		// (`b.N = readN b`): fill pending values once, then let the
		// member retry — a resolved member re-checks the value under
		// its real want on the next round.
		filled := c.finishAssigns()
		filled = c.fillIters(false) || filled
		if len(c.metas) != nMetas || replayed != 0 || filled {
			continue
		}
		if c.applyDefaults() == 0 && !c.fillIters(true) {
			break
		}
	}
	// Field writes deferred on a receiver that never settled keep
	// the RHS their nil-want fill produced — the member's own
	// diagnostic covers the failure, the node just needs to be
	// complete.
	// Applications deferred on an unsettled member still need a
	// final CallInfo. The stages rank by how reversible they are:
	// settled members replay first — a replay's argument
	// unifications can pin the receiver another apply or selection
	// waits on — then defaults (still delayed past every resolver,
	// as above), and only a fully quiet round commits the rest to
	// the Gom convention. Committing early pins the member's
	// standing meta to a Gom shape (`Unit -> ?r`): a member
	// resolving later would both lose its Go convention and
	// mis-unify.
	for {
		nMetas := len(c.metas)
		replayed := c.replayResolvedApplies()
		c.revisitSelects()
		filled := c.finishAssigns()
		filled = c.fillIters(false) || filled
		if replayed != 0 || len(c.metas) != nMetas || filled {
			continue
		}
		if c.applyDefaults() != 0 || c.fillIters(true) {
			continue
		}
		if c.replayUnresolvedApplies() == 0 {
			break
		}
	}
	// Iterables deferred on pending work (`for v in f a` with `f` a
	// member alias) settle now — every call has replayed or committed,
	// so each iterable's meta carries its real shape (or stays unbound
	// and takes the List default inside finishIters).
	c.finishIters()
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
	var ms []metaEntry
	for m, r := range c.metas {
		ms = append(ms, metaEntry{m, r})
	}
	sortEntries(ms)
	// Second pass: report what is left. A meta marked quiet is a
	// recovery artifact — the diagnostic that created it already ran.
	for _, e := range ms {
		m, r := e.m, e.r
		sp := c.pos(r.span)
		if !m.Bound() {
			if r.quiet || r.silent {
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

// funcNOfNamed forwards the handle→FuncN equivalence to the first
// signature source that can answer it (see FuncNSource).
func (c *checker) funcNOfNamed(t types.Type) (types.Type, bool) {
	if src, ok := c.src.(FuncNSource); ok {
		return src.FuncNOfNamed(t)
	}
	return nil, false
}
