// Package gomgen generates random .gom source programs by walking the
// formal grammar in docs/grammar.ebnf (docs/decisions/
// 2026-11-02-grammar-based-generator.md). The grammar file is the
// single source of truth: ParseGrammar reads it into a rule table and
// the generator expands productions with weighted choices, so a
// grammar edit changes what Generate emits without touching this
// package.
//
// Two modes share the walker:
//
//   - mechanical (Options.Typed == false): pure grammar-driven output.
//     Programs mostly parse; names, arities and types are unconstrained
//     so semantic errors are common — good parser-fuzz seed material.
//   - typed-ish (Options.Typed == true): the same productions, but
//     declarations, expressions and patterns are emitted by a
//     type-directed layer that respects simple scope and arity
//     conventions, so a useful fraction of programs typecheck and
//     reach codegen — canary and (future) eval-vs-compiled input.
//
// Lexical rules cannot be walked mechanically (they produce tokens,
// not grammar structure), so the lexer-level nonterminals — ident,
// UIdent, INT, FLOAT, CHAR, STRING, NL — have builtin producers.
// head-expr is a prose restriction over expr ("no leading {"), which
// EBNF cannot encode; the generator implements it as a one-flag
// suppression of brace atoms at head level. Anything reachable from
// source-file that is neither a proper rule nor a builtin fails at
// ParseGrammar time, so coverage gaps are loud rather than silent.
//
// Determinism: every choice comes from the caller's *rand.Rand; the
// same seed always produces byte-identical output, which is what makes
// failures replayable. Generation never reads maps in iteration order
// for output decisions (scope and registry lookups collect into
// slices first).
package gomgen
