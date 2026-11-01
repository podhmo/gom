// Package resultrepr documents the P0-4 experiment: it compares two Go
// representations of Gom's Result[T, E] and the (T, error) <-> Result
// boundary conversion, per TODO.md P0-4.
//
// The packages are:
//
//	rt      stand-in for github.com/podhmo/gom/rt (InvalidVariant)
//	goside  Go-side functions a Gom program would call ((T, error) shaped)
//	payload a large value type used to measure copying
//	sealed  candidate A: sealed interface + Result_Ok / Result_Err structs
//	flat    candidate B: struct{ Value T; Error E; Ok bool }
//	example a main package using both reps the way a Go consumer would
//
// There is no code here that a compiler emits automatically; every file
// in sealed/ and flat/ is hand-written to look like what codegen would
// produce, including the |?> / try expansions of design.md 4.6.
package resultrepr
