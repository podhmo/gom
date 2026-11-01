# Operator coverage gaps: `!=`, `%`, `||`, `!`, bit ops, list spread

- 状態: 未解決
- 日付: 2026-11-01

## Situation

spec.md shows or implies these operators: `|>`, `<|`, `|?>`,
`>>`, `<<`, `+`, `-`, `*`, `/`, `==`, `<`, `>`, `&&`, unary `-`
(via `f (-1)`), member `.`. It never shows:

- `!=` — `==` is defined (structural equality, §3.3) but `!=` is never
  written; assumed to exist, grammar includes it
- `||` — `&&` is listed as a binary operator in §1.5; `||` assumed
- `%` — assumed alongside `*` `/`
- unary `!` (boolean not) — completely absent; grammar has no `!`
- bit operators `&` `|` `^` — absent, and `<<` `>>` are taken by
  function composition, so Gom has no infix shift at all; shifts would
  need Go functions or new syntax
- `..` spread inside a list expression `[x, ..rest]` — spec shows `..`
  only in list *patterns* and Go-call arguments `f(..xs)`

## Why it is a problem

Each missing/extra operator changes the token set and precedence table
that the parser implements. The grammar guesses the plausible set
(marked [PROVISIONAL operator-coverage]).

## Related

- spec.md §1.2, §1.4, §1.5, §3.3 — grammar.ebnf §2.4, §5 precedence
  table, `list-expr`

## Possible responses

- Confirm `!=`, `%`, `||` in spec (one line in §1.2 or a full operator
  table) — this is the biggest gap: spec has NO operator table at all
  except pipes
- Decide whether `!`/`not` exists and whether infix bit ops are wanted
  (`&&`/`||` are already logical, so Go-style `&`/`|`/`^`/`<<`/`>>`
  names are unavailable except maybe `&`/`^`)
- Decide on `[a, ..rest]` list-expression spread
