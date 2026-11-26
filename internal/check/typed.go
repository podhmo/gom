package check

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Base carries the two fields every typed node has — its source span
// and its resolved type — and supplies the Expr/Pat interface methods.
// It is exported so later phases can build typed nodes in tests.
type Base struct {
	Sp  diag.Span
	Typ types.Type
}

// Span returns the node's source span.
func (b Base) Span() diag.Span { return b.Sp }

// Type returns the node's resolved type.
func (b Base) Type() types.Type { return b.Typ }

// Expr is a typed expression: the surface expression with every node
// annotated by its resolved type and every identifier replaced by its
// resolved symbol (docs/design.md section 2). The interface is
// sealed: every variant is declared in this package and type switches
// over Expr must end with `default: panic("unreachable")`.
//
//sumtype:decl
type Expr interface {
	Span() diag.Span
	Type() types.Type
	isExpr()
}

// Lit is a literal.
type Lit struct {
	Base
	Kind syntax.LitKind
	Text string
}

func (*Lit) isExpr() {}

// Var is a reference to a resolved symbol: a variable, a function, a
// variant constructor used as a function, or a Go name.
type Var struct {
	Base
	Sym *resolve.Symbol
	// TypeArgs are the type arguments this reference instantiated
	// the symbol's quantified variables with — the fresh metas
	// valOf substituted for each rigid variable, in first-occurrence
	// order (the same list CallInfo.TypeArgs records). Nil when the
	// symbol's type is monomorphic or the reference is a shared
	// (local) binding.
	TypeArgs []types.Type
}

func (*Var) isExpr() {}

// Arg is one argument of a typed application, in source (evaluation)
// order.
type Arg struct {
	Label   string    // "" for positional, else the `~label` name
	LabelSp diag.Span // span of the label, zero when positional
	Value   Expr
	// Spread marks a `~..e` record-spread argument while the call's
	// convention is undecided (a deferred apply's provisional args).
	// Replay expands it into labeled Args; it never reaches CallInfo.
	Spread bool
}

// Span returns the argument's span (label through value).
func (a Arg) Span() diag.Span {
	if a.LabelSp.Valid() {
		return diag.SpanIn(a.LabelSp.File, a.LabelSp.Start, a.Value.Span().End)
	}
	return a.Value.Span()
}

// Apply is a function application with its structured call info
// (docs/design.md sections 2 and 2.1). Args are the semantic call
// arguments in source order — for a Go call the argument tuple of the
// surface form is unpacked into the individual Go arguments.
type Apply struct {
	Base
	Fn   Expr
	Args []Arg
	Call CallInfo
	// CopyFrom records the deferred call's real result type when a
	// consumer's `Go.Slice` want pinned the result meta before the
	// replay supplied its `List` — the producer still yields the
	// List, so lowering emits the copy the boundary promises
	// (`.ToSlice()`), not a raw alias into callee-owned storage.
	CopyFrom types.Type
}

func (*Apply) isExpr() {}

// Hole is the `_` placeholder of a cute-style partial Go call.
// It appears only inside Apply.Args of a CallGo node whose
// CallInfo.HoleIndex points at it.
type Hole struct {
	Base
}

func (*Hole) isExpr() {}

// MemberExpr is member access `recv.sel` with the member resolved:
// record/variant field access, module member selection, or a Go
// method selection. (The name avoids colliding with the channel
// SelectExpr.) Sel keeps the selector's name node — its span is
// more precise than the whole MemberExpr's for diagnostics.
type MemberExpr struct {
	Base
	Recv        Expr
	Sel         *syntax.Ident   // the written selector
	Sym         *resolve.Symbol // the selected member (KindField, a module member, or KindMethod)
	GoFieldType types.Type      // raw Go field type; nil for non-Go fields
	Index       int             // field position for record/variant payloads; -1 otherwise
	// Ctor is the variant constructor (KindCtor) that owns the
	// selected slot when a named payload field is read through its
	// sum type — the read is legal only for the variant that
	// declared the name, so lowering records which concrete
	// representation the receiver must hold. Nil for record fields
	// and every non-field member.
	Ctor *resolve.Symbol
	// TypeArgs mirrors Var.TypeArgs for member references whose
	// symbol quantifies type variables (`List.map`, `Option.Some`,
	// a generic fn reached through a module).
	TypeArgs []types.Type
	// CopyFrom records the member's declared `List` type when a
	// consumer's `Go.Slice` want pinned the standing meta before
	// the member resolved — the read still owes its per-read copy
	// (`FromSlice` then `ToSlice`), or the callee would share the
	// field's backing array.
	CopyFrom types.Type
}

