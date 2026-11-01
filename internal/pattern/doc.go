// Package pattern checks exhaustiveness and redundancy of patterns
// (phase 5 of the pipeline) using Maranget's usefulness algorithm
// ("Warnings for pattern matching", JFP 2007) and generates up to five
// counterexample patterns on failure (docs/design.md section 4.2).
//
// The check runs on the typed AST, since the set of constructors is
// determined by the type. It is shared by match expressions, let bindings,
// and function parameters, and emits the E0301 diagnostic with fixes for
// non-exhaustive matches.
package pattern
