// Package helper is the plain-Go half of the gocall fixture: the
// generated geom package calls these functions with values boxed
// into `any` parameters, and Go-side tests read the dynamic type
// back. TypeName is the %T probe; Kind is the same observation via
// a real type switch.
package helper

import "fmt"

// TypeName reports the dynamic type of x, fmt %T-style.
func TypeName(x any) string { return fmt.Sprintf("%T", x) }

// Kind classifies x with a Go type switch — the switch form the
// numeric-boxing regression calls for.
func Kind(x any) string {
	switch x.(type) {
	case int64:
		return "int64"
	case int:
		return "int"
	case float64:
		return "float64"
	case string:
		return "string"
	default:
		return "other"
	}
}
