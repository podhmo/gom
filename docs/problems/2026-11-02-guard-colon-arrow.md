# A guard / channel `x : T -> e` absorbs the arm's `->` into the type

- 状態: 未解決
- 日付: 2026-11-02

## Situation

`match-arm = pattern , [ "if" , expr ] , "->" , expr` and
`recv-arm = ( ident | "_" ) , "<-" , expr , "->" , expr` each put a
full `expr` directly before the arm's `->`. An `expr` may end in a
`":" type-expr` ascription (`ascribed-expr`) and a `type-expr` may
be an arrow type, so both of these are ambiguous:

    match s { p if x : T -> e }
    select  { n <- ch : T -> e }

The greedy reading parses the guard / channel as `x : (T -> e)` and
leaves the arm without its `->`; the intended reading is `x : T`
followed by `-> e`. Both readings are valid derivations of the
grammar — this is a genuine ambiguity, not a precedence tweak.

The parser takes the greedy reading, so `p if x : T -> e` reports
`expected "->" between the pattern and the arm body, found "}"`
followed by `expected an expression, found "}"` — the diagnostics
point at whatever follows the absorbed `->`, not at the `:`, so the
user gets no hint that parenthesizing the ascription is the fix.
`p if (x : T) -> e` parses as intended.

Send arms do not suffer: their operands are `simple-expr`
(postfix level), which never consumes `:`, so
`send ch v : T -> e` fails honestly with
`expected "->" ..., found ":"`.

## Mitigation shipped with P1-3

When the arm's `->` is missing and the guard / channel just parsed
is a top-level `*Ascribe` whose type is a `*TypeFn`, the E0101 for
the missing `->` carries a note with the parenthesized rewrite —
`p if (x : T) -> e`, `n <- (ch : T) -> e`. The greedy parse itself
is unchanged.

## Why it is a problem

- The error gives no path to the fix: both diagnostics point after
  the swallowed `->`, and the `:` looks blameless.
- Whether `e : T` is even legal outside `Go.conv` is itself
  unresolved (docs/problems/2026-11-01-type-ascription.md); if a
  general ascription survives, this ambiguity is permanent in the
  grammar as written and users will hit it whenever an ascription
  appears in a guard or channel.

## Related

- grammar.ebnf `match-arm`, `recv-arm`, `ascribed-expr`,
  `arrow-type` — internal/syntax `parseMatchArm`, `parseSelectArm`
- docs/problems/2026-11-01-type-ascription.md — forbidding general
  ascription would shrink the surface but not remove it: a guard
  can still legitimately be `Go.conv x : T`

## Possible responses

- Forbid a top-level `:` in a match guard and a receive channel —
  parse those expressions with a no-ascription flag (the same shape
  as `nb` for `{`), so `x : T -> e` fails at `:` with a
  "parenthesize" message. Cost: even an unambiguous `p if x : T`
  would then need parentheses.
- Keep the greedy parse plus the note (current state). Cheap and
  non-breaking; the cost is two diagnostics where one would do.
- Disambiguate in the grammar itself — e.g. guards / channels
  restricted below ascription level, or an arm arrow other than
  `->`. The first is the `nb`-style option above made normative;
  the second is far too disruptive for one edge case.
