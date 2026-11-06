// Package build ties the compiler together: it owns the package graph,
// runs the pipeline per package, handles incremental builds and caching
// (cache keys cover source hashes, dependency .gomi hashes, codegen
// version, rt/stdlib ABI, interop rules, and target Go version, per
// docs/design.md section 5), and invokes go build / go test on the
// generated code.
//
// Today the per-package driver exists: CompileFiles runs the six
// phases (parse → resolve → check → pattern → lower → codegen) over
// the .gom files of one package as a unit — a top-level name one
// file declares is visible in the others — and Compile is the
// single-file degenerate case. There is no package graph beyond the
// unit and no caching: everything is recompiled on every call.
// cmd/gom stays a thin layer over build and internal/format.
package build
