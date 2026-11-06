# A consumer pin on a deferred `any`-hole result is a valid narrowing

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-check-residuals-verification.md)）
- 日付: 2026-11-06
- 関連: [problem](../problems/2026-11-05-any-hole-deferred-result.md)（これで解消）

## Context

`x.Print(_)` on a still-meta receiver checks provisionally: the
apply's standing result meta `?res` may bind to `String -> ?r` when
`f "a"` runs before the member resolves. The replay then settled
`any -> Unit` against `String -> ?r` and reported
`expected String, got Go.Any` — the eager path accepts the same
program, because `any` on the want side accepts every argument.
The problem asked which reading is right: does the pin bind, or
should `f` keep `any -> Unit`?

The replay also exposed a second divergence: the hole's own meta
never took the parameter type — `unify(pt, hole)` with `pt = any`
accepted without binding (want-side `any` acceptance deliberately
never binds the *got* meta), leaving the placeholder unsolved and
the eventual E0407.

## Decision

The pin is a valid specialization — `f` is monomorphically
`String -> Unit`, the narrower type its consumer wrote down.

- `replayGoApply` binds the hole's meta to the parameter type in
  the binding direction (`unify(hole, pt)`): `?hole := any`. The
  hole genuinely *is* the parameter type; the want-side rule was
  only ever meant to gate *arguments*, not the hole's own shape.
- The settled result unification becomes pin-aware
  (`unifyDeferredResult`): when the standing meta is already a
  function and the settled hole parameter is `any`, the pinned
  parameter conforms to `any` on the want side (a concrete type
  accepts; a still-open meta stays free, matching the eager
  arg-meta behaviour) and the tails unify. `any` is the only
  parameter type this relaxation applies to — a premature pin
  against a concrete parameter still mismatches, as before.
- The emission is unchanged: `f := func(tmp1 string) { x.Print(any(tmp1)) }`
  — the consumer's `String` pin narrows the generated closure
  parameter and the hole boxes it back to `any`, exactly what a
  hand-annotated `let f : String -> Unit` would produce.

Rejected alternatives: nested deferral of every apply whose callee
type is a pendingApply result is a much wider machinery change for
no extra acceptance; making the pin *wrong* (rejecting `f "a"`)
would make the deferred path strictly weaker than the eager one
for a specialization Go's own rules admit.

## Verification

`x.Print(_)` then `f "a"` — eager and deferred — check clean and the
emitted `f("a")`/`x.Print(any(...))` compiles. Spec fixture
`testdata/spec/ok_go_hole_any_deferred.gom` covers both paths; the
`?hole := any` bind removes the trailing E0407 the malformed pin
used to leave.
