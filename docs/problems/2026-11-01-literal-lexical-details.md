# Lexical details of literals and identifiers are unspecified

- 状態: 未解決
- 日付: 2026-11-01

## Situation

spec.md shows examples of literals but never defines their lexical
forms. The grammar's choices, all marked [PROVISIONAL
literal-lexical-details]:

- integers: decimal assumed; hex `0x` / octal `0o` / binary `0b` and
  `_` separators added Go-style. Leading zeros stay decimal —
  see docs/problems/2026-11-02-leading-zero-decimal-ints.md
- floats: `1.0`, `1e3`, `1.5e-3` allowed; `.5` and `5.` rejected so a
  leading/trailing `.` can never clash with member access or `..`
- characters: `'a'` with Go-style escapes; whether `\u{...}` counts as
  one Char (spec says Char is one code point)
- strings: escapes `"` `\` `n` `t` `r` `0` `u` `\u{...}`; a literal
  `${` inside an interpolated string is written `\${` (unspecified);
  no raw/backquote strings
- comments: `/* */` assumed non-nesting (Go-style)
- identifiers: ASCII only; unicode letters and OCaml-style trailing `'`
  are unspecified
- keywords usable as member names after `.` (needed for `Chan.send`)
  — current behavior is split: `Chan.send ch v` and `(Chan.send)`
  work, but a bare `Chan.send` in value position (e.g.
  `let f = Chan.send`) does not parse — `send` lexes as the select
  keyword and the mis-parse surfaces as E0101 at the following
  block item (observed while wiring P5-1's `Chan` module, 2026-11-04)

## Why it is a problem

The lexer is half of the grammar; without these decisions P1-3 must
either guess (current) or leave them configurable later.

## Related

- spec.md §1.2 (comments, interpolation), §3.1 (Char), §4.1 (literals)
- grammar.ebnf §2 (lexical structure), §9 (character classes)

## Possible responses

- Add a short "lexical structure" section to spec.md fixing these
  points (probably "same as Go" for literals/comments, ASCII for idents)
- Or accept the provisional forms and revisit when the lexer lands
