# Lambda parameter forms beyond bare identifiers are unspecified

- 状態: 一部解決（typed params）/ 未解決（patterns, labeled）
- 日付: 2026-11-01

## Situation

spec shows lambdas as `fn x -> x + 1`, `fn a b -> a + b`, `fn o -> o.total`,
`fn c -> Ok {...}`, `fn () -> {...}` and `fn _ -> ...`. So the grammar
admits parameters of the form `ident | "_" | "()"`.

Unspecified:

- ~~typed parameters `fn (x: Int) -> ...`~~ **解決 (wave27 lane B)**:
  `(name: Type)` は spec 1.2 の正式な形として採用 — spec.md §1.2、
  grammar.ebnf `lambda-param`、tree-sitter `lambda_param`、
  testdata/spec/ok_lambda_ann_param.gom で検証済み
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

- Confirm the minimal set, or extend lambda-param to
  patterns — noting that `fn` decls already allow `(~x: T)` and
  `(p: T)` patterns, so parity would suggest allowing them in lambdas.
  `( ident : T )` was adopted in wave27 (see above); `(a, b)` patterns
  and `(~x: T)` labels remain open
