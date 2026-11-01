package syntax

import "github.com/podhmo/gom/internal/diag"

// Pat is a surface-AST pattern (docs/spec.md section 4.1). The
// interface is sealed: every variant is declared in this package and
// type switches over Pat must end with `default: panic("unreachable")`.
type Pat interface {
	Node
	isPat()
}

// WildPat is the wildcard `_`.
type WildPat struct {
	Sp diag.Span
}

func (*WildPat) isPat()            {}
func (n *WildPat) Span() diag.Span { return n.Sp }

// VarPat is a variable pattern `x` binding the matched value.
type VarPat struct {
	Sp   diag.Span
	Name *Ident
}

func (*VarPat) isPat()            {}
func (n *VarPat) Span() diag.Span { return n.Sp }

// LitPat is a literal pattern `0`, `"ok"`, `'a'`, `true`, `()`
// (docs/spec.md section 4.1).
type LitPat struct {
	Sp  diag.Span
	Lit *Lit
}

func (*LitPat) isPat()            {}
func (n *LitPat) Span() diag.Span { return n.Sp }

// CtorArg is one argument of a constructor pattern: a positional
// pattern or `~label: pat` for a labeled variant parameter
// (docs/spec.md section 2, item 6).
type CtorArg struct {
	Sp    diag.Span
	Label *Ident // non-nil when written with `~label`
	Pat   Pat
}

// Span returns the argument's source span.
func (a CtorArg) Span() diag.Span { return a.Sp }

// Labeled reports whether the argument was written with a `~label`.
func (a CtorArg) Labeled() bool { return a.Label != nil }

// FieldPat is one `name = pat` or punned `name` entry inside a
// `{ ... }` field pattern, used by both record patterns and the
// named-field form of constructor patterns `Let { name = n, .. }`
// (docs/spec.md section 4.1).
type FieldPat struct {
	Sp   diag.Span
	Name *Ident
	Pat  Pat // nil when punned (`{ name }` binds the variable `name`)
}

// Span returns the field's source span.
func (f FieldPat) Span() diag.Span { return f.Sp }

// Punned reports whether the field was written as bare `name`.
func (f FieldPat) Punned() bool { return f.Pat == nil }

// CtorPat is a constructor pattern: `Some x`, `Add (Num 0) r`,
// `Transfer ~to: t`, or the named-field form `Let { name = n, .. }`.
// Ctor is an *Ident or a package-qualified *Select.
type CtorPat struct {
	Sp     diag.Span
	Ctor   Expr
	Args   []CtorArg  // positional / labeled arguments
	Fields []FieldPat // non-nil when the `{ name = pat, .. }` form is used
	Rest   bool       // `..` was written in the Fields form
}

func (*CtorPat) isPat()            {}
func (n *CtorPat) Span() diag.Span { return n.Sp }

// TuplePat is a tuple pattern `(p1, p2, ...)`.
type TuplePat struct {
	Sp    diag.Span
	Elems []Pat
}

func (*TuplePat) isPat()            {}
func (n *TuplePat) Span() diag.Span { return n.Sp }

// RecordPat is a record pattern `{ name, age = 0, .. }`
// (docs/spec.md section 4.1; records behave as single-constructor
// types). Rest is true when `..` was written to skip remaining fields.
type RecordPat struct {
	Sp     diag.Span
	Fields []FieldPat
	Rest   bool
}

func (*RecordPat) isPat()            {}
func (n *RecordPat) Span() diag.Span { return n.Sp }

// ListPat is a list pattern `[]`, `[x]`, `[x, ..rest]`
// (docs/spec.md section 4.1). Rest is the pattern after `..`, or nil.
type ListPat struct {
	Sp    diag.Span
	Elems []Pat
	Rest  Pat // nil when no `..rest` is present
}

func (*ListPat) isPat()            {}
func (n *ListPat) Span() diag.Span { return n.Sp }

// OrPat is an or-pattern `Circle _ | Empty` (docs/spec.md
// section 4.1): every alternative binds the same variables at the same
// types.
type OrPat struct {
	Sp   diag.Span
	Alts []Pat
}

func (*OrPat) isPat()            {}
func (n *OrPat) Span() diag.Span { return n.Sp }

// AsPat is an as-binding `pat as name` (docs/spec.md section 4.1):
// the value is bound to Name and also decomposed by Pat.
type AsPat struct {
	Sp   diag.Span
	Pat  Pat
	Name *Ident
}

func (*AsPat) isPat()            {}
func (n *AsPat) Span() diag.Span { return n.Sp }
