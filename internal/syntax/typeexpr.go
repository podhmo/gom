package syntax

import "github.com/podhmo/gom/internal/diag"

// TypeExpr is a surface-AST type expression (docs/spec.md section 3):
// the syntax used in annotations, type declarations and .gomi
// signatures. Type application is juxtaposed like term-level
// application (`List Int`, `Result Unit TxError`). The interface is
// sealed: every variant is declared in this package and type switches
// over TypeExpr must end with `default: panic("unreachable")`.
//
//sumtype:decl
type TypeExpr interface {
	Node
	isTypeExpr()
}

// TypeVar is a type variable `a` — lowercase by convention
// (docs/spec.md section 3).
type TypeVar struct {
	Sp   diag.Span
	Name *Ident
}

func (*TypeVar) isTypeExpr()       {}
func (n *TypeVar) Span() diag.Span { return n.Sp }

// TypeName is a named type: `Int`, `Shape`, or a package-qualified
// `io.Reader` / `Go.Map`. Qual is nil for an unqualified name.
type TypeName struct {
	Sp   diag.Span
	Qual *Ident // package qualifier; nil when unqualified
	Name *Ident
}

func (*TypeName) isTypeExpr()       {}
func (n *TypeName) Span() diag.Span { return n.Sp }

// TypeApp is type application by juxtaposition `Ctor a b ...`
// (docs/spec.md section 3): `List Int`, `Result Unit TxError`,
// `Option (List Int)`.
type TypeApp struct {
	Sp   diag.Span
	Ctor TypeExpr
	Args []TypeExpr
}

func (*TypeApp) isTypeExpr()       {}
func (n *TypeApp) Span() diag.Span { return n.Sp }

// TypeParam is one parameter of a function type: a type `T` or a
// labeled parameter `~name: T` (docs/spec.md section 2.1).
type TypeParam struct {
	Sp    diag.Span
	Label *Ident // non-nil for a labeled parameter `~name`
	Type  TypeExpr
}

// Span returns the parameter's source span.
func (p TypeParam) Span() diag.Span { return p.Sp }

// Labeled reports whether the parameter carries a `~name` label.
func (p TypeParam) Labeled() bool { return p.Label != nil }

// TypeFn is a function type `p1 -> p2 -> ... -> r` with the parameter
// list kept flat (labels included, e.g.
// `Account -> ~to: Account -> Result Unit TxError`).
type TypeFn struct {
	Sp     diag.Span
	Params []TypeParam
	Ret    TypeExpr
}

func (*TypeFn) isTypeExpr()       {}
func (n *TypeFn) Span() diag.Span { return n.Sp }

// TypeTuple is a tuple type `(A, B, ...)`.
type TypeTuple struct {
	Sp    diag.Span
	Elems []TypeExpr
}

func (*TypeTuple) isTypeExpr()       {}
func (n *TypeTuple) Span() diag.Span { return n.Sp }

// TypeRecord is a record type `{ name: T, ... }`.
type TypeRecord struct {
	Sp     diag.Span
	Fields []Field
}

func (*TypeRecord) isTypeExpr()       {}
func (n *TypeRecord) Span() diag.Span { return n.Sp }

// mentionsTypeVar reports whether t contains a type variable. The
// .gomi grammar applies it to constant signatures: a `let` member
// with a quantified variable would publish a generalized let, whose
// implementation emits a thunk the module-member surface cannot
// express (internal/stdlib, internal/gomimport).
func mentionsTypeVar(t TypeExpr) bool {
	switch t := t.(type) {
	case nil:
		return false
	case *TypeVar:
		return true
	case *TypeName:
		return false
	case *TypeApp:
		if mentionsTypeVar(t.Ctor) {
			return true
		}
		for _, a := range t.Args {
			if mentionsTypeVar(a) {
				return true
			}
		}
		return false
	case *TypeFn:
		for _, prm := range t.Params {
			if mentionsTypeVar(prm.Type) {
				return true
			}
		}
		return mentionsTypeVar(t.Ret)
	case *TypeTuple:
		for _, e := range t.Elems {
			if mentionsTypeVar(e) {
				return true
			}
		}
		return false
	case *TypeRecord:
		for _, f := range t.Fields {
			if mentionsTypeVar(f.Type) {
				return true
			}
		}
		return false
	default:
		panic("unreachable")
	}
}
