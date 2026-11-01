// Package option is the Go-side representation of Gom's builtin
// Option a type — an ordinary sum type on the sealed-interface
// representation (P0-1, docs/decisions/2026-11-01-value-representation.md),
// hand-written here in the shape the compiler emits for user ADTs.
//
// The marker method's signature mentions A on purpose: Go type
// inference cannot bind a type parameter that appears only in the
// instantiated interface's name, so isOption() returns A.
package option

import "fmt"

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

// IsSome reports whether o is Some. An invalid Option panics.
func IsSome[A any](o Option[A]) bool {
	switch o.(type) {
	case Option_Some[A]:
		return true
	case Option_None[A]:
		return false
	default:
		panic(invalidVariant(o))
	}
}

// GetOrElse returns o's payload, or fallback when o is None.
func GetOrElse[A any](o Option[A], fallback A) A {
	switch o := o.(type) {
	case Option_Some[A]:
		return o.V0
	case Option_None[A]:
		return fallback
	default:
		panic(invalidVariant(o))
	}
}

// invalidVariant mirrors rt.InvalidVariant's message. This package
// cannot import rt: rt's boundary helpers depend on this package, so
// stdlib packages stay leaf dependencies.
func invalidVariant(v any) error {
	return fmt.Errorf("gom: invalid %s value %#v: not a declared variant", "option.Option", v)
}
