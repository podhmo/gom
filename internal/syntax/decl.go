package syntax

import "github.com/podhmo/gom/internal/diag"

// Decl is a top-level declaration of a .gom file (or the signature
// subset allowed in .gomi files, docs/spec.md section 7). The interface
// is sealed: every variant is declared in this package and type
// switches over Decl must end with `default: panic("unreachable")`.
//
//sumtype:decl
type Decl interface {
	Node
	isDecl()
}

// Param is one parameter of a function or lambda: `x`, `(x: T)`,
// `(~label: T)`, or a pattern `(a, b)` (docs/spec.md section 2 and
// 4.1 — parameters are irrefutable patterns).
type Param struct {
	Sp    diag.Span
	Label *Ident   // non-nil for a labeled parameter `~name`
	Pat   Pat      // binding pattern
	Type  TypeExpr // nil when unannotated
}

// Span returns the parameter's source span.
func (p Param) Span() diag.Span { return p.Sp }

// Labeled reports whether the parameter was declared `~name`.
func (p Param) Labeled() bool { return p.Label != nil }

// FnDecl is a function definition `fn name (p: T) (~l: U) -> R = body`
// or, in .gomi and extern blocks, a signature without a body.
type FnDecl struct {
	Sp       diag.Span
	Doc      *Comment // attached `///` doc comment, or nil
	TestOnly bool     // declared with `@test` (docs/spec.md section 11)
	TailRec  bool     // declared with `@tailrec` (docs/spec.md section 1)
	Name     *Ident
	Params   []Param
	Ret      TypeExpr // nil when unannotated
	Body     Expr     // nil in .gomi signatures
}

func (*FnDecl) isDecl()           {}
func (n *FnDecl) Span() diag.Span { return n.Sp }

// LetDecl is a top-level binding `let pat : T = e`; in .gomi it is the
// value signature `let name : T` (docs/spec.md section 7).
type LetDecl struct {
	Sp       diag.Span
	Doc      *Comment
	TestOnly bool
	Pat      Pat
	Type     TypeExpr // nil when unannotated
	Value    Expr     // nil in .gomi signatures
}

func (*LetDecl) isDecl()           {}
func (n *LetDecl) Span() diag.Span { return n.Sp }

// TypeBody is the right-hand side of `type X = ...` — or `type X`
// alone for an abstract type.
//
//sumtype:decl
type TypeBody interface {
	Node
	isTypeBody()
}

// SumBody is a sum-type body `| V1 ... | V2 ...` (docs/spec.md
// section 3.2).
type SumBody struct {
	Sp       diag.Span
	Variants []Variant
}

func (*SumBody) isTypeBody()       {}
func (n *SumBody) Span() diag.Span { return n.Sp }

// Variant is one constructor of a sum type, e.g.
// `| Rect (w: Float) (h: Float)` or `| Transfer (~to: Account)`
// (docs/spec.md section 3.2 and section 2, item 6).
type Variant struct {
	Sp     diag.Span
	Doc    *Comment
	Name   *Ident
	Params []Field // payload parameters; positional, named or labeled
}

// Span returns the variant's source span.
func (v *Variant) Span() diag.Span { return v.Sp }

// Field is a payload slot of an ADT variant or a record field:
// positional `T`, named `(name: T)`, or a labeled constructor argument
// `(~name: T)`. For a labeled argument, Label carries the name and
// Name is nil.
type Field struct {
	Sp    diag.Span
	Name  *Ident // field name; nil for positional payloads
	Label *Ident // non-nil for `~name` labeled parameters
	Type  TypeExpr
}

// Span returns the field's source span.
func (f Field) Span() diag.Span { return f.Sp }

// ParamName returns the name used for the parameter at call sites:
// the label when present, else the field name ("" for positional).
func (f Field) ParamName() string {
	switch {
	case f.Label != nil:
		return f.Label.Name
	case f.Name != nil:
		return f.Name.Name
	default:
		return ""
	}
}

// RecordBody is a record-type body `{ name: T, ... }`.
type RecordBody struct {
	Sp     diag.Span
	Fields []Field
}

func (*RecordBody) isTypeBody()       {}
func (n *RecordBody) Span() diag.Span { return n.Sp }

// AliasBody is a type alias `type X = T`.
type AliasBody struct {
	Sp   diag.Span
	Type TypeExpr
}

func (*AliasBody) isTypeBody()       {}
func (n *AliasBody) Span() diag.Span { return n.Sp }

// AbstractBody is a bare `type X` — an abstract type whose
// representation is hidden (docs/spec.md section 7, `.gomi`).
type AbstractBody struct {
	Sp diag.Span
}

func (*AbstractBody) isTypeBody()       {}
func (n *AbstractBody) Span() diag.Span { return n.Sp }

// TypeDecl is a type declaration `type Name a b = body`
// (docs/spec.md section 3.2) or the bare `type Name` of an abstract
// type in .gomi.
type TypeDecl struct {
	Sp     diag.Span
	Doc    *Comment
	Name   *Ident
	Params []*Ident // declared type parameters (`type Option a = ...`)
	Body   TypeBody
}

func (*TypeDecl) isDecl()           {}
func (n *TypeDecl) Span() diag.Span { return n.Sp }

// ImportDecl is `import "path"` for a Gom package or
// `import go "path"` for a Go package (docs/spec.md section 5 and 8).
type ImportDecl struct {
	Sp     diag.Span
	Go     bool      // written with the `go` keyword
	Path   string    // import path (literal contents, uninterpreted)
	PathSp diag.Span // span of the path literal
	Name   *Ident    // bound package name: the explicit alias, or synthesized
	// from the last path element by the parser
	Alias bool // Name was written explicitly as an alias
}

func (*ImportDecl) isDecl()           {}
func (n *ImportDecl) Span() diag.Span { return n.Sp }

// ExternEntry is one entry inside an `extern go` block. Currently only
// function signatures exist; the family is sealed so new entry kinds
// (conversion rules, types — docs/spec.md section 8.4) force type
// switches to be revisited.
//
//sumtype:decl
type ExternEntry interface {
	Node
	isExternEntry()
}

// ExternFn is a function signature `fn Name (p: T) -> R` inside an
// extern block.
type ExternFn struct {
	Sp     diag.Span
	Doc    *Comment
	Name   *Ident
	Params []Param
	Ret    TypeExpr
}

func (*ExternFn) isExternEntry()    {}
func (n *ExternFn) Span() diag.Span { return n.Sp }

// ExternDecl is `extern go "pkg" { ... }` — overrides the derived Go
// bindings for one package (docs/spec.md section 8.4).
type ExternDecl struct {
	Sp      diag.Span
	Path    string
	PathSp  diag.Span
	Entries []ExternEntry
}

func (*ExternDecl) isDecl()           {}
func (n *ExternDecl) Span() diag.Span { return n.Sp }

// TestDecl is a `test "name" { ... }` block (docs/spec.md
// section 11). The body is a Block whose type is Unit or
// Result Unit e.
type TestDecl struct {
	Sp     diag.Span
	Name   string    // test name (string literal contents)
	NameSp diag.Span // span of the name literal
	Body   Expr      // a Block
}

func (*TestDecl) isDecl()           {}
func (n *TestDecl) Span() diag.Span { return n.Sp }
