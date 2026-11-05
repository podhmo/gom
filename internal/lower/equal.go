package lower

import (
	"fmt"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// This file turns the checker's `==` judgment into the comparison
// recipe a core.Equal carries — and mints the generated comparators
// (core.EqDecl) composite types compare through.
//
// eqKindOf classifies the operand type with the same
// declaration-aware walk the checker's eq constraint applied:
// local records and ADTs expand their fields and payloads, the
// builtin constructors read their Gom-level contract, and Go-view
// shapes (named Go types, Task, Go.Any, Func, unresolved metas)
// fall to the rt.Equal fallback. Everything a composite type needs
// — a loop, a type switch, a tag read — is a statement, so its
// recipe lives on a generated function; operand positions always
// hold a leaf recipe (==, rt.Equal, the error comparison, or a
// comparator call).

// eqKind classifies what `==` on one operand type renders as.
type eqKind int

const (
	// eqNative — Go's `==` both compiles and carries Gom's
	// structural semantics: scalars, identity references,
	// tuples/records/Results and ADTs built entirely from them
	// (struct == compares fields natively; sealed interfaces
	// compare (variant, value) dynamically — the same reading).
	eqNative eqKind = iota + 1
	// eqRuntime — the rt.Equal fallback: named Go types (opaque
	// layout), Task, Go.Any, and the unreachable-in-valid-code
	// leaves (Func, Var, Meta) the checker rejects with E0409.
	eqRuntime
	// eqError — GoError's nil-aware message-text comparison.
	eqError
	// eqDecl — a generated comparator: lists, slices and maps,
	// tuples, records, ADTs and Results containing anything the
	// native recipe cannot carry.
	eqDecl
)

// eqClass is the per-eqPlan classification walk — a seen set for
// cycles through interfaces and a step budget for generic
// expansion, mirroring the checker's recordCycle guard.
type eqClass struct {
	seen  map[string]bool
	steps int
}

// eqWalkLimit bounds the distinct instantiated types one
// all-native classification may visit — a pathological generic
// expansion (`type W a = {next: W (W a)}`) is rejected upstream by
// E0413's own limit, so this is a defensive bound only.
const eqWalkLimit = 256

// eqExpr lowers `x == y` / `x != y` to a core.Equal — the recipe
// comes from the operands' shared static type, already unified by
// the checker.
func (l *lowerer) eqExpr(s *seq, e *check.Binary) core.Expr {
	x := l.atom(s, e.X)
	y := l.atom(s, e.Y)
	// Two Float literals would emit a Go constant comparison — its
	// exact-rational fold can equate literals a single float64 also
	// equates, but diverges from the per-operation reading whenever
	// the literal values merge after rounding
	// (litFoldDiverges — decision
	// 2026-11-05-numeric-arithmetic-per-operation).
	if l.litFoldDiverges(e.Op, e.X.Type(), x, y) {
		x = l.bindAtom(s, e.X.Span(), x)
	}
	return &core.Equal{
		Base: baseOf(e),
		Neg:  e.Op == syntax.BinNe,
		X:    x,
		Y:    y,
		How:  l.eqPlan(e.Span(), e.X.Type()),
	}
}

// eqDeclLimit bounds the comparators one file may mint. Recursive
// types dedup to a finite set through the type-keyed map, but an
// expanding self-application — `type W a = {next: List (W (W a))}`
// mints a fresh instantiated type at every level and no finite
// comparator set covers it: the cap reports E0906 at the `==` site
// rather than minting forever.
const eqDeclLimit = 128

// eqPlan returns the leaf recipe comparing two values of type t —
// the recipe an Equal node's How or a nested field of a composite
// plan carries.
func (l *lowerer) eqPlan(sp diag.Span, t types.Type) *core.EqPlan {
	w := &eqClass{seen: map[string]bool{}}
	switch l.eqKindOf(w, t) {
	case eqNative:
		return &core.EqPlan{Kind: core.EqNative}
	case eqError:
		return &core.EqPlan{Kind: core.EqError}
	case eqDecl:
		if d := l.cmpDecl(sp, t); d != nil {
			return &core.EqPlan{Kind: core.EqCall, Fn: d}
		}
		// The mint budget ran out — degrade to the runtime recipe
		// so the plan stays well-formed; the diagnostic the
		// overflow reported fails the build before this recipe is
		// ever rendered.
		l.eqOverflow(sp, t)
		return &core.EqPlan{Kind: core.EqRuntime}
	default:
		return &core.EqPlan{Kind: core.EqRuntime}
	}
}

// eqOverflow reports the comparator-minting budget overflow — once
// per file, at the `==` site every nested plan shares. Degrading a
// deep operand pair to rt.Equal would compile but silently
// reintroduce the reflect.DeepEqual reading `==` exists to escape
// (its same-backing-store shortcut answers [NaN] == [NaN] true), so
// the honest answer is an error at the comparison site.
func (l *lowerer) eqOverflow(sp diag.Span, t types.Type) {
	if l.eqOver {
		return
	}
	l.eqOver = true
	l.diags = append(l.diags,
		diag.Error(codeEqBudget,
			fmt.Sprintf("`==` mints more structural comparators than the %d limit — the operand type's expansion does not converge", eqDeclLimit)).
			At(sp).Set("type", t.String()))
}

// cmpDecl returns the generated comparator for the composite type
// t — minting it (and registering it for emission) on first use.
// Registration precedes plan building so a recursive type's nested
// recipe resolves to its own in-progress comparator. A nil answer
// means the eqDeclLimit budget ran out — the caller reports the
// overflow and degrades the plan.
func (l *lowerer) cmpDecl(sp diag.Span, t types.Type) *core.EqDecl {
	if l.eqDecls == nil {
		l.eqDecls = map[string]*core.EqDecl{}
	}
	t = types.Chase(t)
	key := t.String()
	if d, ok := l.eqDecls[key]; ok {
		return d
	}
	if len(l.eqDecls) >= eqDeclLimit {
		return nil
	}
	l.eqSeq++
	d := &core.EqDecl{
		Sp:  sp,
		Sym: &resolve.Symbol{Name: fmt.Sprintf("gomEq%d_%s", l.eqSeq, l.eqMark), Kind: resolve.KindFunc},
		Typ: t,
	}
	l.eqDecls[key] = d
	l.eqOrder = append(l.eqOrder, d)
	d.Plan = l.eqDeclPlan(sp, t)
	return d
}

// eqKindOf reports which comparison recipe operand type t needs.
// Local declarations (typeD) shadow the builtin table, which
// shadows the foreign oracle — the same precedence the checker
// applies.
func (l *lowerer) eqKindOf(w *eqClass, t types.Type) eqKind {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual == "Go" {
			switch t.Name {
			case "Int":
				return eqNative
			case "Slice", "Map":
				return eqDecl
			default:
				return eqRuntime
			}
		}
		key := t.String()
		if w.seen[key] {
			// A revisit is only reachable through a sum type's
			// interface indirection — a shape Go's `==` recurses
			// through at runtime. Answer native; the cycle adds
			// no non-native field on this path.
			return eqNative
		}
		if w.steps >= eqWalkLimit {
			return eqDecl
		}
		if t.Qual == "" {
			if d, ok := l.typeD[conKey(t)]; ok {
				w.seen[key] = true
				w.steps++
				return l.eqTypeKind(w, t, d)
			}
			switch t.Name {
			case "Unit", "Bool", "Int", "Int32", "Int64", "Float",
				"Char", "String",
				"Ref", "Ptr", "Chan", "RecvChan", "SendChan":
				return eqNative
			case "GoError":
				return eqError
			case "List", "Array":
				// Slices never compile under `==`.
				return eqDecl
			case "Option", "Result":
				w.seen[key] = true
				w.steps++
				return l.eqVariantsKind(w, l.variantsOf(t))
			default:
				// Undeclared or abstract local name — no body
				// to expand (abstract decls land here only via
				// typeD's miss; the oracle does not see file-local
				// names either way).
				return eqRuntime
			}
		}
		// A foreign nominal type: the oracle may know its shape.
		if l.env.Types != nil {
			w.seen[key] = true
			w.steps++
			if fs := l.env.Types.Fields(t); fs != nil {
				return l.eqFieldsKind(w, fs)
			}
			if vs := l.env.Types.Variants(t); vs != nil {
				w.seen[key] = true
				return l.eqVariantsKind(w, vs)
			}
		}
		return eqRuntime
	case *types.Tuple:
		for _, e := range t.Elems {
			if l.eqKindOf(w, e) != eqNative {
				return eqDecl
			}
		}
		return eqNative
	case *types.Go, *types.Func, *types.Var, *types.Meta:
		return eqRuntime
	default:
		panic("unreachable")
	}
}

