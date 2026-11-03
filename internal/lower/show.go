package lower

import (
	"fmt"

	"github.com/podhmo/gom/internal/check"
	"github.com/podhmo/gom/internal/core"
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// This file turns an assert_eq operand type into the display recipe
// a core.CallBuiltin's Show carries — and mints the generated
// display functions (core.ShowDecl) composite types render through
// (docs/design.md section 4.8, spec section 11: "失敗すると両方の値を
// 表示する").
//
// The classification is simpler than eqPlan's: `==` can fall back on
// Go's native comparison for all-scalar composites, but no display
// shortcut exists — every composite operand type mints its own
// `func gomShowN(v T) string`, so nested operands always land on a
// leaf recipe (scalar strconv, runtime %v, the error recipe, or a
// ShowCall into the minted decl). The operand type comes from
// args[0] — the checker already unified both operands under the eq
// constraint, so one recipe serves both sides.

// showDeclLimit bounds the display functions one file may mint, for
// the same reason eqDeclLimit bounds comparators: an expanding
// self-application mints a fresh instantiated type at every level.
// Overflow degrades the plan to ShowRuntime — a failure display
// through %v is less informative than the structured rendering but
// never wrong, so the budget reports no diagnostic.
const showDeclLimit = 128

// showPlan returns the leaf recipe displaying a value of type t —
// the recipe a CallBuiltin's Show or a nested field of a composite
// plan carries.
func (l *lowerer) showPlan(sp diag.Span, t types.Type) *core.ShowPlan {
	switch l.showKindOf(t) {
	case showScalar:
		c := types.Chase(t).(*types.Con)
		return &core.ShowPlan{Kind: core.ShowScalar, Scalar: c.Name}
	case showError:
		return &core.ShowPlan{Kind: core.ShowError}
	case showDecl:
		if d := l.showDecl(sp, t); d != nil {
			return &core.ShowPlan{Kind: core.ShowCall, Fn: d}
		}
		// The mint budget ran out — degrade to the runtime recipe;
		// the failure display loses structure but keeps the values.
		return &core.ShowPlan{Kind: core.ShowRuntime}
	default:
		return &core.ShowPlan{Kind: core.ShowRuntime}
	}
}

// showKind classifies what displaying one operand type renders as.
type showKind int

const (
	// showScalar — a builtin scalar rendered through strconv.
	showScalar showKind = iota + 1
	// showError — GoError's nil-aware `error("text")` recipe.
	showError
	// showDecl — a generated display function: tuples, lists,
	// slices, records, ADTs, Option, Result.
	showDecl
	// showRuntime — the fmt %v fallback for everything else.
	showRuntime
)

// showKindOf reports which display recipe operand type t needs.
// Local declarations (typeD) shadow the builtin table, which
// shadows the foreign oracle — the same precedence eqKindOf applies.
func (l *lowerer) showKindOf(t types.Type) showKind {
	switch t := types.Chase(t).(type) {
	case *types.Con:
		if t.Qual == "Go" {
			switch t.Name {
			case "Int":
				return showScalar
			case "Slice":
				return showDecl
			default:
				return showRuntime
			}
		}
		if t.Qual == "" {
			if d, ok := l.typeD[conKey(t)]; ok {
				switch d.Body.(type) {
				case *check.RecordType, *check.SumType:
					return showDecl
				case *check.AliasType:
					// Unreachable — aliases expand at check time —
					// but the runtime recipe stays sound if one
					// slips through.
					return showRuntime
				case *check.AbstractType:
					return showRuntime
				default:
					panic("unreachable")
				}
			}
			switch t.Name {
			case "Unit", "Bool", "Int", "Int32", "Int64", "Float",
				"Char", "String":
				return showScalar
			case "GoError":
				return showError
			case "List", "Array", "Option", "Result":
				return showDecl
			default:
				return showRuntime
			}
		}
		// A foreign nominal type: the oracle may know its shape.
		if l.env.Types != nil {
			if l.env.Types.Fields(t) != nil || l.env.Types.Variants(t) != nil {
				return showDecl
			}
		}
		return showRuntime
	case *types.Tuple:
		return showDecl
	case *types.Go, *types.Func, *types.Var, *types.Meta:
		return showRuntime
	default:
		panic("unreachable")
	}
}

// showDecl returns the generated display function for the composite
// type t — minting it (and registering it for emission) on first
// use. Registration precedes plan building so a recursive type's
// nested recipe resolves to its own in-progress decl. A nil answer
// means the showDeclLimit budget ran out — the caller degrades the
// plan.
func (l *lowerer) showDecl(sp diag.Span, t types.Type) *core.ShowDecl {
	if l.showDecls == nil {
		l.showDecls = map[string]*core.ShowDecl{}
	}
	t = types.Chase(t)
	key := t.String()
	if d, ok := l.showDecls[key]; ok {
		return d
	}
	if len(l.showDecls) >= showDeclLimit {
		return nil
	}
	l.showSeq++
	d := &core.ShowDecl{
		Sp:  sp,
		Sym: &resolve.Symbol{Name: fmt.Sprintf("gomShow%d_%s", l.showSeq, l.eqMark), Kind: resolve.KindFunc},
		Typ: t,
	}
	l.showDecls[key] = d
	l.showOrder = append(l.showOrder, d)
	d.Plan = l.showDeclPlan(sp, t)
	return d
}

// showDeclPlan builds the composite recipe a display function's body
// interprets — the root plan of a ShowDecl, where the statement-
// shaped kinds are legal.
func (l *lowerer) showDeclPlan(sp diag.Span, t types.Type) *core.ShowPlan {
	switch t := types.Chase(t).(type) {
	case *types.Tuple:
		elems := make([]*core.ShowPlan, len(t.Elems))
		for i, e := range t.Elems {
			elems[i] = l.showPlan(sp, e)
		}
		return &core.ShowPlan{Kind: core.ShowTuple, Elems: elems}
	case *types.Con:
		if t.Qual == "Go" {
			if t.Name == "Slice" {
				return &core.ShowPlan{Kind: core.ShowSlice, Elem: l.showPlan(sp, t.Args[0])}
			}
			return &core.ShowPlan{Kind: core.ShowRuntime}
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
				fields := make([]core.ShowField, len(b.Fields))
				for i, f := range b.Fields {
					fields[i] = core.ShowField{
						Sym: f.Sym, Name: f.Name, Index: f.Index,
						Plan: l.showPlan(sp, sub.Apply(f.Type)),
					}
				}
				return &core.ShowPlan{Kind: core.ShowRecord, Fields: fields}
			case *check.SumType:
				return &core.ShowPlan{Kind: core.ShowSum, Variants: l.showVariants(sp, t, b, sub)}
			case *check.AliasType:
				return l.showDeclPlan(sp, sub.Apply(b.Type))
			case *check.AbstractType:
				return &core.ShowPlan{Kind: core.ShowRuntime}
			default:
				panic("unreachable")
			}
		}
		if t.Qual == "" {
			switch t.Name {
			case "List", "Array":
				return &core.ShowPlan{Kind: core.ShowSlice, Elem: l.showPlan(sp, t.Args[0])}
			case "Option":
				return &core.ShowPlan{Kind: core.ShowSum, Variants: l.showBuiltinVariants(sp, t)}
			case "Result":
				return &core.ShowPlan{Kind: core.ShowResult, Variants: l.showBuiltinVariants(sp, t)}
			default:
				return &core.ShowPlan{Kind: core.ShowRuntime}
			}
		}
		if l.env.Types != nil {
			if fs := l.env.Types.Fields(t); fs != nil {
				fields := make([]core.ShowField, len(fs))
				for i, f := range fs {
					fields[i] = core.ShowField{
						Sym: f.Sym, Name: f.Name, Index: f.Index,
						Plan: l.showPlan(sp, f.Type),
					}
				}
				return &core.ShowPlan{Kind: core.ShowRecord, Fields: fields}
			}
			if vs := l.env.Types.Variants(t); vs != nil {
				vars := make([]core.ShowVariant, len(vs))
				for i, v := range vs {
					vars[i] = core.ShowVariant{Sym: v.Sym, Fields: l.showTFields(sp, v.Fields, nil)}
				}
				return &core.ShowPlan{Kind: core.ShowSum, Variants: vars}
			}
		}
		return &core.ShowPlan{Kind: core.ShowRuntime}
	case *types.Func, *types.Go, *types.Meta, *types.Var:
		return &core.ShowPlan{Kind: core.ShowRuntime}
	default:
		panic("unreachable")
	}
}