func (*MemberExpr) isExpr() {}

// FieldAssign is `ptr.Field = v` — a field write through a `Ptr T`
// handle where T is a named Go struct (spec 8.3,
// decisions/2026-11-19-struct-field-write.md). Recv/Sel/Sym/
// GoFieldType describe the resolved Go field, exactly as the read
// side's MemberExpr does; Value checked against the field's
// Gom-facing type. The node's own type is Unit.
type FieldAssign struct {
	Base
	Recv        Expr
	Sel         *syntax.Ident
	Sym         *resolve.Symbol // KindField (a Go struct field)
	GoFieldType types.Type      // raw Go field type — the write's target type
	Value       Expr
}

func (*FieldAssign) isExpr() {}

// Unary is a unary operation.
type Unary struct {
	Base
	Op syntax.UnOp
	X  Expr
}

func (*Unary) isExpr() {}

// Binary is a binary operation. The sugared pipeline and composition
// operators (`|>`, `<|`, `|?>`, `>>`, `<<`) are still Binary nodes at
// this stage — they are lowered to applications in phase 6.
type Binary struct {
	Base
	Op   syntax.BinOp
	X, Y Expr
}

func (*Binary) isExpr() {}

// StrPart is one part of an interpolated string: text or a splice.
//
//sumtype:decl
type StrPart interface {
	Span() diag.Span
	isStrPart()
}

// StrText is a literal chunk of an interpolated string.
type StrText struct {
	Sp   diag.Span
	Text string
}

func (*StrText) isStrPart()        {}
func (n *StrText) Span() diag.Span { return n.Sp }

// StrExpr is a `${expr}` splice.
type StrExpr struct {
	Sp diag.Span
	X  Expr
}

func (*StrExpr) isStrPart()        {}
func (n *StrExpr) Span() diag.Span { return n.Sp }

// StrInterp is an interpolated string literal.
type StrInterp struct {
	Base
	Parts []StrPart
}

func (*StrInterp) isExpr() {}

// Param is one parameter of a lambda. Its pattern carries the
// parameter type and binds the parameter symbol.
type Param struct {
	Label string // non-empty for `~name` parameters
	Pat   Pat
}

// Lambda is `fn a b -> body`.
type Lambda struct {
	Base
	Params []Param
	Body   Expr
}

func (*Lambda) isExpr() {}

// If is `if c { a } else { b }`. Else is nil when omitted (the type
// checker has already verified Then is Unit).
type If struct {
	Base
	Cond Expr
	Then Expr
	Else Expr
}

func (*If) isExpr() {}

// Match is `match subj { pat -> e ... }` — still pattern arms, not a
// decision tree; pattern checking runs on this form and the decision
// tree is built in Core IR.
type Match struct {
	Base
	Subj Expr
	Arms []MatchArm
}

func (*Match) isExpr() {}

// MatchArm is one typed match arm.
type MatchArm struct {
	Sp    diag.Span
	Pat   Pat
	Guard Expr // nil when the arm has no `if` guard
	Body  Expr
}

// Span returns the arm's source span.
func (a MatchArm) Span() diag.Span { return a.Sp }

// Block is `{ e1; ...; en }`.
type Block struct {
	Base
	Stmts []Expr
}

func (*Block) isExpr() {}

// Let is a typed `let pat = e`; its own type is Unit.
type Let struct {
	Base
	Pat   Pat
	Value Expr
}

func (*Let) isExpr() {}

// Ignore is `ignore e`.
type Ignore struct {
	Base
	X Expr
}

func (*Ignore) isExpr() {}

// Try is `try e`.
type Try struct {
	Base
	X Expr
}

func (*Try) isExpr() {}

// GoExpr is `go { e }`.
type GoExpr struct {
	Base
	Body Expr
}

