package check

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/resolve"
	"github.com/podhmo/gom/internal/types"
)

// Decl is a typed top-level declaration. The interface is sealed:
// every variant is declared in this package and type switches over
// Decl must end with `default: panic("unreachable")`.
//
// Surface-only declarations are gone by this stage: import clauses
// become File.Imports and extern blocks have already been consumed by
// name resolution.
//
//sumtype:decl
type Decl interface {
	Span() diag.Span
	isDecl()
}

// FnDecl is a typed function definition.
type FnDecl struct {
	Sp     diag.Span
	Sym    *resolve.Symbol // the bound function (KindFunc)
	Sig    *types.Func     // declared or inferred signature
	Params []Param
	Body   Expr
	// TailRec marks a declaration written with `@tailrec`: the
	// lowering-stage requirement that every self-recursive call sits
	// in tail position (docs/spec.md section 1). Only declared `fn`s
	// carry it — a `let` flipped to a function declaration does not.
	TailRec bool
	// TypeParams are the declaration's quantified type variables in
	// source occurrence order — the rigid variables of Sig for a
	// `fn`, or the metas generalization bound to fresh variables for
	// a `let f = <lambda>` / value `let` (P2-1, decisions
	// 2026-11-02-let-polymorphism-emission). A generalized let that
	// is not a lambda has nil Params and Sig of `() -> T` — the
	// "thunk" shape lowering emits as `func x[T...]() T`.
	TypeParams []*types.Var
	// Thunk marks the `() -> T` shape as a generalized value-let
	// (`let x = <value>`), not a declared niladic function:
	// references denote the *value* and emit the call `x[T]()`,
	// while `fn f () -> T` stays an ordinary function whose value
	// position is `f`. The shape alone cannot tell the two apart.
	Thunk bool
}

func (*FnDecl) isDecl()           {}
func (n *FnDecl) Span() diag.Span { return n.Sp }

// LetDecl is a typed top-level `let pat = e`.
type LetDecl struct {
	Sp    diag.Span
	Pat   Pat
	Value Expr
}

func (*LetDecl) isDecl()           {}
func (n *LetDecl) Span() diag.Span { return n.Sp }

// TypeBody is the typed body of a type declaration, with every field
// type resolved to a types.Type.
//
//sumtype:decl
type TypeBody interface {
	Span() diag.Span
	isTypeBody()
}

// TField is a resolved payload slot / record field.
type TField struct {
	Sp    diag.Span       // the field's source span
	Name  string          // field name; "" for positional payload
	Label string          // non-empty for `~name` labeled parameters
	Index int             // position in the constructor/record
	Sym   *resolve.Symbol // the field (KindField); nil for unnamed positional payload
	Type  types.Type
}

// Variant is one resolved constructor of a sum type.
type Variant struct {
	Sp     diag.Span
	Sym    *resolve.Symbol // the constructor (KindCtor)
	Fields []TField
}

// SumType is a resolved sum-type body.
type SumType struct {
	Sp       diag.Span
	Variants []Variant
}

func (*SumType) isTypeBody()       {}
func (n *SumType) Span() diag.Span { return n.Sp }

// RecordType is a resolved record-type body.
type RecordType struct {
	Sp     diag.Span
	Fields []TField
}

func (*RecordType) isTypeBody()       {}
func (n *RecordType) Span() diag.Span { return n.Sp }

// AliasType is a resolved type alias.
type AliasType struct {
	Sp   diag.Span
	Type types.Type
}

func (*AliasType) isTypeBody()       {}
func (n *AliasType) Span() diag.Span { return n.Sp }

// AbstractType is an abstract type (name only, .gomi).
type AbstractType struct {
	Sp diag.Span
}

func (*AbstractType) isTypeBody()       {}
func (n *AbstractType) Span() diag.Span { return n.Sp }

// TypeDecl is a typed type declaration.
type TypeDecl struct {
	Sp  diag.Span
	Sym *resolve.Symbol // the declared type (KindType)
	// TypeParams are the declared type parameters in source order —
	// `type Result a e` yields [a, e]. It is the order Con.Args for
	// this type are given in. Nil for monomorphic declarations.
	TypeParams []*types.Var
	Body       TypeBody
}

func (*TypeDecl) isDecl()           {}
func (n *TypeDecl) Span() diag.Span { return n.Sp }

// TestDecl is a typed `test "name" { ... }` block. It stays in the
// typed file — it is type- and pattern-checked on every build and
// only split out at lowering (docs/spec.md section 11).
type TestDecl struct {
	Sp     diag.Span
	Name   string
	NameSp diag.Span
	Body   Expr // a Block
}

func (*TestDecl) isDecl()           {}
func (n *TestDecl) Span() diag.Span { return n.Sp }

// Import is one resolved import clause kept on the typed File for
// reference; the symbol is the bound module (KindModule).
type Import struct {
	Sp   diag.Span
	Path string
	IsGo bool
	Sym  *resolve.Symbol
}

// File is a fully type-checked source file: the typed AST root
// produced by phase 4.
type File struct {
	Sp      diag.Span
	Package string
	PkgSp   diag.Span // span of the package name
	Imports []Import
	Decls   []Decl
}

// Span returns the whole-file span.
func (f *File) Span() diag.Span { return f.Sp }