// eqTypeKind classifies a locally declared type — a record is
// native when every substituted field is, a sum type when every
// payload slot is, an alias delegates to its target.
func (l *lowerer) eqTypeKind(w *eqClass, c *types.Con, d *check.TypeDecl) eqKind {
	sub := types.Subst{}
	for i, p := range d.TypeParams {
		if i < len(c.Args) && p != nil {
			sub[p.ID] = c.Args[i]
		}
	}
	switch b := d.Body.(type) {
	case *check.RecordType:
		for _, f := range b.Fields {
			if l.eqKindOf(w, sub.Apply(f.Type)) != eqNative {
				return eqDecl
			}
		}
		return eqNative
	case *check.SumType:
		for _, v := range b.Variants {
			for _, f := range v.Fields {
				if l.eqKindOf(w, sub.Apply(f.Type)) != eqNative {
					return eqDecl
				}
			}
		}
		return eqNative
	case *check.AliasType:
		return l.eqKindOf(w, sub.Apply(b.Type))
	case *check.AbstractType:
		return eqRuntime
	default:
		panic("unreachable")
	}
}

// eqFieldsKind answers the all-native question for a list of
// already-instantiated fields — the oracle's check.TField shape.
func (l *lowerer) eqFieldsKind(w *eqClass, fs []check.TField) eqKind {
	for _, f := range fs {
		if l.eqKindOf(w, f.Type) != eqNative {
			return eqDecl
		}
	}
	return eqNative
}