func (*GoExpr) isExpr() {}

// For is `for pat in iter { body }`.
type For struct {
	Base
	Pat  Pat
	Iter Expr
	Body Expr
}

func (*For) isExpr() {}

// SelectArm is one arm of a typed select expression.
//
//sumtype:decl
type SelectArm interface {
	Span() diag.Span
	isSelectArm()
}

// RecvArm is `x <- ch -> e`; Bind is a VarPat or WildPat.
type RecvArm struct {
	Sp   diag.Span
	Bind Pat
	Chan Expr
	Body Expr
}

func (*RecvArm) isSelectArm()      {}
func (n *RecvArm) Span() diag.Span { return n.Sp }

// SendArm is `send ch v -> e`.
type SendArm struct {
	Sp    diag.Span
	Chan  Expr
	Value Expr
	Body  Expr
}

func (*SendArm) isSelectArm()      {}
func (n *SendArm) Span() diag.Span { return n.Sp }

// DefaultArm is `default -> e`.
type DefaultArm struct {
	Sp   diag.Span
	Body Expr
}

func (*DefaultArm) isSelectArm()      {}
func (n *DefaultArm) Span() diag.Span { return n.Sp }

// SelectExpr is `select { ... }`.
type SelectExpr struct {
	Base
	Arms []SelectArm
}

func (*SelectExpr) isExpr() {}

// ListLit is `[e, ...]`; a `..rest` tail is a Spread element.
type ListLit struct {
	Base
	Elems []Expr
}

func (*ListLit) isExpr() {}

// Spread is `..e`.
type Spread struct {
	Base
	X Expr
}

func (*Spread) isExpr() {}

// TupleLit is `(e1, e2, ...)`.
type TupleLit struct {
	Base
	Elems []Expr
}

func (*TupleLit) isExpr() {}

// FieldInit is one `name = e` of a record literal or update, with the
// field resolved.
type FieldInit struct {
	Sp    diag.Span
	Name  string
	Sym   *resolve.Symbol // the field (KindField)
	Value Expr
}

// Span returns the field's source span.
func (f FieldInit) Span() diag.Span { return f.Sp }

// RecordLit is `{ name = e, ... }` or `{ base with name = e, ... }`.
// With is the record being updated — nil for literal construction.
type RecordLit struct {
	Base
	With   Expr
	Fields []FieldInit
}

func (*RecordLit) isExpr() {}

// Ascribe is a checked type ascription `e : T` — also the typed form
// of `Go.conv` (docs/spec.md section 8.4), with Conv set. The node's
// Type() is the ascribed type.
type Ascribe struct {
	Base
	X Expr
	// LentLabels marks the implicit unlabeled→labeled introduction.
	// It publishes the labeled contract at the conformance boundary but
	// must not be mistaken for the underlying expression's own type.
	LentLabels bool
	// Conv asks lowering to emit a Go conversion, for explicit `Go.conv`
	// or an implicit interface assignment. Plain ascription is a no-op.
	Conv bool
	// Iface marks an oracle-validated implicit Go interface assignment.
	Iface bool
	// Slice marks an implicit List ↔ Go.Slice boundary conversion —
	// the checker validated the pair; lowering picks the direction
	// (ConvSliceToGo / ConvSliceFromGo / ConvSliceBox) from the types.
	Slice bool
	// ChanDir marks an implicit `Chan a` → directional-channel
	// narrowing granted at a Go-call boundary (spec section 10.2).
	// `chan T` is already assignable to `chan<- T`/`<-chan T`, so
	// lowering emits the operand as-is — the explicit conversion
	// would defeat `go vet`'s sigchanyzer tracking.
	ChanDir bool
	// TryAs marks a `Go.try_as x : Option T` extraction — the comma-ok
	// assertion out of `Go.Any` (decision 2026-11-24-go-any-extraction).
	TryAs bool
	// Mat marks a literal materialization into a defined type — the
	// checker minted the `T lit` conversion (scalarLitConv and the
	// numeric literal peel). The value is the scalar itself (a
	// one-field record evaluates to its field), so the ascribe is a
	// syntactic value under the `let` restriction and a no-op for
	// the evaluator.
	Mat bool
}

func (*Ascribe) isExpr() {}