// showVariants renders a local sum type's constructor arms — the
// variant symbols come from the declaration, the payload recipes
// from each field's instantiated type.
func (l *lowerer) showVariants(sp diag.Span, c *types.Con, b *check.SumType, sub types.Subst) []core.ShowVariant {
	vars := make([]core.ShowVariant, len(b.Variants))
	for i, v := range b.Variants {
		vars[i] = core.ShowVariant{Sym: v.Sym, Fields: l.showTFields(sp, v.Fields, sub)}
	}
	return vars
}

// showBuiltinVariants renders Option/Result constructor arms through
// variantsOf — the Some/None and Ok/Err payload recipes. Result's
// fields select Value/Error on the flat representation rather than
// a positional slot.
func (l *lowerer) showBuiltinVariants(sp diag.Span, c *types.Con) []core.ShowVariant {
	vs := l.variantsOf(c)
	out := make([]core.ShowVariant, len(vs))
	for i, v := range vs {
		fields := make([]core.ShowField, len(v.Fields))
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
			fields[j] = core.ShowField{
				Sel: sel, Sym: f.Sym, Name: f.Name, Index: f.Index,
				Plan: l.showPlan(sp, f.Type),
			}
		}
		out[i] = core.ShowVariant{Sym: v.Sym, Fields: fields}
	}
	return out
}

// showTFields renders one variant's payload slots — positional slots
// select `V<Index>`; named ones select their declared name.
func (l *lowerer) showTFields(sp diag.Span, fs []check.TField, sub types.Subst) []core.ShowField {
	out := make([]core.ShowField, len(fs))
	for i, f := range fs {
		typ := f.Type
		if sub != nil {
			typ = sub.Apply(typ)
		}
		out[i] = core.ShowField{
			Sym: f.Sym, Name: f.Name, Index: f.Index,
			Plan: l.showPlan(sp, typ),
		}
	}
	return out
}
