package syntax

import "github.com/podhmo/gom/internal/diag"

// Expr is a surface-AST expression. The interface is sealed: every
// variant is declared in this package and type switches over Expr must
// end with `default: panic("unreachable")`.
type Expr interface {
	Node
	isExpr()
}

// Ident is a name token. In expression position it is a variable
// reference; binders (VarPat.Name, declaration names) and member
// selectors (Select.Sel) reuse the same node type, so an *Ident is the
// key that internal/resolve attaches a symbol to. Whether an Ident is
// a definition or a use is decided by where it sits in the tree.
type Ident struct {
	Sp   diag.Span
	Name string
}

func (*Ident) isExpr()           {}
func (n *Ident) Span() diag.Span { return n.Sp }
func (n *Ident) String() string  { return n.Name }

// LitKind classifies literals (docs/spec.md section 1.2 and 4.1).
type LitKind int

const (
	// LitInt is an integer literal (`42`, `0x1F`).
	LitInt LitKind = iota + 1
	// LitFloat is a floating-point literal (`3.14`).
	LitFloat
	// LitStr is a string literal without interpolation (`"ok"`).
	LitStr
	// LitChar is a character literal (`'a'`).
	LitChar
	// LitBool is `true` or `false`.
	LitBool
	// LitUnit is the unit literal `()`.
	LitUnit
)

// String returns the kind name for debugging ("int", "float", ...).
func (k LitKind) String() string {
	switch k {
	case LitInt:
		return "int"
	case LitFloat:
		return "float"
	case LitStr:
		return "string"
	case LitChar:
		return "char"
	case LitBool:
		return "bool"
	case LitUnit:
		return "unit"
	default:
		return "LitKind(?)"
	}
}

// Lit is a literal: `42`, `3.14`, `"s"`, `'c'`, `true`, `()`.
// Interpolated strings are StrInterp instead. Text is the raw source
// lexeme; its meaning is fixed during type checking.
type Lit struct {
	Sp   diag.Span
	Kind LitKind
	Text string
}

func (*Lit) isExpr()           {}
func (n *Lit) Span() diag.Span { return n.Sp }

// StrPart is one part of an interpolated string `"hello ${name}"`
// (docs/spec.md section 1.2): a literal text chunk or a `${expr}`
// splice.
type StrPart interface {
	Node
	isStrPart()
}

// StrText is a literal chunk of an interpolated string.
type StrText struct {
	Sp   diag.Span
	Text string
}

func (*StrText) isStrPart()        {}
func (n *StrText) Span() diag.Span { return n.Sp }

// StrExpr is a `${expr}` splice inside an interpolated string.
type StrExpr struct {
	Sp diag.Span
	X  Expr
}

func (*StrExpr) isStrPart()        {}
func (n *StrExpr) Span() diag.Span { return n.Sp }

// StrInterp is an interpolated string literal `"hello ${name}"`.
type StrInterp struct {
	Sp    diag.Span
	Parts []StrPart
}

func (*StrInterp) isExpr()           {}
func (n *StrInterp) Span() diag.Span { return n.Sp }

// Arg is one argument of an application: a positional argument, a
// labeled argument `~label: e`, or a punned label `~label` (sugar for
// `~label: label`, docs/spec.md section 2).
type Arg struct {
	Sp     diag.Span // covers the label through the value
	Label  *Ident    // non-nil when written with `~label`
	Value  Expr      // the argument expression; the synthesized name when Punned
	Punned bool      // written as bare `~label` (Value holds the name's Ident)
}

// Span returns the argument's source span.
func (a Arg) Span() diag.Span { return a.Sp }

// Labeled reports whether the argument was written with a `~label`.
func (a Arg) Labeled() bool { return a.Label != nil }

// Apply is function application by juxtaposition, `f x y`
// (docs/spec.md section 1.3), with the arguments kept in source order.
// Arguments may be positional, labeled (`~to: e`, `~to`), or — for Go
// calls — a tuple literal in parentheses: `fmt.Println(a, b)` is Apply
// of `fmt.Println` to the tuple `(a, b)` (docs/spec.md section 8.1).
// Whether the callee is a Gom function, a Go function or an unknown
// function value is decided at type checking and recorded as
// structured call info on the typed Apply node.
type Apply struct {
	Sp   diag.Span
	Fn   Expr
	Args []Arg
}

func (*Apply) isExpr()           {}
func (n *Apply) Span() diag.Span { return n.Sp }

// Select is member access `recv.sel`: field access `p.name`, module
// member access `List.map` / `fmt.Println`, and method selection
// `buf.WriteString`. Which of those it is depends on what Recv
// resolves to.
type Select struct {
	Sp   diag.Span
	Recv Expr
	Sel  *Ident
}

func (*Select) isExpr()           {}
func (n *Select) Span() diag.Span { return n.Sp }

// UnOp is a unary operator.
type UnOp int

const (
	// UnNeg is numeric negation `-x`.
	UnNeg UnOp = iota + 1
	// UnNot is boolean negation `!x`.
	UnNot
)

