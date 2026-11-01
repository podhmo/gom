// Package syntax implements the lexer and parser for Gom and defines the
// surface AST produced by phases 1 (lexing) and 2 (parsing) of the compiler
// pipeline (recursive descent with a Pratt parser for expressions).
//
// The surface AST preserves every source position and keeps comments and
// blank lines as trivia so that gom fmt can share the same parser
// (docs/design.md section 3).
package syntax
