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
}

func (*Var) isExpr() {}

// Arg is one argument of a typed application, in source (evaluation)
// order.
type Arg struct {
	Label   string    // "" for positional, else the `~label` name
	LabelSp diag.Span // span of the label, zero when positional
	Value   Expr
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
	Recv  Expr
	Sel   *syntax.Ident   // the written selector
	Sym   *resolve.Symbol // the selected member (KindField, a module member, or KindMethod)
	Index int             // field position for record/variant payloads; -1 otherwise
}

func (*MemberExpr) isExpr() {}

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
	// Conv is true when the node comes from `Go.conv`: lowering emits
	// an explicit Go conversion, while a plain ascription is a no-op
	// once checked.
	Conv bool
}

func (*Ascribe) isExpr() {}
