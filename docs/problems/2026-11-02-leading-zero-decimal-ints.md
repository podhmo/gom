# Leading-zero decimal ints: Gom semantics vs Go spelling

- 状態: 未解決
- 日付: 2026-11-02

## Situation

The grammar's `dec-int` is `digit , { digit | "_" }` — leading
zeros are accepted, and nothing in spec.md gives them octal
semantics. Go, however, reads a leading zero as octal: `080` is a
compile error and `077` is silently 63, not 77. A fuzz seed
(`FuzzCompile/f7bdbe872126e546`) emitted `080` verbatim and gofmt
rejected the generated Go.

The parser now normalizes the `Lit.Text` of a decimal literal to
the spelling of the value denoted: `080` → `80`, `0_17` → `17`,
`00` → `0` (see `intLitText` in internal/syntax/parse_expr.go).
Base-prefixed literals (`0x`/`0o`/`0b`) pass through unchanged
because they denote the same value in Go.

## Why it is a problem

The choice "leading zero is decimal, normalized away" is a guess
made under [PROVISIONAL literal-lexical-details]. Alternatives:

- reject leading zeros with a diagnostic (Go rejects `080` too —
  but Gom's own grammar accepts them, and a spec change would be
  needed)
- give them octal semantics (matches Go source syntax, but
  surprises anyone who writes fixed-width numbers)

Either way the behavior is currently implemented, not specified.

## Related

- docs/problems/2026-11-01-literal-lexical-details.md (the
  umbrella issue for unspecified literal forms)
- grammar.ebnf dec-int; internal/syntax/parse_expr.go intLitText