// eqVariantsKind answers the all-native question for a variant set
// — builtin Option/Result and oracle sums share the shape.
func (l *lowerer) eqVariantsKind(w *eqClass, vs []Variant) eqKind {
	for _, v := range vs {
		for _, f := range v.Fields {
			if l.eqKindOf(w, f.Type) != eqNative {
				return eqDecl
			}
		}
	}
	return eqNative
}

// eqDeclPlan builds the composite recipe a comparator's body
// interprets — the root plan of an EqDecl, where the statement-
// shaped kinds are legal.
func (l *lowerer) eqDeclPlan(sp diag.Span, t types.Type) *core.EqPlan {
	switch t := types.Chase(t).(type) {
	case *types.Tuple:
		fields := make([]core.EqField, len(t.Elems))
		for i, e := range t.Elems {
			fields[i] = core.EqField{Index: i, Plan: l.eqPlan(sp, e)}
		}
		return &core.EqPlan{Kind: core.EqTuple, Fields: fields}
	case *types.Con:
		if t.Qual == "Go" {
			switch t.Name {
			case "Slice":
				return &core.EqPlan{Kind: core.EqSlice, Elem: l.eqPlan(sp, t.Args[0])}
			case "Map":
				return &core.EqPlan{Kind: core.EqMap, Elem: l.eqPlan(sp, t.Args[1])}
			default:
				return &core.EqPlan{Kind: core.EqRuntime}
			}
		}
		if d, ok := l.typeD[conKey(t)]; ok {
			sub := types.Subst{}
			for i, p := range d.TypeParams {
				if i < len(t.Args) && p != nil {
					sub[p.ID] = t.Args[i]
				}
			}
			switch b := d.Body.(type) {
			case *check.RecordType:
				fields := make([]core.EqField, len(b.Fields))
				for i, f := range b.Fields {
					fields[i] = core.EqField{
						Sym: f.Sym, Name: f.Name, Index: f.Index,
						Plan: l.eqPlan(sp, sub.Apply(f.Type)),
					}
				}
				return &core.EqPlan{Kind: core.EqRecord, Fields: fields}
			case *check.SumType:
				return &core.EqPlan{Kind: core.EqSum, Variants: l.eqVariants(sp, t, b, sub)}
			case *check.AliasType:
				// Aliases expand at check time — a Con for one
				// should not arrive; when it does, delegate to the
				// target's recipe rather than silently falling to
				// rt.Equal (the classification walk delegates the
				// same way).
				return l.eqDeclPlan(sp, sub.Apply(b.Type))
			case *check.AbstractType:
				return &core.EqPlan{Kind: core.EqRuntime}
			default:
				panic("unreachable")
			}
		}
		if t.Qual == "" {
			switch t.Name {
			case "List", "Array":
				return &core.EqPlan{Kind: core.EqSlice, Elem: l.eqPlan(sp, t.Args[0])}
			case "Option":
				return &core.EqPlan{Kind: core.EqSum, Variants: l.eqBuiltinVariants(sp, t)}
			case "Result":
				return &core.EqPlan{Kind: core.EqResult, Variants: l.eqBuiltinVariants(sp, t)}
			default:
				return &core.EqPlan{Kind: core.EqRuntime}
			}
		}
		if l.env.Types != nil {
			if fs := l.env.Types.Fields(t); fs != nil {
				fields := make([]core.EqField, len(fs))
				for i, f := range fs {
					fields[i] = core.EqField{
						Sym: f.Sym, Name: f.Name, Index: f.Index,
						Plan: l.eqPlan(sp, f.Type),
					}
				}
				return &core.EqPlan{Kind: core.EqRecord, Fields: fields}
			}
			if vs := l.env.Types.Variants(t); vs != nil {
				vars := make([]core.EqVariant, len(vs))
				for i, v := range vs {
					vars[i] = core.EqVariant{Sym: v.Sym, Fields: l.eqTFields(sp, v.Fields, nil)}
				}
				return &core.EqPlan{Kind: core.EqSum, Variants: vars}
			}
		}
		return &core.EqPlan{Kind: core.EqRuntime}
	case *types.Func, *types.Go, *types.Meta, *types.Var:
		// cmpDecl is only reached when eqKindOf said eqDecl, and
		// those kinds never say it for these shapes — but a
		// defensive runtime plan beats a panic if the
		// classification and plan phases ever disagree.
		return &core.EqPlan{Kind: core.EqRuntime}
	default:
		panic("unreachable")
	}
}

