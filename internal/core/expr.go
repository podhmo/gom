package core

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Expr is a Core IR expression. The interface is sealed: every
// variant is declared in this package and type switches over Expr
// must end with `default: panic("unreachable")`.
//
// Two families share the interface: atoms (also satisfying Atom) —
// trivially evaluable operands — and complex expressions, which may
// appear only at Let.X or Body.Tail. There are no patterns, no
// sugared operators and no holes left at this level.
//
//sumtype:decl
type Expr interface {
	Span() diag.Span
	Type() types.Type
	BaseOf() Base
	isExpr()
}

// Atom is an operand: an expression whose evaluation is trivial —
// no effects that ordering must protect and cheap enough to inline
// wherever a complex expression takes an operand. The interface is
// sealed: every variant is declared in this package and type
// switches over Atom must end with `default: panic("unreachable")`.
//
//sumtype:decl
type Atom interface {
	Expr
	isAtom()
}

// Var is a reference to a bound variable: a let binding, a
// parameter, a decision-tree binding or a package-level value
// (including a Go package variable). Functions and constructors are
// referenced through FuncRef / CtorRef instead — Sym.Kind is KindVar.
type Var struct {
	Base
	Sym *resolve.Symbol
}

func (*Var) isExpr() {}
func (*Var) isAtom() {}

// Lit is a literal: `42`, `3.14`, `"s"`, `'c'`, `true`, `()`.
// Interpolated strings do not reach Core IR — lowering expands them
// into calls. Text is the raw source lexeme; Kind is fixed.
type Lit struct {
	Base
	Kind syntax.LitKind
	Text string
}

func (*Lit) isExpr() {}
func (*Lit) isAtom() {}

// FuncRef is a top-level Gom function used as a value: a direct
// reference `pkg.F` emitted when the function's declared arity equals
// the arrow length of its type (P0-2 rule 3 — no flatten wrapper
// needed). When the declared arity is smaller — a function returning
// a function — lowering emits a Closure applying the trailing
// arguments instead; Arity records the declared arity so the
// validator can check the rule.
type FuncRef struct {
	Base
	Sym      *resolve.Symbol // KindFunc, not IsGo
	TypeArgs []types.Type    // instantiation of a polymorphic callee; nil while monomorphic
	Arity    int             // the callee's declared arity
}

func (*FuncRef) isExpr() {}
func (*FuncRef) isAtom() {}

// CtorRef is an ADT variant constructor used as a value. Variants
// construct as struct literals, so the reference materializes as a
// generated closure `func(v0, ...) T { return T_C{...} }`; Type()
// is the constructor's curried function type.
type CtorRef struct {
	Base
	Sym      *resolve.Symbol // KindCtor
	TypeArgs []types.Type
}

func (*CtorRef) isExpr() {}
func (*CtorRef) isAtom() {}

// GoFuncRef is a Go function or bound method used as a value —
// `strconv.Itoa` in `List.map strconv.Itoa xs`, or `buf.WriteString`.
// It is emitted directly only when the Go signature needs no
// adaptation: exactly one parameter (a zero-parameter function's
// `func() R` does not match the Gom `Unit -> R` value shape, and a
// multi-parameter function takes its arguments flat, not as the Gom
// tuple) and a single non-error result (Results == GoResultsOne).
// Anything else lowers to a Closure wrapping CallGo.
type GoFuncRef struct {
	Base
	Sym      *resolve.Symbol // IsGo; KindFunc, or KindMethod with Recv
	Recv     Atom            // the receiver for a bound method value; nil for package functions
	TypeArgs []types.Type
	Arity    int       // the Go function's parameter count
	Results  GoResults // the Go function's result shape
}

func (*GoFuncRef) isExpr() {}
func (*GoFuncRef) isAtom() {}

// FieldGet is a projection: a record field read `r.name`, a variant
// payload read `s.V0`, or a tuple element `t.V1`. Field is the
// declared field symbol (KindField) for records and named payload
// slots; it is nil for tuple projections and positional payload
// slots, which are selected by Index alone.
// Reading a field is pure and cheap, so FieldGet is an atom.
type FieldGet struct {
	Base
	Recv  Atom
	Field *resolve.Symbol // KindField; nil for tuple/positional projection
	Index int             // element position in the record / variant / tuple
	// Ctor is the variant constructor (KindCtor) owning the selected
	// payload slot when a named field is read through the sealed sum
	// interface — the receiver's Gom type is the ADT, so the read
	// asserts it to the variant's concrete struct
	// (`recv.(Shape_Circle).R`). Nil for record fields, tuple
	// projections, and payload reads on a variable the decision
	// tree already narrowed (Recv's representation is concrete).
	Ctor *resolve.Symbol
}

func (*FieldGet) isExpr() {}
func (*FieldGet) isAtom() {}

// Closure is a function literal `func(p...) R { ... }`. All function
// values that cannot be referenced directly are explicit closures at
// this level: partial applications (the body is the saturated
// CallDirect, CallFunc or CtorApp over bound atoms and the closure's
// own parameters, in declaration order), cute `_` placeholder calls,
// function compositions, flatten wrappers for function-returning
// functions, and Go-function adapters (body is a CallGo).
type Closure struct {
	Base
	Fn *Func
}

func (*Closure) isExpr() {}

// Unary is a primitive unary operation `op x`.
type Unary struct {
	Base
	Op syntax.UnOp
	X  Atom
}

func (*Unary) isExpr() {}