// String returns the source spelling of the operator.
func (o UnOp) String() string {
	switch o {
	case UnNeg:
		return "-"
	case UnNot:
		return "!"
	default:
		return "UnOp(?)"
	}
}

// Unary is a unary operation `op x` such as `(-1)`.
type Unary struct {
	Sp diag.Span
	Op UnOp
	X  Expr
}

func (*Unary) isExpr()           {}
func (n *Unary) Span() diag.Span { return n.Sp }

// BinOp is a binary operator, including the sugared pipeline and
// composition operators (docs/spec.md section 1.4) that desugar into
// ordinary applications at lowering.
type BinOp int

const (
	// BinAdd is `+`.
	BinAdd BinOp = iota + 1
	// BinSub is `-`.
	BinSub
	// BinMul is `*`.
	BinMul
	// BinDiv is `/`.
	BinDiv
	// BinMod is `%`.
	BinMod
	// BinEq is `==`.
	BinEq
	// BinNe is `!=`.
	BinNe
	// BinLt is `<`.
	BinLt
	// BinLe is `<=`.
	BinLe
	// BinGt is `>`.
	BinGt
	// BinGe is `>=`.
	BinGe
	// BinAnd is `&&`.
	BinAnd
	// BinOr is `||`.
	BinOr
	// BinPipeFwd is `|>`: `x |> f` is `f x`.
	BinPipeFwd
	// BinPipeBack is `<|`: `f <| x` is `f x`.
	BinPipeBack
	// BinPipeTry is `|?>`: the Result-carrying pipe of spec section 9.
	BinPipeTry
	// BinComposeFwd is `>>`: `f >> g` is `fn x -> g (f x)`.
	BinComposeFwd
	// BinComposeBack is `<<`: `g << f` is `fn x -> g (f x)`.
	BinComposeBack
)

// String returns the source spelling of the operator.
func (o BinOp) String() string {
	switch o {
	case BinAdd:
		return "+"
	case BinSub:
		return "-"
	case BinMul:
		return "*"
	case BinDiv:
		return "/"
	case BinMod:
		return "%"
	case BinEq:
		return "=="
	case BinNe:
		return "!="
	case BinLt:
		return "<"
	case BinLe:
		return "<="
	case BinGt:
		return ">"
	case BinGe:
		return ">="
	case BinAnd:
		return "&&"
	case BinOr:
		return "||"
	case BinPipeFwd:
		return "|>"
	case BinPipeBack:
		return "<|"
	case BinPipeTry:
		return "|?>"
	case BinComposeFwd:
		return ">>"
	case BinComposeBack:
		return "<<"
	default:
		return "BinOp(?)"
	}
}

// Sugared reports whether the operator is one of the sugared
// pipeline/composition forms (`|>`, `<|`, `|?>`, `>>`, `<<`) that are
// lowered to applications rather than primitive operations.
func (o BinOp) Sugared() bool {
	switch o {
	case BinPipeFwd, BinPipeBack, BinPipeTry, BinComposeFwd, BinComposeBack:
		return true
	default:
		return false
	}
}

// Binary is a binary operation `x op y`.
type Binary struct {
	Sp   diag.Span
	Op   BinOp
	X, Y Expr
}

func (*Binary) isExpr()           {}
func (n *Binary) Span() diag.Span { return n.Sp }

// Hole is the `_` placeholder usable as one argument of a Go call
// (docs/spec.md section 8.2). Anywhere else it is a compile error.
type Hole struct {
	Sp diag.Span
}

func (*Hole) isExpr()           {}
func (n *Hole) Span() diag.Span { return n.Sp }

// Lambda is an anonymous function `fn a b -> body` (docs/spec.md
// section 1.2). The body extends as far right as possible.
type Lambda struct {
	Sp     diag.Span
	Params []Param
	Body   Expr
}

func (*Lambda) isExpr()           {}
func (n *Lambda) Span() diag.Span { return n.Sp }

// If is a conditional expression `if c { a } else { b }`
// (docs/spec.md section 1.1). Else is nil when omitted, which is only
// allowed when Then has type Unit.
type If struct {
	Sp   diag.Span
	Cond Expr
	Then Expr
	Else Expr // nil when omitted
}

func (*If) isExpr()           {}
func (n *If) Span() diag.Span { return n.Sp }

// Match is a pattern-matching expression `match subj { pat -> e ... }`
// (docs/spec.md section 4).
type Match struct {
	Sp   diag.Span
	Subj Expr
	Arms []MatchArm
}

func (*Match) isExpr()           {}
func (n *Match) Span() diag.Span { return n.Sp }

// MatchArm is one arm `pat -> e` or `pat if guard -> e`.
type MatchArm struct {
	Sp    diag.Span
	Pat   Pat
	Guard Expr // nil when the arm has no `if` guard
	Body  Expr
}

// Span returns the arm's source span.
func (a MatchArm) Span() diag.Span { return a.Sp }

// Block is a braced sequence of expressions `{ e1; e2; ...; en }`
// whose value is the last expression's (docs/spec.md section 1.1). Newlines
// inside braces separate expressions; Let and Ignore are expressions
// here so Stmts is a plain []Expr.
type Block struct {
	Sp    diag.Span
	Stmts []Expr
}

