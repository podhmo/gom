# Verification: post-argument selector binding (`x.M(1).N(2)`)

Verification for
[decisions/2026-11-05-post-arg-selector-binding](../decisions/2026-11-05-post-arg-selector-binding.md).

## What changed

`internal/syntax/parse_expr.go`:

- `parseApply` loops `args → "."-selectors → args …`: after each
  argument list, consecutive `.member` selectors wrap the Apply in
  Select nodes left-associatively, then the loop resumes collecting
  arguments.
- `parseAppArg` parses argument values through the new
  `parseAppArgValue`: a `(`-led argument is just the paren-expr
  (the postfix `.` loop does not run inside it), so the `.` after
  `)` reaches `parseApply`'s chain loop. All other arguments still
  go through `parsePostfix` unchanged.
- `docs/grammar.ebnf` gains `arg-expr = paren-expr | simple-expr`
  and the `apply-expr` chain production; spec 8.1.3 item 3 records
  the left-associative chain reading.

## Evidence

`gom dump -phase=syntax` on `t.AddDate(1, 0, 0).AddDate(0, 1, 0)`:

- before: `apply (sel t.AddDate) (sel (tuple 1 0 0).AddDate) (tuple 0 1 0)`
  — the second `.AddDate` selected on the argument tuple (E0401
  "tuple type has no AddDate" + arity errors, reproduced in
  [the problem note](../problems/2026-11-05-method-chain-select-binds-args.md)).
- after: `apply (sel (apply (sel t.AddDate) (tuple 1 0 0)).AddDate) (tuple 0 1 0)`
  — `(t.AddDate(1,0,0)).AddDate(0,1,0)`; `gom check` is clean.

Shape table (dump-verified):

| source | parse |
| --- | --- |
| `x.M(1)` | `(apply (x.M) 1)` — identical to before |
| `x.M(1).N` | `((apply (x.M) 1).N)` |
| `x.M(1).N(2)` | `(apply ((apply (x.M) 1).N) 2)` |
| `(x.M(1)).N(2)` | same — explicit parens now agree with the unparenthesized form |
| `f x.y` | `(apply f (x.y))` — unchanged |
| `f (x.y)` | `(apply f (x.y))` — select-on-argument spelling survives |
| `f (x).y` | `((apply f x).y)` — changed (was `f ((x).y)`) |
| `g (h z).m` | `((apply g (apply h z)).m)` — uniform: `.` after a `(` arg binds to the apply |
| `send ch (f x).y` | `(f x).y` — send operands keep postfix binding (no enclosing apply) |

## Regression surface

- `grep -rE '\)\.[A-Za-z_]' --include='*.gom' --include='*.gomi' .`
  finds no source relying on the old `f (e).m` reading (one comment
  mention only), so no golden or spec fixture changed shape.
- `go test ./internal/syntax` — includes new `TestParseExpr` rows
  pinning both the chain and the preserved single-call/argument
  shapes.
- `testdata/spec/ok_method_chain.gom` + generated expected.json
  pin the accepted chain end to end through check.
- `internal/build` compare case `go_method_chain` runs
  `t0.AddDate(1, 0, 0).AddDate(0, 1, 0)` through eval (stub-bound
  `time.Time.AddDate`) and the compiled binary — both print
  `1971-02-01`.
- `go test ./...` clean.
