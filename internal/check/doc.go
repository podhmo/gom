// Package check performs type checking (phase 4 of the pipeline) and
// produces the typed AST.
//
// Application nodes in the typed AST carry structured call information —
// the callee symbol, type arguments, the number of applied and remaining
// arguments, the calling convention (Gom or Go), and whether the call can
// be emitted directly — so later phases never re-derive it from names or
// syntax. Pattern checking and error reporting operate on the typed AST.
// The mutable-sharing lint of spec section 10 (W0701/W0702) also lives
// here as a post-inference sweep (lintMutableSharing): it needs solved
// types, the tyCons declaration tables, and shadowedBuiltin.
package check
