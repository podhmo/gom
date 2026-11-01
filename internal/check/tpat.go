package check

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/syntax"
	"github.com/podhmo/gom/internal/types"
)

// Pat is a typed pattern: a surface pattern annotated with the type
// of the value it matches and with resolved symbols in place of
// names. Pattern checking (exhaustiveness, redundancy) runs on this
// form. The interface is sealed: every variant is declared in this
// package and type switches over Pat must end with
// `default: panic("unreachable")`.
//
//sumtype:decl
type Pat interface {
	Span() diag.Span
	Type() types.Type
	isPat()
}

// WildPat is `_`.
type WildPat struct {
	Base
}

func (*WildPat) isPat() {}

// VarPat binds the matched value to a symbol.
type VarPat struct {
	Base
	Sym *resolve.Symbol
}

func (*VarPat) isPat() {}

// LitPat is a literal pattern.
type LitPat struct {
	Base
	Kind syntax.LitKind
	Text string
}

func (*LitPat) isPat() {}

// CtorArg is one positional or `~label: pat` argument of a
// constructor pattern.
type CtorArg struct {
	Label string // "" for positional
	Pat   Pat
}

// FieldPat is one `name = pat` entry of a `{ ... }` field pattern
// with the field resolved. Punned surface fields (`{ name }`) arrive
// already expanded to `name = <var>`.
type FieldPat struct {
	Sp    diag.Span
	Name  string
	Sym   *resolve.Symbol // the field or labeled payload slot (KindField)
	Index int             // field position in the record/variant; -1 when unknown
	Pat   Pat
}

// Span returns the field's source span.
func (f FieldPat) Span() diag.Span { return f.Sp }

// CtorPat is a constructor pattern with the constructor resolved to
// its variant symbol. Name keeps the written constructor expression
// (an *Ident or a qualified *Select) for its span.
type CtorPat struct {
	Base
	Name   syntax.Expr     // the written constructor name/selector
	Ctor   *resolve.Symbol // the variant constructor (KindCtor)
	Args   []CtorArg
	Fields []FieldPat // non-nil for the `{ name = pat }` form
	Rest   bool       // `..` was written in the Fields form
}

func (*CtorPat) isPat() {}

// TuplePat is `(p1, p2, ...)`.
type TuplePat struct {
	Base
	Elems []Pat
}

func (*TuplePat) isPat() {}

// RecordPat is `{ name = pat, .. }`; Rest records whether `..` was
// written.
type RecordPat struct {
	Base
	Fields []FieldPat
	Rest   bool
}

func (*RecordPat) isPat() {}

// ListPat is `[p, ..rest]`; Rest is nil when absent.
type ListPat struct {
	Base
	Elems []Pat
	Rest  Pat
}

func (*ListPat) isPat() {}

// OrPat is `p1 | p2 | ...`.
type OrPat struct {
	Base
	Alts []Pat
}

func (*OrPat) isPat() {}

// AsPat is `pat as name`.
type AsPat struct {
	Base
	Pat Pat
	Sym *resolve.Symbol // the variable bound by `as`
}

func (*AsPat) isPat() {}
