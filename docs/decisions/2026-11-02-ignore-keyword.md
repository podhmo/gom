# `ignore` is a keyword, not a plain function

- 状態: 採用
- 日付: 2026-11-02

## Background

spec 1.1 item 2 makes `ignore e` the explicit discard form, but the
parser never produced `syntax.Ignore`: `ignore` was a plain
identifier, so `ignore e` parsed as an application and failed resolve
with E0201 "undefined name `ignore`" (TODO "[bug] `ignore e` がデッド構文").

grammar.ebnf's keyword note claimed `ignore` belongs with assert /
assert_eq / todo / not / show / compare — "plain functions, not
keywords". That reading does not survive contact with the
implementation:

- `syntax.Ignore` / `check.Ignore` exist and every phase after the
  parser already handles them (resolve, check, pattern, lower, eval,
  treedump) — the node was designed for this syntax.
- No function named `ignore` exists in the builtin scope, the builtin
  signatures, or the (not yet written) standard library. The other
  names on that list (`assert`, `todo`, `not`, ...) are all present as
  `KindBuiltin` symbols; `ignore` alone was missing.

## Decision

`ignore` is a reserved keyword and a unary prefix form, exactly like
`try`: `ignore e` parses as `Ignore(X)` where the operand is an
application-level expression (`ignore f x` is `ignore (f x)`,
`ignore a + b` is `(ignore a) + b`). After `.` it remains a legal
member name, like every keyword.

This keeps spec 1.1's wording true — `ignore e` is the discard form —
with the smallest change: only the parser needed to start producing
the node.

## Consequences

- `ignore` can no longer be a variable, function, or type name.
- The grammar note listing `ignore` among the plain functions is
  removed; the keyword list and `unary-expr` rule now include it.
- `ignore` cannot be passed as a value (`List.map ignore xs`). If a
  first-class discard function is ever wanted it needs a different
  name — same restriction `try` already lives with.

## Verification

- `internal/syntax` parses `ignore e` to `Ignore`; the existing
  `internal/syntax/testdata/spec/ok_control_flow.gom` case (`ignore x`
  in a `for` body) now exercises the real production path.
- `testdata/golden/ignore.gom` pins the generated shape: the operand
  evaluates for effect and its value is dropped (`compute(40)` as a
  bare statement, `_ = rt.ResultFromGo(...)` for a Go call).
