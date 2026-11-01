// Package treedump renders the compiler's intermediate
// representations — the surface AST, the resolved AST with its symbol
// tables, the typed AST, and types — as stable S-expression-style
// text for golden tests and debugging.
//
// One node of the printed tree is `(head fields...)` with children
// indented two spaces, e.g.
//
//	(file@0:40 main@8:12
//	  (fn@14:40 add%1
//	    (params
//	      (param@20:25 (varpat@20:21 x%2))
//	    (-> Int@30:33)
//	    (binary@35:40 :Int + (var@35:36 :Int x%2) (var@39:40 :Int y%3))))
//
// Conventions (docs/decisions/2026-11-02-treedump-format.md):
//
//   - `@<start>:<end>` is a source span in UTF-8 byte offsets, glued
//     to the head token (or to an atom for leaf names). The zero span
//     is omitted.
//   - `name%<n>` is a *resolve.Symbol: n is a 1-based index assigned
//     in first-encounter order, so diffs survive renames. A `(syms)`
//     section lists every interned symbol with kind and origin.
//   - `:T` annotates a typed node's types.Type, wrapped in
//     parentheses when the rendering contains a space. `:?` means a
//     missing (nil) type.
//   - `->` separates a written name from the symbol it resolved to.
//   - Bare strings containing whitespace, quotes or parens are
//     Go-quoted (strconv.Quote); names that need no escaping stay raw.
//
// Output is deterministic: map side tables are sorted by position and
// nothing depends on map iteration order, so equal trees dump equal
// text — the property golden tests rely on.
package treedump
