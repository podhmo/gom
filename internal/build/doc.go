// Package build ties the compiler together: it owns the package graph,
// runs the pipeline per package, handles incremental builds and caching
// (cache keys cover source hashes, dependency .gomi hashes, codegen
// version, rt/stdlib ABI, interop rules, and target Go version, per
// docs/design.md section 5), and invokes go build / go test on the
// generated code.
//
// cmd/gom stays a thin layer over build and internal/format.
package build
