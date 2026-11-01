package types

import (
	"fmt"
	"strings"
)

// Type is the internal representation of a Gom type
// (docs/spec.md section 3). The interface is sealed: every variant is
// declared in this package and type switches over Type must end with
// `default: panic("unreachable")`.
//
// Types are nominal — there is no structural subtyping — and are
// compared with Equal rather than ==. Substitution over type
// variables is Subst.
//
// Type values are immutable after construction: the exported slice
// fields (Con.Args, Func.Params, Tuple.Elems, Go.Args) are shared
// between nodes and must not be mutated — build a new node instead.
// The single mutation point is Meta.Bind, which is inference's.
type Type interface {
	isType()
	String() string // renders in Gom surface syntax
}

// Con is a nominal type constructor applied to zero or more type
// arguments: `Int`, `List Int`, `Result Unit TxError`,
// `util.Option Int`. Nullary constructors carry no Args.
//
// Qual is the import path of the package that declares the
// constructor — the canonical package identity, not the local binding
// name, so `import "x/util" as a` and `import "x/util" as b` produce
// identical Cons. Name resolution fills it from Symbol.Package. Two
// reserved qualifiers are not import paths:
//
//   - "" is the builtin scope — the types of spec section 3.1,
//     including Option, Result and List, which the standard library
//     implements but which are in scope with no import — as well as
//     types declared in the package being compiled. The two cannot
//     collide: a local declaration shadows a builtin name during
//     resolution, and Cons never compare across compilations.
//   - "Go" is the compiler-owned Go-interop namespace (Go.Slice,
//     Go.Map); no Gom package may be named Go.
//
// Builtin-scope symbols carry Package == "", so the helpers in this
// package (Option, List, Result, ...) Equal whatever the resolver
// produces for the same name.
//
// Con covers both builtin constructors (List, Option, Result, Ref,
// Array, Ptr, Chan, RecvChan, SendChan, Task, Go.Slice, Go.Map,
// GoError, the scalar types of spec section 3.1) and user-declared
// nominal types (ADTs, records, aliases, abstract types). A type
// defined by a Go package is a Go instead — its identity is a
// NamedRef, not a name.
type Con struct {
	Qual string // declaring package's import path, or a reserved qualifier
	Name string
	Args []Type
}

func (*Con) isType() {}

// String renders the constructor in Gom surface syntax:
// `List Int`, `Result Unit TxError`, `io.Option Int`.
func (c *Con) String() string {
	var sb strings.Builder
	writeType(&sb, c, precTop)
	return sb.String()
}

// Var is a rigid type variable — a source-level `a` bound by a type
// declaration or a quantifier. Identity is ID; Name is only for
// display ("" prints as t<ID>).
type Var struct {
	ID   int
	Name string
}

func (*Var) isType() {}

// String renders the variable as its source name (`a`), or `t<ID>`
// when unnamed.
func (v *Var) String() string {
	if v.Name != "" {
		return v.Name
	}
	return fmt.Sprintf("t%d", v.ID)
}

// Meta is a mutable unification variable — a type unknown filled in
// by Hindley–Milner inference (task P2). While unbound it is a
// placeholder; once Bind records a solution, String and Equal chase
// through it. Identity is the pointer.
type Meta struct {
	ID    int
	bound bool
	sol   Type
}

func (*Meta) isType() {}

// Bound reports whether the variable has been solved.
func (m *Meta) Bound() bool { return m.bound }

// Bind records the variable's solution. Binding twice is a caller
// bug and panics.
func (m *Meta) Bind(t Type) {
	if m.bound {
		panic(fmt.Sprintf("types: meta ?%d bound twice", m.ID))
	}
	m.bound = true
	m.sol = t
}

// Solution returns the solved type, or nil while unbound. The result
// is not chased — a solution may itself contain metas.
func (m *Meta) Solution() Type {
	if !m.bound {
		return nil
	}
	return m.sol
}

// String renders `?<id>` while unbound and the solution once bound.
func (m *Meta) String() string {
	if t := m.Solution(); t != nil {
		return t.String()
	}
	return fmt.Sprintf("?%d", m.ID)
}

// FuncParam is one parameter of a function type: positional or
// labeled (`~name: T`, docs/spec.md section 2.1).
type FuncParam struct {
	Label string // "" for positional, else the label without `~`
	Type  Type
}

// Func is a function type `p1 -> p2 -> ... -> r` with the parameter
// list kept flat — `Account -> ~to: Account -> Result Unit TxError`
// is one Func with three parameters, matching the arity and label
// information call sites need.
//
// Canonical form: Ret is never a *Func. `Int -> Int -> Int` is always
// Func{Params: [Int, Int], Ret: Int}, never Func{[Int],
// Func{[Int], Int}} — currying makes the two spellings the same type,
// so only one is stored. Fn is the constructor and flattens;
// constructing a Func literal directly must preserve the invariant.
// A bound Meta in Ret position can still hide a function type (the
// meta node is inference's to keep, so Fn does not chase it), which
// is why Equal flattens after chasing — the two spellings always
// compare equal.
type Func struct {
	Params []FuncParam
	Ret    Type // never a *Func — see the canonical form above
}

