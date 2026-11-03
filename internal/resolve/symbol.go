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
