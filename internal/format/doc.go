// Package format implements gom fmt, the official formatter. It is a
// Wadler-style pretty-printer over the surface AST that wraps only
// expressions exceeding the line width and uses the trivia (comments and
// blank lines) preserved by the parser (docs/design.md section 6).
//
// Idempotence and parse round-tripping (parse(fmt(x)) == parse(x)) are
// verified by property tests over generated syntax trees.
package format
