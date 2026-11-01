// Package curried is style A of the P0-2 function-representation
// experiment: a Gom function value of type
//
//	T1 -> T2 -> ... -> Tn -> R
//
// is represented in Go as nested single-argument functions:
//
//	func(T1) func(T2) ... func(Tn) R
//
// The code below mirrors what the Gom compiler would emit for the module
// in curried.go under the rules of docs/design.md 4.4:
//
//   - a saturated call to a known function compiles to a direct
//     multi-argument Go call: `add3 x y z` -> Add3(x, y, z)
//   - a function mentioned as a value goes through a currying wrapper:
//     `add3` -> Add3Val (shared per function and instantiation)
//   - a partial application `add3 x` emits a closure in curried form:
//     func(b) func(c) { ... }
//   - applying an unknown function value to n arguments emits nested
//     single-argument calls: f(x)(y)(z)
//
// Helpers such as Add3_1 and Transfer_Amount stand for anonymous closures
// the compiler would emit inline at each application site; they are named
// here only so tests and benchmarks can call them.
package curried
