package core

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// SwitchKind selects the discrimination mechanism a DSwitch compiles
// to — which generated-code shape the subject's representation
// implies (P0-1 / P0-3 / P0-4). Lowering fixes it; codegen only
// reads it.
type SwitchKind int

const (
	// SwitchType is a type switch `switch s := subj.(type)` over a
	// sealed-interface ADT (P0-1). Each case names a variant
	// constructor (DCtorTest) and may bind the subject narrowed to
	// the variant struct. The Default must be a DPanic — Go-side
	// invalid values (a nil interface, a typed nil) panic via
	// rt.InvalidVariant. A wildcard arm is compiled by expanding it
	// over the uncovered variants as cases, keeping the panic in
	// Default.
	SwitchType SwitchKind = iota + 1
	// SwitchTag is a tag switch `switch subj.Tag()` over the flat2
	// Result representation (P0-4). Each case names the Ok or Err
	// constructor (DCtorTest) and may bind the variant's payload —
	// `subj.Value` / `subj.Error`. The Default must be a DPanic:
	// tag 0 (zero value) or an unknown tag is invalid.
	SwitchTag
	// SwitchValue is a value switch `switch subj` over literal
	// equality — Int / String / Char / Bool tests (DLitTest) — used
	// for constructor-free discrimination on infinite signatures.
	// The Default is the fallthrough tree (any DTree), or DPanic
	// when the literal set is complete (e.g. both Bool arms).
	SwitchValue
	// SwitchList is the list-shape test over the slice
	// representation of `List a` (P0-3) — `len(subj) == 0` selects
	// Nil, otherwise Cons; a Cons case may bind head (`subj[0]`) and
	// tail (`subj[1:]`). A nil slice is a valid empty list, so the
	// shape covers every value; the Default is unreachable but must
	// still be present (a DPanic is the natural filler).
	SwitchList
)

// String returns the kind name for debugging.
func (k SwitchKind) String() string {
	switch k {
	case SwitchType:
		return "type"
	case SwitchTag:
		return "tag"
	case SwitchValue:
		return "value"
	case SwitchList:
		return "list"
	default:
		return "SwitchKind(?)"
	}
}

// DTree is a decision-tree node — the compiled form of pattern
// matching in which no patterns remain (docs/design.md section 2.1,
// item 4). The interface is sealed: every variant is declared in this
// package and type switches over DTree must end with
// `default: panic("unreachable")`.
//
//sumtype:decl
type DTree interface {
	Span() diag.Span
	Type() types.Type // the tree's result type (DPanic keeps the match's type)
	BaseOf() Base
	isDTree()
}

// DSwitch branches on an atomic subject. Cases are tried in order
// — each is a DTest — and Default is the tree to continue in when no
// case matches: another DSwitch (nested discrimination), a DLeaf
// (the catch-all arm), a DGuard, or a DPanic. Whether Default may be
// a non-panic tree depends on the kind — see SwitchKind.
type DSwitch struct {
	Base
	Subj    Atom
	Kind    SwitchKind
	Cases   []DCase
	Default DTree
}

func (*DSwitch) isDTree() {}

// DCase is one case of a DSwitch. Binds are the symbols the case
// introduces, in the order the kind fixes (see SwitchKind): the
// narrowed subject for SwitchType, the payload for SwitchTag, head
// and tail for a SwitchList Cons case, and none elsewhere. A case
// that uses no bind leaves Binds empty rather than binding `_`.
// Body is the arm's computation, which may itself begin with
// projections of the bound subject and nested Match expressions.
type DCase struct {
	Sp    diag.Span
	Test  DTest
	Binds []*Bind
	Body  *Body
}

// Span returns the case's source span.
func (c DCase) Span() diag.Span { return c.Sp }

// DTest is the discriminator one DCase tests. The interface is
// sealed: every variant is declared in this package and type
// switches over DTest must end with `default: panic("unreachable")`.
//
//sumtype:decl
type DTest interface {
	Span() diag.Span
	isDTest()
}

// DCtorTest tests a variant constructor: the type-switch case type
// for SwitchType (`case Shape_Circle`) or the tag constant for
// SwitchTag (`case ResultTagOk`). Ctor is the constructor symbol —
// for Result it is the stdlib's Ok / Err ctor.
type DCtorTest struct {
	Sp   diag.Span
	Ctor *resolve.Symbol // KindCtor
}

func (*DCtorTest) isDTest()          {}
func (n *DCtorTest) Span() diag.Span { return n.Sp }

// DLitTest is a literal case for SwitchValue — `case 0, "ok", 'a'`.
// Kind is never LitUnit (a Unit subject carries no information).
type DLitTest struct {
	Sp   diag.Span
	Kind syntax.LitKind
	Text string
}

func (*DLitTest) isDTest()          {}
func (n *DLitTest) Span() diag.Span { return n.Sp }

// DListTest is the empty/non-empty case of a SwitchList switch:
// Cons false is the `[]` / Nil shape, Cons true the `[x, ..rest]`
// shape.
type DListTest struct {
	Sp   diag.Span
	Cons bool
}

func (*DListTest) isDTest()          {}
func (n *DListTest) Span() diag.Span { return n.Sp }

// DLeaf is a terminal of the decision tree: run Body — the arm's
// computation with the case binds in scope.
type DLeaf struct {
	Base
	Body *Body
}

func (*DLeaf) isDTree() {}

// DPanic is a terminal marking the contract-violation arm for
// invalid Go-side values (spec section 8.6): it emits
// `panic(rt.InvalidVariant(TypeName, Subj))`. Exhaustive ADT and
// Result switches keep it as their Default so a nil or corrupt value
// can never silently match.
type DPanic struct {
	Base
	Subj     Atom   // the offending value
	TypeName string // qualified type name for the message, e.g. "shapes.Shape"
}

func (*DPanic) isDTree() {}

// DGuard is a guard continuation: if Cond holds, run Then (usually a
// DLeaf), else continue in Else — the tree for the remaining arms.
// Guards test already-bound atoms, so this is a plain conditional in
// tree form; it exists so the tree can express "arm applies only when
// the guard is true" without re-testing the pattern.
type DGuard struct {
	Base
	Cond Atom
	Then DTree
	Else DTree
}

func (*DGuard) isDTree() {}
