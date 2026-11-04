# Post-argument selector binding: `x.M(1).N(2)` chains like Go

- 状態: 採用
- 日付: 2026-11-05
- 検証: [sketch/2026-11-05-post-arg-selector-binding-verification](../sketch/2026-11-05-post-arg-selector-binding-verification.md)

## Background

`x.M(1).N(2)` did not parse as "call `x.M(1)`, then select `.N` on
the result". The postfix selector bound at tighter precedence than
the application's argument tuple, so the surface read `x.M ((1).N
(2))`-ish: `.N` applied to the argument tuple — the second selector
looked up `N` on `(Int, Int)`, found nothing, and the call itself
saw the wrong arity. The same mis-parse hit every `f(e).m` shape:
`f(x).y` was `f ((x).y)`, never `(f x).y`
([problems/2026-11-05-method-chain-select-binds-args](../problems/2026-11-05-method-chain-select-binds-args.md)).

Spec 8.1.3 says "メソッド呼び出しは `x.M(args)`" and is silent on
chains. `a.b(c).d(e)` is idiomatic Go/OCaml — users will write it —
so the silent mis-parse produced confusing tuple-member diagnostics
instead of a chain.

## Decision

A `.` that follows a parenthesized application argument belongs to
the *application*, not to the argument. In grammar.ebnf terms:

- `apply-expr` gains a trailing chain: `postfix-expr { app-arg }
  { "." member-name { app-arg } }` — `x.M(1).N(2)` parses as
  `((x.M 1).N) 2`, left-associative, exactly the shape
  `(x.M(1)).N(2)` already produced.
- An `arg-expr` (an argument's value) is `paren-expr | simple-expr`:
  a leading `(` stops at the `)`, so its `.` escapes to the
  application chain. Every other argument keeps the postfix rule —
  `f x.y` is still `f (x.y)`.

To select on the parenthesized value itself write `(e.f)` or drop
the parens — `f e.f` binds the `.` inside the argument already, so
the old reading of `f (e).f` has a shorter spelling.

## Consequences

- `t.AddDate(1, 0, 0).AddDate(0, 1, 0)` type-checks and runs,
  matching Go's chain (verified in the sketch below, eval and
  compiled agree through the `time.Time.AddDate` stub).
- The single-call path is unchanged: `x.M(1)` still parses as
  `(x.M 1)` with the same Apply node — `parseAppArgValue` only
  skips the postfix `.` loop for a `(`-led argument.
- `f (e).m` changed reading: it was `f ((e).m)`, it is now
  `(f e).m`. Selecting on a parenthesized argument spelled
  `f (e.m)` or `f e.m` still works; a repo-wide grep found no
  `.gom`/`.gomi` source relying on the old shape.
- Uniform for labeled arguments: `f ~a: (v).m` is `(f ~a: v).m`.
- `send` arms are unaffected — their operands are simple-expr with
  no enclosing application, so `send ch (f x).y` still selects `.y`
  on `(f x)`.

## Alternatives considered

- Keep the current precedence and document the parenthesization
  requirement (`(x.M(1)).N(2)` always worked). Rejected: it leaves
  an attractive nuisance — `x.M(1).N(2)` looks correct and produces
  tuple-member diagnostics far from the real cause.
- Defer `.` after *any* argument, not just parenthesized ones.
  Rejected: `f x.y` must stay `f (x.y)` — juxtaposed selectors on
  arguments are load-bearing everywhere (`f p.name`).
- Re-parenthesize at a later phase (resolve/check rewrite
  `Select`–`Apply` shapes). Rejected: the mis-parse is a grammar
  fact — downstream phases would have to un-apply the tuple
  selector heuristically, reintroducing exactly the guessing the
  CallInfo contract bans.
