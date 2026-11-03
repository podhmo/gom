// Package option is the Go-side representation of Gom's builtin
// Option a type — an ordinary sum type on the sealed-interface
// representation (P0-1, docs/decisions/2026-11-01-value-representation.md),
// hand-written here in the shape the compiler emits for user ADTs.
//
// The marker method's signature mentions A on purpose: Go type
// inference cannot bind a type parameter that appears only in the
// instantiated interface's name, so isOption() returns A.
//
// This file is the hand-written representation generated code
// references; the `Option` module's functions (Option.map,
// Option.get_or_else, ...) are implemented in Gom —
// stdlib/option/option.gom, generated to option.go — with the
// public surface declared by option.gomi.
package option

// Option is Gom's `Option a`: `Some a | None`.
//
//sumtype:decl
type Option[A any] interface{ isOption() A }

// Option_Some is the Some variant of [Option].
type Option_Some[A any] struct{ V0 A }

// Option_None is the None variant of [Option].
type Option_None[A any] struct{}

func (o Option_Some[A]) isOption() A { return o.V0 }
func (Option_None[A]) isOption() A   { var zero A; return zero }

// FromGo materializes an Option T from the value and ok flag of a
// comma-ok Go call — or from a channel receive in a select arm.
func FromGo[T any](v T, ok bool) Option[T] {
	if ok {
		return Option_Some[T]{V0: v}
	}
	return Option_None[T]{}
}
