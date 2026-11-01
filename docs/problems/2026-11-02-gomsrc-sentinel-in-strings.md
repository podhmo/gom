# `//gomsrc:` sentinel could collide with generated string content

- 状態: 未解決
- 発見: 2026-11-02, P1-8 review follow-up

## Situation

`render` splices `//line` directives into the gofmt'd output by
rewriting lines whose first non-whitespace text is the sentinel
`//gomsrc:` (`internal/codegen/codegen.go` `spliceLines`). The
sentinel is only ever produced by `g.line`, but the rewrite is
textual: any *other* line in the formatted output starting with
`//gomsrc:` would also be rewritten into a `//line` directive.

## Why it is (not currently) a problem

- Gom string literals cannot produce such a line: the grammar has
  only `"` strings (no backquote/raw strings — grammar.ebnf
  `STRING`), the lexer rejects a raw newline inside one (`E0102
  unterminated string literal`), and `Lit` atoms keep the source
  lexeme, so emitted Go strings are single-line `"..."` literals.
  A `//gomsrc:` inside one stays mid-line.
- Gom comments are never emitted into generated code.
- So today the sentinel can only appear where `g.line` put it.

## 関連

- `internal/codegen/codegen.go` (`spliceLines`, `g.line`)
- `internal/syntax/lexer.go` (string scanning, `E0102`)
- [decisions/generated-go-conventions](../decisions/2026-11-02-generated-go-conventions.md)
  — the sentinel-splice design

## 考えられる対応

- Revisit if raw/multiline string literals are ever added to Gom
  (see problems/literal-lexical-details): a `"..."`-with-newline or
  backquote literal could carry a line starting with `//gomsrc:`
  into the output. Options then: a collision-proof sentinel
  (e.g. with a random token), escaping the marker inside literals,
  or emitting `//line` positions through a side table.
