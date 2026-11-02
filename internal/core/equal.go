package core

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Equal is a structural comparison `x == y` — or `x != y` when Neg
// is set — whose recipe is fixed by How. Lowering decides How from
// the operands' shared static type using the same declaration
// information the checker's eq constraint applied (records and ADTs
// expand, aliases resolve); codegen only renders the recipe — it
// never re-derives which comparison a type needs.
//
// An Equal always has a leaf How (EqNative, EqRuntime, EqError or
// EqCall): every composite operand type routes through a generated
// comparator declared on File.EqDecls, so one type has exactly one
// comparison function shared by all of its `==` sites and by every
// composite plan that nests it.
type Equal struct {
	Base
	Neg  bool // `!=` when set
	X, Y Atom
	How  *EqPlan
}

func (*Equal) isExpr() {}

// EqKind selects how one operand pair of a comparison renders.
type EqKind int

const (
	// EqNative renders Go's own `==`/`!=`. It is sound exactly where
	// the representation's native comparison carries Gom's
	// structural semantics: scalars, identity references (Ref, Ptr,
	// channels), tuples/records/Results built entirely from those
	// (Go compares struct fields with `==`, which preserves IEEE
	// NaN behavior), and ADTs whose payloads are all native —
	// interface `==` compares (variant, value) pairs, the same
	// reading, and the sealed interface guarantees the dynamic
	// values are comparable.
	EqNative EqKind = iota + 1
	// EqRuntime renders `rt.Equal(x, y)` — the reflect-based
	// fallback for Go-view types whose Gom-level recipe does not
	// exist: named Go types (their layout is opaque), Task, Go.Any,
	// and the defensive landing spots (Func, unresolved Var/Meta)
	// that the checker's eq constraint rejects before codegen.
	EqRuntime
	// EqError renders the GoError recipe: nil-aware message-text
	// comparison, `(x == nil) == (y == nil) && (x == nil ||
	// x.Error() == y.Error())` — matching the evaluator's VGoErr
	// reading.
	EqError
	// EqCall renders `Fn(x, y)` — a call to the generated
	// comparator for a composite operand type. Fn's Plan is what
	// makes the comparison recursive.
	EqCall
	// EqSlice compares elementwise — the EqDecl.Plan of List,
	// Array and Go.Slice operands: same length (a nil slice and an
	// empty slice both have length 0, so nil equals empty) and
	// every element pair Eq-ing per Elem. This is the reading that
	// removes reflect.DeepEqual's identical-backing-store shortcut,
	// so `[NaN] == [NaN]` is false even when both sides alias.
	EqSlice
	// EqMap compares key sets and values — the EqDecl.Plan of
	// Go.Map operands. Keys are always Go-comparable by
	// construction; values use Elem.
	EqMap
	// EqTuple compares an rt.TupleN fieldwise — the EqDecl.Plan of
	// tuple operands whose elements are not all EqNative.
	EqTuple
	// EqRecord compares a record struct fieldwise — the
	// EqDecl.Plan of record operands that are not all EqNative.
	EqRecord
	// EqSum compares a sealed ADT: both sides nil or the same
	// variant, then payloads fieldwise — the EqDecl.Plan of Option
	// and declared sum types that are not all EqNative.
	EqSum
	// EqResult compares the builtin Result's flat representation —
	// same tag, then the Value payload for Ok and the Error payload
	// for Err — the EqDecl.Plan of Result operands whose payload
	// types are not all EqNative.
	EqResult
)

// eqLeafKinds are the kinds legal as an Equal.How and as a nested
// plan inside a composite one — every recipe that renders in
// expression position.
func eqLeafKind(k EqKind) bool {
	switch k {
	case EqNative, EqRuntime, EqError, EqCall:
		return true
	default:
		return false
	}
}

// String returns the kind name for debugging and core dumps.
func (k EqKind) String() string {
	switch k {
	case EqNative:
		return "native"
	case EqRuntime:
		return "runtime"
	case EqError:
		return "error"
	case EqCall:
		return "call"
	case EqSlice:
		return "slice"
	case EqMap:
		return "map"
	case EqTuple:
		return "tuple"
	case EqRecord:
		return "record"
	case EqSum:
		return "sum"
	case EqResult:
		return "result"
	default:
		return "EqKind(?)"
	}
}

// EqPlan is one operand pair's comparison recipe — the mini-program
// an Equal node's How or an EqDecl's Plan carries. Leaf kinds
// (EqNative, EqRuntime, EqError, EqCall) may appear anywhere; the
// composite kinds (EqSlice through EqResult) appear only as an
// EqDecl's Plan — nested operand pairs always go through EqCall,
// which is what keeps recursive types finite.
type EqPlan struct {
	Kind EqKind
	// Fn is the comparator to call — EqCall only.
	Fn *EqDecl
	// Elem is the element recipe — EqSlice and EqMap only (for maps,
	// the value recipe; keys always compare natively).
	Elem *EqPlan
	// Fields is the projected operand pairs — EqTuple and EqRecord
	// only, in declaration order.
	Fields []EqField
	// Variants is the constructor arms — EqSum and EqResult only, in
	// declaration order (Result: Ok then Err).
	Variants []EqVariant
}

// EqField is one projected operand pair inside a composite plan:
// the selector applied to both sides and the recipe the pair
// compares under. Sel, when non-empty, names a method/accessor
// override used by flat representations (Result's Value / Error);
// otherwise Sym names the declared record/payload field or Index
// selects `V<Index>` on tuple/variant structs.
type EqField struct {
	Sel   string          // accessor override (Result Value/Error); "" selects by Sym/Index
	Sym   *resolve.Symbol // KindField when the field is declared; nil otherwise
	Name  string          // the field's Go name when Sym is nil (foreign records)
	Index int             // position — `V<Index>` selection when Sym and Name are absent
	Plan  *EqPlan
}

// EqVariant is one constructor branch of an EqSum or EqResult plan:
// Sym is the constructor whose concrete type the a-side operand is
// tested against; Fields are the payload recipes the narrowed pair
// compares under. A variant with no payload fields contributes only
// its tag to the comparison.
type EqVariant struct {
	Sym    *resolve.Symbol // KindCtor
	Fields []EqField
}

// EqDecl is a generated structural comparator — `func <Sym>(a, b
// Typ) bool` — emitted as a file-scope function after the file's own
// declarations. Lowering mints one per distinct composite operand
// type; recursion between comparators goes through EqCall plans, so
// a recursive type's comparator is a recursive Go function.
//
// EqDecl is deliberately not a Decl: it carries no user-source
// meaning and lives on File.EqDecls, emitted by codegen without the
// decl-walking passes (binder walks, symbol-use analysis) running
// over it — its plan describes comparisons, not bindings.
type EqDecl struct {
	Sp  diag.Span
	Sym *resolve.Symbol // KindFunc — a synthesized gomEq<N> name
	// Typ is the compared type; both parameters take it. Codegen
	// renders the Go representation of Typ as the parameter type.
	Typ types.Type
	// Plan is the comparator body's recipe — a composite EqKind, or
	// a leaf kind for the defensive cases (a runtime-fallback type
	// that still reached an EqDecl).
	Plan *EqPlan
}

// Span returns the declaration's source span — the first `==` site
// that needed the comparator.
func (d *EqDecl) Span() diag.Span { return d.Sp }
