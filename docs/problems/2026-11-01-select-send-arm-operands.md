# Extent of `send ch v` operands in select arms is ambiguous

- 状態: 未解決
- 日付: 2026-11-01

## Situation

spec §10.4 defines the send arm as `send ch v -> e` but does not say how
far `ch` and `v` extend. Two juxtaposed expressions cannot both be full
expressions: `send f x v` could mean `send (f x) v` or `send f (x v)`,
and `send ch f x` could mean `send ch (f x)` or be an error.

The grammar currently takes both operands as `simple-expr`
(atoms + member access), so `send ch v` works but `send (f x) v` and
`send ch (g v)` need parens. This fails loudly rather than silently
misparsing.

Alternative readings:

- channel = simple-expr, value = full expr up to `->`: `send ch f x`
  silently becomes `send ch (f x)` — matches the receive arm's rule
  (`x <- expr -> e` reads a full expr), but can hide mistakes
- Go-style `ch <- v` syntax would remove the problem entirely

## Why it is a problem

Silent misreading of the send value is a real footgun in the only place
Gom has two bare juxtaposed operands.

## Related

- spec.md §10.4 — grammar.ebnf `send-arm`, `recv-arm`
- decisions/select-receive-arms (receive side already restricted to
  `x | _ <- expr`)

## Possible responses

- Keep both operands as simple-expr (current grammar choice)
- Or make the value a full expr (receive-arm-like) and accept the
  ambiguity for `send f x v`
- Or change the syntax to `ch <- v` in a later spec revision
