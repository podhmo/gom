# `expr : Type` ascription is only defined for `Go.conv`

- 状態: 未解決
- 日付: 2026-11-01

## Situation

spec §8.4 shows `Go.conv user_id : Int` — a `:` applied after a call,
at expression level. The spec never says whether `e : T` is a general
type-ascription operator or part of the `Go.conv` built-in's syntax.

Differences that matter:

- if `e : T` is a general ascription, `let x : Int = 1`-looking forms,
  `{ a : Int }`, `f (x : Int)` and `match e {..} : T` all become
  expressible and their meaning must be defined — but `Go.conv` converts
  while an ascription only asserts, so the two are not interchangeable
- if `:` is part of `Go.conv`, then `Go.conv` is not a normal function
  and the grammar should mark the whole form specially

## Grammar choice

`docs/grammar.ebnf` defines a general ascription
`ascribed-expr = pipe-expr , [ ":" , type-expr ]` at the loosest
precedence, and defers to a semantic check that only `Go.conv`
applications may be ascribed [SEMANTIC — PROVISIONAL type-ascription].
This keeps the grammar permissive while the spec decides.

Note `:` can not lead a continuation line under spec 1.5, so
`Go.conv e\n: T` does not parse.

## Related

- spec.md §8.4 — grammar.ebnf `ascribed-expr`

## Possible responses

- Declare `e : T` a general ascription and make `Go.conv` special only
  in that it performs a conversion (ascription on other expressions is
  then a type assertion — still needs a spec sentence)
- Or restrict `:` to `Go.conv` and reject general ascriptions in check
  (current implementation stance)
