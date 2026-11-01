// Package pattern checks exhaustiveness and redundancy of patterns
// (phase 5 of the pipeline) using Maranget's usefulness algorithm
// ("Warnings for pattern matching", JFP 2007) and generates up to five
// counterexample patterns on failure (docs/design.md section 4.2).
//
// The check runs on the typed AST, since the set of constructors is
// determined by the type (Env). It is shared by match expressions, let
// bindings, for bindings, and function parameters (CheckMatch,
// CheckPattern, CheckFile) and emits the pattern-matching
// diagnostics of docs/error-codes.md: E0301 non-exhaustive match
// (with the "add the missing arms" placeholder fix), E0302
// unreachable match arm, E0303 refutable pattern in an irrefutable
// position, and E0304 or-pattern alternatives binding different
// variables.
//
// Decision-tree compilation for code generation is Maranget 2008 and
// belongs to internal/lower (P1-7), not to this package.
package pattern
