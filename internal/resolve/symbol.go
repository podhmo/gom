package resolve

import (
	"github.com/podhmo/gom/internal/diag"
	"github.com/podhmo/gom/internal/syntax"
)

// Kind classifies what a Symbol names.
type Kind int

const (
	// KindVar is a value binding: a let binding, a parameter, or a
	// pattern variable.
	KindVar Kind = iota + 1
	// KindFunc is a top-level function.
	KindFunc
	// KindType is a declared type.
	KindType
	// KindCtor is an ADT variant constructor.
	KindCtor
	// KindField is a record field or a named variant payload slot.
	KindField
	// KindModule is an imported package (Gom or Go).
	KindModule
	// KindTypeVar is a bound type variable (`type Option a` binds a).
	KindTypeVar
	// KindMethod is a Go method.
	KindMethod
	// KindBuiltin is a compiler-provided name such as assert_eq or todo.
	KindBuiltin
)

// String returns the kind name for debugging.
func (k Kind) String() string {
	switch k {
	case KindVar:
		return "var"
	case KindFunc:
		return "func"
	case KindType:
		return "type"
	case KindCtor:
		return "ctor"
	case KindField:
		return "field"
	case KindModule:
		return "module"
	case KindTypeVar:
		return "typevar"
	case KindMethod:
		return "method"
	case KindBuiltin:
		return "builtin"
	default:
		return "Kind(?)"
	}
}

// Symbol is a resolved name binding (docs/design.md section 2:
// "every identifier is bound to a symbol"). Identity is the pointer:
// each binder owns a distinct *Symbol and all its use sites point at
// it, so later phases never compare names.
type Symbol struct {
	Name     string // the declared name
	Kind     Kind   // what the symbol names
	Def      syntax.Node
	IsGo     bool // defined by a Go package (import go)
	TestOnly bool // lives in the test-only scope (@test, docs/spec.md section 11)

	// Thunk marks a member loaded through a .gomi member table whose
	// `let name : T` signature quantified type variables: the member
	// denotes a generalized value, types at T, and emits the call
	// `name[T]()`. It carries the same oracle check's local
	// `thunks` map and lower's `thunks` map hold for a
	// source-level generalized let — those maps key on symbols
	// minted by this package's own declarations, so the flag is the
	// import-crossing carrier (check.go's valOf, lower's
	// funcRefOrFlatten).
	Thunk bool

	// Hidden, when non-empty, marks a loaded Go symbol whose
	// signature cannot be represented in Gom (spec section 8.5):
	// the member still binds — references resolve to it — but the
	// checker reports E0501 carrying this reason at every use site.
	// It is empty for every symbol defined by Gom source.
	Hidden string
	// HiddenCode overrides the diagnostic code hiddenDiag emits —
	// "" means the default E0501. The loader sets E0506 for a
	// generic function whose constraints fall outside the
	// any/comparable callable set.
	HiddenCode string

	// Package is the path of the package that defines the symbol — a
	// Go import path or a Gom package path. It is "" for symbols
	// defined in the file being compiled (locals and top-level names
	// alike) and for builtin-scope names (Option, assert_eq, ...),
	// matching the reserved qualifier of types.Con.
	Package string

	// Escapes is the per-parameter escape flags of a Gom function
	// (spec 6.1, SC-2): Escapes[i] is true when the i-th flattened
	// parameter may retain its argument. check's escape analysis
	// fills it for locally declared functions; the .gomi loader
	// fills it from `//gom:noescape` directives. An absent or short
	// slice means "assume escaping" — the conservative default.
	Escapes []bool

	// ResultTaint is the per-parameter result-carry flags of a Gom
	// function: ResultTaint[i] is true when the function's result
	// may carry the products of the i-th flattened parameter (a
	// saturated call's result is produced by its callee — `f 3`
	// carries `f`'s product; `const_f` discarding `f` carries
	// none). check's result-taint inference fills it for locally
	// declared functions; the .gomi loader fills it from
	// `//gom:result-notaint` directives (parameters not listed
	// there stay tainted). An absent or short slice means "assume
	// tainted" — the conservative default.
	ResultTaint []bool

	// Surface, when non-empty, is the user-facing spelling of the
	// symbol — `Go.Map.make` — for diagnostics where Name (the rt
	// backing symbol, `MapMake`) would mislead. Compiler-owned
	// member tables set it; every other symbol leaves it empty and
	// diagnostics fall back to Name.
	Surface string

	// EmitClash, when non-empty, is the package-scope Go name this
	// member's emitted reference collides with: `p.foo` compiles to
	// `p.Foo`, which silently names the package's *other* member
	// `Foo` (E0607). The .gomi loader sets it on members whose
	// generated name another member claims — private decls are
	// invisible to the interface, so only member-vs-member clashes
	// are marked; the declaring package diagnoses the rest at its
	// own conformance check.
	EmitClash string
}

// DefSpan returns the span of the symbol's definition site, or the
// zero span for symbols without a source definition (Go symbols,
// builtins).
func (s *Symbol) DefSpan() diag.Span {
	if s.Def == nil {
		return diag.Span{}
	}
	return s.Def.Span()
}
