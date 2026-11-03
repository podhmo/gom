package core

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Decl is a top-level declaration in a Core IR file. The interface
// is sealed: every variant is declared in this package and type
// switches over Decl must end with `default: panic("unreachable")`.
// Imports live on File, not as decls.
//
//sumtype:decl
type Decl interface {
	Span() diag.Span
	isDecl()
}

// FuncDecl is a top-level function definition — emitted at its
// declared arity as `func f(A1, ..., Am) R'` where R' may itself be
// a flattened func type (P0-2).
type FuncDecl struct {
	Sp  diag.Span
	Sym *resolve.Symbol // KindFunc, or KindVar for ValueAlias; TestOnly marks @test definitions
	// ValueAlias marks a static function-valued let emitted as a named
	// arity-one wrapper. Sym keeps KindVar and references remain Var /
	// CallFunc, preserving the canonical function-value convention.
	ValueAlias bool
	// IsEntry marks the program entry — `fn main () -> Unit` in
	// `package main` — decided by lowering (docs/design.md section
	// 2.1: codegen consumes the flag, it does not re-derive the rule
	// from the package and symbol names). Codegen emits it as
	// `func main()`: no parameters, no result.
	IsEntry bool
	// TypeParams are the declared type parameters in source order —
	// the order the type's instantiation arguments are given in.
	// Nil while the definition is monomorphic.
	TypeParams []*types.Var
	Fn         *Func
}

func (*FuncDecl) isDecl()           {}
func (n *FuncDecl) Span() diag.Span { return n.Sp }

// VarDecl is a top-level `let` — a package-level value. Its body is
// a Body because the initializer may need statements; codegen emits
// `var name T = func() T { ... }()` when the initializer is not a
// single expression.
type VarDecl struct {
	Sp   diag.Span
	Sym  *resolve.Symbol // KindVar
	Body *Body
}

func (*VarDecl) isDecl()           {}
func (n *VarDecl) Span() diag.Span { return n.Sp }

// TestDecl is a `test "name" { ... }` block, split out at lowering so
// codegen can route it to _test.go (docs/design.md section 4.8). The
// block is a Func — a function boundary for escapes, with no params.
type TestDecl struct {
	Sp     diag.Span
	Name   string
	NameSp diag.Span
	Fn     *Func
}

func (*TestDecl) isDecl()           {}
func (n *TestDecl) Span() diag.Span { return n.Sp }

// TypeDecl is a type declaration — codegen emits the Go
// representation of its body: the sealed interface and variant
// structs for a sum type (P0-1), the struct for a record, the alias
// target for an alias. Abstract declarations produce no code.
type TypeDecl struct {
	Sp  diag.Span
	Sym *resolve.Symbol // KindType
	// TypeParams are the declared type parameters in source order —
	// `type Result a e` yields [a, e]. Nil for monomorphic types.
	TypeParams []*types.Var
	Body       TypeBody
}

func (*TypeDecl) isDecl()           {}
func (n *TypeDecl) Span() diag.Span { return n.Sp }

// TypeBody is the body of a TypeDecl. The interface is sealed: every
// variant is declared in this package and type switches over
// TypeBody must end with `default: panic("unreachable")`.
//
//sumtype:decl
type TypeBody interface {
	Span() diag.Span
	isTypeBody()
}

// TField is one payload slot of a variant or one field of a record.
type TField struct {
	Sp    diag.Span
	Name  string          // field name; "" for positional payload
	Label string          // non-empty for `~name` labeled parameters
	Index int             // position in the constructor / record
	Sym   *resolve.Symbol // KindField; nil for unnamed positional payload
	Type  types.Type
}

// Span returns the field's source span.
func (f TField) Span() diag.Span { return f.Sp }

// TVariant is one constructor of a sum type.
type TVariant struct {
	Sp     diag.Span
	Sym    *resolve.Symbol // KindCtor
	Fields []TField
}

// TSum is a sum-type body — one variant per constructor, emitted as
// the sealed interface plus variant structs of P0-1 (the
// parameter-in-marker-signature form for generic ADTs). The builtin
// Result is not declared through TSum — its flat2 representation is
// fixed.
type TSum struct {
	Sp       diag.Span
	Variants []TVariant
}

func (*TSum) isTypeBody()       {}
func (n *TSum) Span() diag.Span { return n.Sp }

// TRecord is a record-type body — emitted as a struct with named
// fields.
type TRecord struct {
	Sp     diag.Span
	Fields []TField
}

func (*TRecord) isTypeBody()       {}
func (n *TRecord) Span() diag.Span { return n.Sp }

// TAlias is a resolved type alias — emitted as a Go type alias or
// defined type pointing at Type.
type TAlias struct {
	Sp   diag.Span
	Type types.Type
}

func (*TAlias) isTypeBody()       {}
func (n *TAlias) Span() diag.Span { return n.Sp }

// TAbstract is an abstract type (name only — reachable only from
// .gomi signatures). It produces no code.
type TAbstract struct {
	Sp diag.Span
}

func (*TAbstract) isTypeBody()       {}
func (n *TAbstract) Span() diag.Span { return n.Sp }

// Import is one resolved import clause kept on the File for
// reference; the symbol is the bound module (KindModule).
type Import struct {
	Sp   diag.Span
	Path string
	IsGo bool
	Sym  *resolve.Symbol
}

// File is a Core IR file — the unit Validate checks and codegen
// consumes.
type File struct {
	Sp      diag.Span
	Package string
	PkgSp   diag.Span
	Imports []Import
	Decls   []Decl
	// EqDecls are the generated structural comparators the file's
	// Equal nodes and CallBuiltin{assert_eq} plans call — one per
	// distinct composite type lowering compared. They are emitted
	// after Decls; see EqDecl for why they are not Decls.
	EqDecls []*EqDecl
	// ShowDecls are the generated display functions the file's
	// CallBuiltin{assert_eq} plans render values through — one per
	// distinct assert_eq operand type (docs/design.md section 4.8).
	// They are emitted into the _test.go half after the test
	// declarations; see ShowDecl for why they are not Decls.
	ShowDecls []*ShowDecl
}

// Span returns the whole-file span.
func (f *File) Span() diag.Span { return f.Sp }
