# A negative literal pattern on a non-first match arm reads as binary minus

- 状態: 未解決
- 日付: 2026-11-02

## Situation

NL-2 (grammar.ebnf section 3) suppresses a virtual newline before
every token in `lineStart` — the binary operators, `else`, and `|`
— so that an infix expression may wrap to the next line. `-` is in
that set, which collides with the pattern grammar's `- int|float`
literal: in

```gom
match n {
  x -> 1
  -80 -> 2
}
```

no NL is inserted between `1` and `-`, so the first arm's body
parses as the binary expression `1 - 80` and the following `->`
fails with `expected a newline, ";" or "}" between match arms`.
A negative literal pattern on the FIRST arm is unaffected (the `-`
directly follows `{`); on any later arm it must be written
`(-80) -> 2` or separated explicitly (`x -> 1 ; -80 -> 2`).

## Why it is a problem

- The surface syntax suggests negative literal patterns are
  ordinary patterns usable in any arm position; the silent
  reinterpretation as a continuation of the previous arm's body is
  a grammar trap, and the diagnostic does not hint at it.
- Match arms are the only spot the trap fires today — the other
  pattern positions (`let` heads, `for` heads, ctor arguments)
  take their pattern directly after a keyword or delimiter rather
  than at a line start. But the same ambiguity is latent in the
  expression grammar: `f a\n-b` parses as `f a - b`, never as
  `f a (-b)` — a wrapped negative operand needs parens too.

## Plausible directions

- Spec decision: declare `-` at arm-start ambiguous and require
  parens (status quo, then document it in spec.md), or
- give match/select arm parsing a targeted resync: when an arm
  body ends in `e - int|float ->`, reinterpret the tail as a new
  arm (fragile — `x -> 1\n-80 -> 2` genuinely means `1 - 80 -> 2`
  is unwritable today anyway), or
- remove `-` from `lineStart` and require explicit continuations
  for wrapped subtraction (a spec-visible grammar change — needs a
  spec.md amendment and would break `a\n- b` style code), or
- make the diagnostic suggest `(-N)` when `->` follows a
  `- literal` continuation.

## Related

- grammar.ebnf section 3 (NL-2, line-start set); token.go `lineStart`
- parse_expr.go parsePatAtom `tokMinus` (negative literal patterns)