func (*Func) isType() {}

// Arity returns the declared parameter count.
func (f *Func) Arity() int { return len(f.Params) }

// String renders `A -> ~to: B -> R`. A function parameter that is
// itself a function type is parenthesized.
func (f *Func) String() string {
	var sb strings.Builder
	writeType(&sb, f, precTop)
	return sb.String()
}

// Tuple is a tuple type `(A, B, ...)` — at least two elements.
type Tuple struct {
	Elems []Type
}

func (*Tuple) isType() {}

// String renders `(A, B)`.
func (t *Tuple) String() string {
	var sb strings.Builder
	writeType(&sb, t, precTop)
	return sb.String()
}

// Go is a Go-interop type handle: a type defined by a Go package, such
// as the named type `time.Duration` or the interface `io.Reader`
// (docs/spec.md section 8.3). Its identity is Ref — a package path and
// name pair that is the whole nominal identity. Everything a path+name
// cannot express (method sets, underlying types, assignability) lives
// in goimport-side tables keyed on Path+Name, not behind an interface
// here.
type Go struct {
	Ref  NamedRef
	Args []Type // instantiated arguments for generic Go named types
}

func (*Go) isType() {}

// String renders the Go type as `path.Name` (`io.Reader`), or
// `path.Name[a, b]` when instantiated.
func (g *Go) String() string {
	var sb strings.Builder
	writeType(&sb, g, precTop)
	return sb.String()
}

// NamedRef is the identity of a Go-defined type inside a Go type
// node: a package-qualified named type by path and name —
// `NamedRef{"io", "Reader"}` renders `io.Reader`. Two Go types are
// identical exactly when their refs compare equal, keeping named
// types distinct as docs/design.md section 2.5 requires.
type NamedRef struct {
	Path string // Go import path of the declaring package
	Name string
}

// String renders `path.Name`.
func (r NamedRef) String() string { return r.Path + "." + r.Name }

// --- printing ---

// prec is the syntactic context a type is printed in; a type that is
// not atomic gets parentheses in an operand position.
type prec int

const (
	precTop   prec = iota // top level / function result
	precArg               // argument of a type application
	precParam             // parameter of a function type
)

// atom reports whether the type prints without parentheses in the
// given context. In an argument position an applied constructor needs
// parentheses (`Option (List Int)`); in a parameter position
// juxtaposition already binds tighter than `->`, so `List Int -> R`
// reads correctly without. Tuples carry their own parentheses and Go
// refs self-delimit, so both count atomic everywhere.
func atom(t Type, p prec) bool {
	switch t := t.(type) {
	case *Var, *Meta, *Tuple, *Go:
		return true
	case *Con:
		return p != precArg || len(t.Args) == 0
	case *Func:
		return false
	default:
		panic("unreachable")
	}
}

func writeType(sb *strings.Builder, t Type, p prec) {
	wrap := p != precTop && !atom(t, p)
	if wrap {
		sb.WriteByte('(')
	}
	switch t := t.(type) {
	case *Var:
		sb.WriteString(t.String())
	case *Meta:
		if t.bound {
			writeType(sb, t.sol, p)
		} else {
			sb.WriteString(t.String())
		}
	case *Con:
		if t.Qual != "" {
			sb.WriteString(t.Qual)
			sb.WriteByte('.')
		}
		sb.WriteString(t.Name)
		for _, a := range t.Args {
			sb.WriteByte(' ')
			writeType(sb, a, precArg)
		}
	case *Func:
		for i, fp := range t.Params {
			if i > 0 {
				sb.WriteString(" -> ")
			}
			if fp.Label != "" {
				sb.WriteByte('~')
				sb.WriteString(fp.Label)
				sb.WriteString(": ")
			}
			writeType(sb, fp.Type, precParam)
		}
		sb.WriteString(" -> ")
		writeType(sb, t.Ret, precTop)
	case *Tuple:
		sb.WriteByte('(')
		for i, e := range t.Elems {
			if i > 0 {
				sb.WriteString(", ")
			}
			writeType(sb, e, precTop)
		}
		sb.WriteByte(')')
	case *Go:
		sb.WriteString(t.Ref.String())
		if len(t.Args) > 0 {
			sb.WriteByte('[')
			for i, a := range t.Args {
				if i > 0 {
					sb.WriteString(", ")
				}
				writeType(sb, a, precTop)
			}
			sb.WriteByte(']')
		}
	default:
		panic("unreachable")
	}
	if wrap {
		sb.WriteByte(')')
	}
}
