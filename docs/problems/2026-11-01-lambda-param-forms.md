# Lambda parameter forms beyond bare identifiers are unspecified

- 状態: 未解決
- 日付: 2026-11-01

## Situation

spec shows lambdas as `fn x -> x + 1`, `fn a b -> a + b`, `fn o -> o.total`,
`fn c -> Ok {...}`, `fn () -> {...}` and `fn _ -> ...`. So the grammar
admits parameters of the form `ident | "_" | "()"`.

Unspecified:

- typed parameters `fn (x: Int) -> ...`
- pattern parameters `fn (a, b) -> ...` (spec 4 mentions function
  argument patterns — does that cover lambda parameters too?)
- labeled parameters `fn (~x: Int) -> ...`

The grammar rejects these in lambda position (they remain valid only in
`fn` declarations). Note this is what keeps `fn` decl vs lambda
disambiguation simple: `fn` followed by `ident` then `(` must be a
declaration, `fn` followed by bare param names then `->` is a lambda.

## Related

- spec.md §1.2, §4 — grammar.ebnf `lambda`, `lambda-param`

## Possible responses

- Confirm the minimal set, or extend lambda-param to `( ident : T )` /
  patterns — noting that `fn` decls already allow `(~x: T)` and
  `(p: T)` patterns, so parity would suggest allowing them in lambdas
