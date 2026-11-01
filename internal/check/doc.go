// Package check performs type checking (phase 4 of the pipeline) and
// produces the typed AST.
//
// Application nodes in the typed AST carry structured call information —
// the callee symbol, type arguments, the number of applied and remaining
// arguments, the calling convention (Gom or Go), and whether the call can
// be emitted directly — so later phases never re-derive it from names or
// syntax. Pattern checking and error reporting operate on the typed AST.
package check
