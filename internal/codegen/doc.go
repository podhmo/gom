// Package codegen translates Core IR into go/ast (phase 7 of the
// pipeline) and prints it with go/printer (phase 8).
//
// Expressions are compiled to statements by destination passing (Return,
// Assign, Discard); sum types become sealed interfaces with variant
// structs whose invalid values panic via rt.InvalidVariant; tests are
// emitted as _test.go files; and //line directives map generated code back
// to .gom sources. It makes no semantic decisions: evaluation order,
// arity, interop conversions, escape boundaries, and monomorphization are
// already fixed in Core IR (docs/design.md section 2.1).
package codegen
