// Package build ties the compiler together: it owns the package graph,
// runs the pipeline per package, handles incremental builds and caching
// (cache keys cover source hashes, dependency .gomi hashes, codegen
// version, rt/stdlib ABI, interop rules, and target Go version, per
// docs/design.md section 5), and invokes go build / go test on the
// generated code.
//
// Today only the per-file driver exists: Compile runs the six phases
// (parse → resolve → check → pattern → lower → codegen) over a single
// source file and aggregates diagnostics. There is no package graph —
// every file compiles independently, so sibling .gom files cannot
// reference each other's top-level names — and no caching: everything
// is recompiled on every call. cmd/gom stays a thin layer over build
// and internal/format.
package build
