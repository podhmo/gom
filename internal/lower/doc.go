// Package lower translates the typed AST into Core IR (phase 6 of the
// pipeline). It desugars pipes, function composition, try and |?>,
// compiles match expressions into decision trees, orders evaluation into
// explicit bindings (ANF), detects self-tail recursion, classifies call
// arity, and separates test definitions from build output.
//
// Its output always satisfies the Core IR invariants validated by
// internal/core.
package lower
