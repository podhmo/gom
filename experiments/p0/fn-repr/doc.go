// Package fnrepr is the root of the P0-2 experiment comparing Go
// representations for Gom function values.
//
// Both subpackages implement the same hypothetical Gom module (see the
// package docs) so that the generated Go surface and benchmark numbers are
// directly comparable:
//
//   - curried/ — style A: a Gom function value of type a -> b -> c is
//     func(a) func(b) c (nested single-argument functions).
//   - arity/ — style B: a Gom function value of type a -> b -> c is
//     func(a, b) c (the remaining arity lives in the Go function type; a
//     partial application is a closure carrying the applied arguments).
//
// Benchmarks and Go-side usage examples live in this package's external
// test package (bench_test.go, example_test.go), which exercises both
// subpackages as a cross-package consumer.
//
// Run the experiment from this directory:
//
//	go test ./...
//	go test -bench=. -benchmem
package fnrepr
