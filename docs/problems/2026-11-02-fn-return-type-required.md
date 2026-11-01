# `-> T` is mandatory on every `fn` — no elision anywhere

- 状態: 未解決
- 日付: 2026-11-02

## Situation

grammar.ebnf requires `-> type-expr` in all three function
positions: `.gom` fn-decl, `.gomi` fn-sig, and `extern` items.
The parser enforces it (`fn f () = ()` is E0101 even though the
body `()` shows the return type).

## Why it is a problem

- Unit-returning functions must always write `-> Unit`; effectful
  helper functions are the common case where the annotation feels
  like noise.
- The body makes the type inferable in `.gom`, so the annotation
  is a signature discipline, not a parse necessity — a defensible
  rule, but one the spec never states explicitly.
- `.gomi` genuinely needs it (there is no body); the question is
  only whether `.gom` fn-decl may elide it.

## Related

- grammar.ebnf `fn-decl`, `fn-sig`, `extern-item`
- spec.md fn declarations
- docs/decisions/2026-11-02-p1-3-parser-interpretations.md

## Possible responses

- Keep the rule and state it in spec.md (signatures are always
  complete — good for .gomi generation)
- Or allow elision only when it would be `-> Unit`
