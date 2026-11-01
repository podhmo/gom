# `|?>` step name for nameless steps uses byte offsets, not line numbers

- 状態: 未解決
- 日付: 2026-11-02

## Situation

spec.md section 9.2 item 4: when the right-hand side of `|?>` has no
name (a lambda, a partially applied expression, etc.), the step name
used to wrap the GoError is the source position written as
`config.gom:14` — file and *line*.

The evaluator (`internal/eval.stepName`) can only produce
`file:byteOffset`: diagnostics work in UTF-8 byte offsets, and the
evaluator holds no source text, so it cannot map an offset to a line
number at error-construction time.

## Why it is a problem

Nameless-step `|?>` errors will read `foo.gom:231` instead of
`foo.gom:14`, so interpreted and compiled programs produce different
error text — exactly the kind of divergence the comparison harness is
meant to catch, except here it is the evaluator that is wrong.

## Possible responses

- Give the Evaluator an optional `*diag.LineIndex` (built from the
  source by the caller) so `stepName` can compute `file:line`; a nil
  index keeps the byte-offset fallback.
- Or accept that the offset form is fine for diagnostics and instead
  make codegen emit `file:line` the same way — compare only the
  wrapped-error text loosely in the harness.
