# Method-call chains bind the selector to the argument tuple, not the call

- 状態: 未決

## 状況

`x.M(1).N(2)` does not parse as "call `x.M(1)`, then select `.N` on
the result". The postfix selector binds at a tighter precedence than
the application's argument tuple, so the surface reads `x.M ((1).N
(2))`-ish: `.N` is applied to the argument tuple — the second
selector looks up `N` on `(Int, Int)`, finds nothing, and the call
itself sees the wrong arity.

Reproduced on a `time.Time` receiver: `x.AddDate(1, 0,
0).AddDate(0, 1, 0)` reports E0401 "the Go callee takes 3
argument(s), got 2", E0401 "tuple type has no AddDate", and the
elementwise mismatches — while the explicitly parenthesized
`(x.AddDate (1, 0, 0)).AddDate (0, 1, 0)` compiles and runs.

This is grammar, not inference: the same mis-parse happens with a
fully resolved receiver, so the deferred member-application work
(P3-4) is not at fault — it surfaced the shape while testing the
`x.M(1).N(2)` replay ordering.

## なぜ問題か

- Spec 8.1.3 says "メソッド呼び出しは `x.M(args)`" — silent on
  chains. `a.b(c).d(e)` is idiomatic Go/OCaml and users will write
  it; today it produces confusing tuple-member diagnostics instead
  of a chain.
- The fix is a grammar/precedence question: either `select` binds
  tighter than the application's tuple argument — `x.M(1).N(2)`
  parses as `(x.M(1)).N(2)` — or the tuple's tail selector is
  re-read as part of the application. Which rule spec 8.1.3 wants is
  a language decision, not an implementation choice.

## 考えられる方向

- Bind the postfix selector tighter than the argument tuple:
  `apply = postfix-expr , "(" args ")"` — `x.M(1).N(2)` parses as
  `(x.M(1)).N(2)`. Matches Go's `x.M(1).N(2)` exactly. Cost: changes
  every existing `f(x).y` parse — though that shape is exactly what
  users expect anyway.
- Keep the current precedence and document the parenthesization
  requirement — cheap, but leaves an attractive nuisance in the
  surface syntax.
