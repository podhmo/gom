// Package arity is style B of the P0-2 function-representation
// experiment: a Gom function value of type
//
//	T1 -> T2 -> ... -> Tn -> R
//
// is represented in Go as a function of its remaining arity:
//
//	func(T1, T2, ..., Tn) R
//
// A partial application is an ordinary Go closure that carries the
// already-applied arguments — that closure is the "arity-aware closure":
// a func value plus bound arguments, with the remaining arity encoded in
// its static type.
//
// The code below mirrors what the Gom compiler would emit for the same
// module as the curried package, under these rules:
//
//   - a saturated call compiles to a direct call — for known functions
//     and for unknown function values alike, because the static type
//     always records the remaining arity: f(x, y, z)
//   - a function mentioned as a value needs no wrapper: `add3` is just
//     Add3 (its Go type is already the value type)
//   - a partial application `add3 x` emits a closure of the remaining
//     arity: func(b, c) { return Add3(x, b, c) }
//   - a declared-arity-1 function that returns a function keeps nesting:
//     Gom `a -> (b -> c -> r)` is func(A) func(B, C) R — the flat type is
//     driven by the declared arity, not by flattening the arrow chain
//
// Helpers such as Add3_1 and Transfer_Amount stand for anonymous closures
// the compiler would emit inline at each application site.
package arity
