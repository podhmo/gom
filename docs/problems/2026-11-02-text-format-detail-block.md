# The spec's text-format example shows a block the model cannot emit

- 状態: 未解決
- 日付: 2026-11-02

## Situation

spec §4 shows this `--format text` output for E0301:

```
error[E0301]: non-exhaustive match on `Expr`
  --> eval.gom:2:3
   |  patterns not covered:
   |    Neg (Num _)
   |    Neg (Add _ _)
   |    Neg (Let _ _ _)
   |    Let _ _ _
   = note: arms with guards do not count toward exhaustiveness
```

The `   |  patterns not covered:` block is *not* a note — notes render
with the `   = note:` prefix in the same example. In the internal
model there is nothing it could come from: `data.missing_patterns` is
machine-facing JSON detail, and the text emitter deliberately does not
interpret `data` keys (that would hard-code per-code knowledge into
the format layer). P1-10's text emitter renders header, source
snippet with `^` underlines, notes and fix titles — the closest
faithful rendering of the current model
([decision](../decisions/2026-11-02-diag-output-formats.md)).

## Why it is a problem

The spec's only concrete text example cannot be reproduced by any
mechanical conversion of `diag.Diagnostic`. Either the example is
aspirational, or the model is missing a concept (rustc-style labeled
spans / per-diagnostic detail lines) that the JSON wire shape does not
have either — the wire object has no field for it.

## Possible responses

- Convention: phases render such detail as notes (e.g.
  `"patterns not covered: Neg (Num _), …"`), which display as
  `   = note:` lines. No model or spec change; the example's exact
  `|` block stays unrepresentable.
- Extend the model with labeled/secondary spans (`labels: [{span,
  message}]` rustc-style). That changes the JSON wire shape too, so it
  needs a spec section 12 amendment first.
- Per-code text templates (fluent-style): the emitter maps
  code+data→sentences. Heavier machinery; probably overkill for v1.
- Or amend the spec example to match the implemented format (snippet
  + underlines + notes).

## Related

- spec.md §4 (the example), §12 (message format)
- [decision: diag-output-formats](../decisions/2026-11-02-diag-output-formats.md)
- [problem: none yet — E0301 is the only code whose spec example shows
  a detail block]
