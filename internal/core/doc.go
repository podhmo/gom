// Package core defines the Core IR: a desugared, ANF-style intermediate
// representation produced by internal/lower and consumed by
// internal/codegen.
//
// All language semantics — evaluation order, call arity and calling
// conventions, Go interop conversions, match decision trees, escape
// boundaries, and type monomorphization — are fixed at this level so that
// codegen makes no semantic decisions (docs/design.md section 2.1). The
// package also provides a validator for those invariants.
package core
