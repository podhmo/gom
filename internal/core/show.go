package core

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// This file holds the display recipe `assert_eq` carries — the
// compiler-generated display functions of docs/design.md section 4.8
// ("表示用の関数をコンパイラがすべての型について自動生成する").
// Lowering fixes a ShowPlan from the operands' static type using the
// same declaration information the comparator plans read; codegen
// only renders the recipe. The rendering format matches the
// evaluator's comparison display (internal/eval/value.go's String):
// `Rect ~w: 2 ~h: 3`, `{ name = "n" }`, `(1, 2)`, `[1, 2]`,
// `Ok 42` — with Go-view values shown through fmt's %v (spec
// section 11, "Go 由来の値は `fmt` の `%v` で表示する").

// ShowKind selects how a value of one type renders to text.
type ShowKind int

const (
	// ShowScalar renders a builtin scalar through strconv —
	// decimal ints, 'g' shortest floats, quoted strings and runes,
	// true/false bools, `()` for Unit.
	ShowScalar ShowKind = iota + 1
	// ShowRuntime renders `fmt.Sprintf("%v", v)` — the fallback for
	// Go-view types whose Gom-level shape does not exist or is
	// opaque: named Go types, Task, Go.Any, function values,
	// channels, references, and the defensive landing spots
	// (Var, Meta).
	ShowRuntime
	// ShowError renders a GoError as `error("text")`, the
	// evaluator's VGoErr reading, nil-aware.
	ShowError
	// ShowCall renders `Fn(v)` — a call to the generated display
	// function of a composite operand type. Fn's Plan is what makes
	// the rendering recursive.
	ShowCall
	// ShowTuple renders `(a, b, c)` fieldwise — a ShowDecl.Plan for
	// tuple operand types.
	ShowTuple
	// ShowSlice renders `[a, b]` elementwise — the ShowDecl.Plan of
	// List, Array and Go.Slice operands.
	ShowSlice
	// ShowRecord renders `{ name = v, ... }` fieldwise in
	// declaration order — the ShowDecl.Plan of record operands.
	ShowRecord
	// ShowSum renders a sealed ADT as `Ctor ~name: v v2` (named
	// fields carry `~name:`, positional ones just the value) — the
	// ShowDecl.Plan of Option and declared sum types.
	ShowSum
	// ShowResult renders the builtin Result's flat representation
	// as `Ok v` / `Err e` through a Tag() switch — the
	// ShowDecl.Plan of Result operands.
	ShowResult
)

// String returns the kind name for debugging and core dumps.
func (k ShowKind) String() string {
	switch k {
	case ShowScalar:
		return "scalar"
	case ShowRuntime:
		return "runtime"
	case ShowError:
		return "error"
	case ShowCall:
		return "call"
	case ShowTuple:
		return "tuple"
	case ShowSlice:
		return "slice"
	case ShowRecord:
		return "record"
	case ShowSum:
		return "sum"
	case ShowResult:
		return "result"
	default:
		return "ShowKind(?)"
	}
}

// ShowPlan is one operand's display recipe — the mini-program a
// CallBuiltin's Show field or a ShowDecl's Plan carries. Leaf kinds
// (ShowScalar, ShowRuntime, ShowError, ShowCall) may appear
// anywhere; the composite kinds (ShowTuple through ShowResult)
// appear only as a ShowDecl's Plan — nested operands always go
// through ShowCall, which is what keeps recursive types finite.
type ShowPlan struct {
	Kind ShowKind
	// Scalar is the builtin constructor name the leaf renders —
	// ShowScalar only.
	Scalar string
	// Fn is the display function to call — ShowCall only.
	Fn *ShowDecl
	// Elem is the element recipe — ShowSlice only.
	Elem *ShowPlan
	// Elems are the element recipes — ShowTuple only, in order.
	Elems []*ShowPlan
	// Fields is the projected operand — ShowRecord only, in
	// declaration order.
	Fields []ShowField
	// Variants is the constructor arms — ShowSum and ShowResult
	// only, in declaration order (Result: Ok then Err).
	Variants []ShowVariant
}

// ShowField is one projected operand inside a record or variant
// plan: the selector applied to the operand and the recipe it
// renders under. Sel, when non-empty, names a method/accessor
// override used by flat representations (Result's Value / Error);
// otherwise Sym names the declared field or Index selects `V<Index>`
// on tuple/variant structs.
type ShowField struct {
	Sel   string          // accessor override (Result Value/Error); "" selects by Sym/Index
	Sym   *resolve.Symbol // KindField when the field is declared; nil otherwise
	Name  string          // the field's Gom name — the label `{ name = v }` prints and `~name:` selects
	Index int             // position — `V<Index>` selection when Sym and Name are absent
	Plan  *ShowPlan
}

// ShowVariant is one constructor branch of a ShowSum or ShowResult
// plan: Sym is the constructor whose concrete type the operand is
// tested against (or whose tag the flat Result reads); Fields are
// the payload recipes.
type ShowVariant struct {
	Sym    *resolve.Symbol // KindCtor
	Fields []ShowField
}

// ShowDecl is a generated display function — `func <Sym>(v Typ)
// string` — emitted into the test file after the test declarations.
// Lowering mints one per distinct assert_eq operand type; recursion
// between display functions goes through ShowCall plans.
//
// ShowDecl is deliberately not a Decl, for the same reason as
// EqDecl: it carries no user-source meaning and lives on
// File.ShowDecls, emitted by codegen without the decl-walking passes
// running over it.
type ShowDecl struct {
	Sp  diag.Span
	Sym *resolve.Symbol // KindFunc — a synthesized gomShow<N> name
	// Typ is the displayed type; the parameter takes it. Codegen
	// renders the Go representation of Typ as the parameter type.
	Typ types.Type
	// Plan is the display function body's recipe — a composite
	// ShowKind, or a leaf kind for the defensive cases.
	Plan *ShowPlan
}

// Span returns the declaration's source span — the first assert_eq
// site that needed the display function.
func (d *ShowDecl) Span() diag.Span { return d.Sp }