func (*Block) isExpr()           {}
func (n *Block) Span() diag.Span { return n.Sp }

// Let is a block-local binding `let pat = e` or `let pat : T = e`.
// The left side must be an irrefutable pattern (docs/spec.md
// section 1.1, item 5).
type Let struct {
	Sp    diag.Span
	Pat   Pat
	Type  TypeExpr // nil when unannotated
	Value Expr
}

func (*Let) isExpr()           {}
func (n *Let) Span() diag.Span { return n.Sp }

// Ignore is the explicit discard `ignore e` (docs/spec.md section 1.1,
// item 2): X is evaluated and its value thrown away.
type Ignore struct {
	Sp diag.Span
	X  Expr
}

func (*Ignore) isExpr()           {}
func (n *Ignore) Span() diag.Span { return n.Sp }

// Try is the early-escape `try e` (docs/spec.md section 6): if e is
// `Err x` the enclosing function returns `Err x`, else e's Ok value.
type Try struct {
	Sp diag.Span
	X  Expr
}

func (*Try) isExpr()           {}
func (n *Try) Span() diag.Span { return n.Sp }

// GoExpr is `go { e }`, evaluating Body on a new goroutine
// (docs/spec.md section 10).
type GoExpr struct {
	Sp   diag.Span
	Body Expr // a Block
}

func (*GoExpr) isExpr()           {}
func (n *GoExpr) Span() diag.Span { return n.Sp }

// For is the loop sugar `for pat in iter { body } : Unit`
// (docs/spec.md section 1.1, item 7, and section 10 for channels).
type For struct {
	Sp   diag.Span
	Pat  Pat
	Iter Expr
	Body Expr // a Block
}

func (*For) isExpr()           {}
func (n *For) Span() diag.Span { return n.Sp }

// SelectArm is one arm of a select expression (docs/spec.md
// section 10): a receive `x <- ch -> e`, a send `send ch v -> e`, or
// `default -> e`.
type SelectArm interface {
	Node
	isSelectArm()
}

// RecvArm is a receive arm `x <- ch -> e`. Bind is a variable or `_`
// only — the bound value has type `Option a` (docs/spec.md section 10,
// item 5).
type RecvArm struct {
	Sp   diag.Span
	Bind Pat
	Chan Expr
	Body Expr
}

func (*RecvArm) isSelectArm()      {}
func (n *RecvArm) Span() diag.Span { return n.Sp }

// SendArm is a send arm `send ch v -> e`.
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

// SelectExpr is a `select { ... }` expression over channel arms
// (docs/spec.md section 10). All arms return the same type.
type SelectExpr struct {
	Sp   diag.Span
	Arms []SelectArm
}

func (*SelectExpr) isExpr()           {}
func (n *SelectExpr) Span() diag.Span { return n.Sp }

// ListLit is a list literal `[e, ...]`. A `..rest` tail is written as
// a Spread element (docs/spec.md section 4.1 shows the pattern form).
type ListLit struct {
	Sp    diag.Span
	Elems []Expr
}

func (*ListLit) isExpr()           {}
func (n *ListLit) Span() diag.Span { return n.Sp }

// Spread is `..e`: the list tail inside a list literal `[x, ..rest]`,
// or slice expansion in a Go call argument `f(..xs)` (docs/spec.md
// section 8.1, item 4).
type Spread struct {
	Sp diag.Span
	X  Expr
}

func (*Spread) isExpr()           {}
func (n *Spread) Span() diag.Span { return n.Sp }

// TupleLit is a tuple literal `(e1, e2, ...)` — at least two elements
// (a parenthesized single expression is not a TupleLit).
type TupleLit struct {
	Sp    diag.Span
	Elems []Expr
}

func (*TupleLit) isExpr()           {}
func (n *TupleLit) Span() diag.Span { return n.Sp }

// Ascribe is a type ascription `e : T`. The only form the spec names
// is `Go.conv e : T` — the explicit conversion of spec section 8.4 —
// but the node is general: whether `e : T` ascription is a general
// form or restricted to Go.conv is a grammar question (D-1).
type Ascribe struct {
	Sp   diag.Span
	X    Expr
	Type TypeExpr
}

func (*Ascribe) isExpr()           {}
func (n *Ascribe) Span() diag.Span { return n.Sp }

// FieldInit is one `name = e` field of a record literal or record
// update.
type FieldInit struct {
	Sp    diag.Span
	Name  *Ident
	Value Expr
}

// Span returns the field's source span.
func (f FieldInit) Span() diag.Span { return f.Sp }

// RecordLit is a record literal `{ name = e, ... }` or a record update
// `{ base with name = e, ... }` (docs/spec.md section 1.2). Base is nil
// for literal construction.
type RecordLit struct {
	Sp     diag.Span
	Base   Expr // nil when constructing a new record
	Fields []FieldInit
}

func (*RecordLit) isExpr()           {}
func (n *RecordLit) Span() diag.Span { return n.Sp }