// eqVariants renders a local sum type's constructor arms — the
// variant symbols come from the declaration, the payload recipes
// from each field's instantiated type.
func (l *lowerer) eqVariants(sp diag.Span, c *types.Con, b *check.SumType, sub types.Subst) []core.EqVariant {
	vars := make([]core.EqVariant, len(b.Variants))
	for i, v := range b.Variants {
		vars[i] = core.EqVariant{Sym: v.Sym, Fields: l.eqTFields(sp, v.Fields, sub)}
	}
	return vars
}

// eqBuiltinVariants renders Option/Result constructor arms through
// variantsOf — the Some/None and Ok/Err payload recipes. Result's
// fields select Value/Error on the flat representation rather than
// a positional slot.
func (l *lowerer) eqBuiltinVariants(sp diag.Span, c *types.Con) []core.EqVariant {
	vs := l.variantsOf(c)
	out := make([]core.EqVariant, len(vs))
	for i, v := range vs {
		fields := make([]core.EqField, len(v.Fields))
		for j, f := range v.Fields {
			sel := ""
			if l.isResultCon(c) {
				// The flat Result keeps its Ok payload in Value
				// and its Err payload in Error.
				if v.Sym.Name == "Ok" {
					sel = "Value"
				} else {
					sel = "Error"
				}
			}
			fields[j] = core.EqField{
				Sel: sel, Sym: f.Sym, Name: f.Name, Index: f.Index,
				Plan: l.eqPlan(sp, f.Type),
			}
		}
		out[i] = core.EqVariant{Sym: v.Sym, Fields: fields}
	}
	return out
}

// eqTFields renders one variant's payload slots — positional slots
// select `V<Index>`; named ones select their declared name.
func (l *lowerer) eqTFields(sp diag.Span, fs []check.TField, sub types.Subst) []core.EqField {
	out := make([]core.EqField, len(fs))
	for i, f := range fs {
		typ := f.Type
		if sub != nil {
			typ = sub.Apply(typ)
		}
		out[i] = core.EqField{
			Sym: f.Sym, Name: f.Name, Index: f.Index,
			Plan: l.eqPlan(sp, typ),
		}
	}
	return out
}