// Binary is a primitive binary operation `x op y`. Sugared pipeline
// and composition operators (`|>`, `<|`, `|?>`, `>>`, `<<`) never
// reach Core IR — lowering has already expanded them — so Op is
// never one for which syntax.BinOp.Sugared reports true.
type Binary struct {
	Base
	Op   syntax.BinOp
	X, Y Atom
}

func (*Binary) isExpr() {}

// If is `if c { a } else { b }`. Else is nil when omitted — allowed
// only when Then's result type is Unit.
type If struct {
	Base
	Cond Atom
	Then *Body
	Else *Body // nil when omitted
}

func (*If) isExpr() {}

// Match is a compiled match expression: the decision tree produced
// from pattern matching (docs/design.md section 4.2). No patterns
// remain — the tree is a nest of DSwitch / DGuard tests terminating
// in DLeaf bodies, with DPanic marking the contract-violation arm for
// invalid Go-side values. The subject is already bound; the tree's
// outermost switch reads it through an atom.
type Match struct {
	Base
	Tree DTree
}

func (*Match) isExpr() {}

// SelectExpr is `select { ... }` — one SelArm per Go select case
// (docs/decisions/2026-11-01-select-receive-arms.md). Channel and
// value expressions are atoms: evaluation of every arm's channel and
// send value happened once, in source order, in the enclosing body's
// bindings before the select is entered.
type SelectExpr struct {
	Base
	Arms []SelArm
}

func (*SelectExpr) isExpr() {}

// SelArm is one arm of a SelectExpr. The interface is sealed: every
// variant is declared in this package and type switches over SelArm
// must end with `default: panic("unreachable")`.
//
//sumtype:decl
type SelArm interface {
	Span() diag.Span
	isSelArm()
}

// SelRecv is a receive arm `x <- ch -> e`. Bind is the `Option a`
// variable the received value binds — or a discard Bind for `_ <- ch`
// — and Body runs with it in scope (an inner Match does the usual
// Option destructuring; there is no select-specific exhaustiveness).
type SelRecv struct {
	Sp   diag.Span
	Chan Atom
	Bind *Bind // Option-typed; may be Discard
	Body *Body
}

func (*SelRecv) isSelArm()         {}
func (n *SelRecv) Span() diag.Span { return n.Sp }

// SelSend is a send arm `send ch v -> e`.
type SelSend struct {
	Sp    diag.Span
	Chan  Atom
	Value Atom
	Body  *Body
}

func (*SelSend) isSelArm()         {}
func (n *SelSend) Span() diag.Span { return n.Sp }

// SelDefault is `default -> e`. At most one per SelectExpr.
type SelDefault struct {
	Sp   diag.Span
	Body *Body
}

func (*SelDefault) isSelArm()         {}
func (n *SelDefault) Span() diag.Span { return n.Sp }

// Spawn is `go { e }`: Body evaluated on a new goroutine, emitted as
// `go func() { ... }()`. The body's result type is Unit — a value
// produced by a goroutine would be silently lost (spec section 10).
type Spawn struct {
	Base
	Body *Body
}

func (*Spawn) isExpr() {}

// ForKind selects the iteration convention of a ForRange — the
// slice-family range versus the channel range.
type ForKind int

const (
	// ForList ranges over a List, Array or Go.Slice — `for _, x := range s`.
	ForList ForKind = iota + 1
	// ForChan receives from a Chan or RecvChan until it closes — `for x := range ch`.
	ForChan
)

// String returns the kind name for debugging.
func (k ForKind) String() string {
	switch k {
	case ForList:
		return "list"
	case ForChan:
		return "chan"
	default:
		return "ForKind(?)"
	}
}

// ForRange is `for x in iter { body }` — a loop, typed Unit. Pattern
// destructuring of the element is already compiled: Elem is a plain
// variable (or a discard Bind) and any destructuring match sits at
// the head of Body.
type ForRange struct {
	Base
	Kind ForKind
	Iter Atom
	Elem *Bind // the element variable; may be Discard
	Body *Body // result type Unit
}

func (*ForRange) isExpr() {}

// TupleNew is `(a, b, ...)` — an rt.TupleN literal.
type TupleNew struct {
	Base
	Elems []Atom // at least two
}

func (*TupleNew) isExpr() {}

// ListNew is `[e, ...]` with an optional `..rest` tail — the
// slice-representation `append(copy-of-rest, e...)` / literal
// construction decided by P0-3.
type ListNew struct {
	Base
	Elems []Atom
	Rest  Atom // the `..rest` tail list; nil when absent
}

func (*ListNew) isExpr() {}

// FieldInit is one `name = e` of a record construction or update,
// with the field resolved. Fields are stored in declaration order —
// lowering has already moved them out of evaluation order, which the
// ordered Lets of the enclosing body preserve.
type FieldInit struct {
	Sp    diag.Span
	Sym   *resolve.Symbol // the field (KindField)
	Index int             // position in the record
	Value Atom
}

// Span returns the field's source span.
func (f FieldInit) Span() diag.Span { return f.Sp }

// RecordNew is `{ name = e, ... }` — a record literal.
type RecordNew struct {
	Base
	Fields []FieldInit
}

func (*RecordNew) isExpr() {}

// RecordUpdate is `{ base with name = e, ... }` — copy the base
// record and overwrite the listed fields.
type RecordUpdate struct {
	Base
	Rec    Atom
	Fields []FieldInit
}

func (*RecordUpdate) isExpr() {}
