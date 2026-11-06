# Go zero-parameter function *values* apply as `f ()` — the `()` is a call marker

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-check-residuals-verification.md)。spec.md §8.1 へ反映済み）
- 日付: 2026-11-06
- 関連: [problem](../problems/2026-11-02-go-nullary-fn-value.md)（これで解消）

## Context

A Go zero-parameter signature maps to `-> T`, not `Unit -> T`
(problems/2026-11-02-go-nullary-fn-value). The reference `let f =
e.Error` bound and lowered correctly — the method value emits an
adapter `func() string { return e.Error() }` — but there was no way
to *call* the bound value: `f ()` unified the result against
`Unit -> ?r`, reporting `expected String, got Unit -> ?7` (E0401).
The problem named three directions: remap the signature to
`Unit -> T` at the boundary, treat `-> T` as callable by `()` in
the value path, or reject with a clearer diagnostic.

## Decision

A `-> T` *value* applied as `f ()` is a zero-argument application —
the unit literal is the call marker, exactly the way `checkGoApply`
already drops `()` from a Go argument list. Anything else keeps the
ordinary over-application reading (`f x` applies `f`'s result to
`x`; `f (x, y)` is not a call form).

- `checkGomApply` saturates `-> T` with a literal `()` argument to a
  zero-argument `CallUnknown`; `replayGomApply` does the same on the
  deferred pass, so `x.M ()` where `.M` settles to `-> T` works too.
- `let m = x.Error` where `.Error` is still a pendingSel leaves `m`
  on the member's standing meta; `m ()` cannot pick the marker vs
  `Unit` reading early, so the application joins the member's own
  replay (`pendingApply` with `alwaysGom` — the callee is a Var, no
  CallGo can name the receiver).
- Lowering emits a real `f()`: `unknownCall` produces a
  zero-argument `core.CallFunc` when the callee type is `-> T`, the
  one place an Apply node exists with no arguments — every other
  CallUnknown carries at least one. `core` validation keeps the
  canonical one-argument-per-arrow invariant and exempts exactly
  this shape.
- No signature remapping: `Unit -> T` stays Gom's own nullary-callable
  shape (`func(struct{}) T` in Go), distinct from the Go view `-> T`
  (`func() T`). The two never unify — they were never equal — and
  `f ()` on a `Unit -> T` value keeps emitting `f(struct{}{})`.

Rejected alternatives: remapping the boundary type to `Unit -> T`
loses the Go-side shape everywhere (the value's own emission already
relies on `-> T` rendering `func() T`), and a clearer diagnostic
leaves spec 8.1's "Go functions are exposed as values" only half
true. A `-> T` parameter slot stays a `-> T` parameter slot — a
`Unit -> T` value does not unify with it.

## Verification

`let m = (errors.New "x").Error; m ()` and `let r = rand.Int; r ()`
check clean and emit `m()`/`g()` — the generated Go builds.
`x.Error ()` on an unresolved receiver and `let m = x.Error; m ()`
inside a lambda replay to the same zero-argument call. Ordinary
`Unit -> T` calls still emit `f(struct{}{})`. Spec fixture
`testdata/spec/ok_go_nullary_fn_value.gom`, unit test
`TestCallNullaryGoValue`.
