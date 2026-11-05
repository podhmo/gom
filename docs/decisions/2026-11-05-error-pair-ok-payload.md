# `(error, error)` の Ok ペイロード — 派生ビューは `Option`、never-nil は宣言して境界で検査

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-error-pair-ok-payload-verification.md)。spec.md §8.3/§8.4 へ反映済み）
- 日付: 2026-11-05
- 関連: [error-result-bindings](2026-11-05-error-result-bindings.md)（単一 `error` の宣言ビュー。こちらは `(T, error)` の Ok 側を扱う — 同 decision が「untouched」とした範囲の一部を扱う）

## Context

`func F() (error, error)` — a Go signature whose first result is
itself an `error` — derives `Result GoError GoError` under the
`(T, error)` rule. That view is *unsound as derived*: the first
result is nil-able (Go code returns `(nil, nil)` legitimately —
think "partial error, fatal error" pairs), and when it is nil the
generated code places a nil `error` interface into `Ok`. Gom then
has no way to observe the nil — `GoError` offers no `is_nil`, and
`=` requires Eq — so the value smuggles itself downstream until
`e.Error()` panics as a nil-interface trap far from the boundary
(wave8 error-boundary review F-4; implementation-gaps ledger).

[error-result-bindings](2026-11-05-error-result-bindings.md)
established the shape this family of fixes takes: **type-derived
views are honest; strict claims are declared and verified at the
boundary**. A lone `error` defaults to `Result Unit GoError`
(fallible) and admits `Option GoError` (nil-able value) or
`GoError` (never-nil claim, boundary-checked). The `(T, error)`
case needs the same treatment on the *payload* side: the honest
default records that the first result is nil-able, while a
declaration may claim it is not — and pays for the claim with a
generated check.

## Decision

### The derived view of `(error, error)` is `Result (Option GoError) GoError`

When a two-result Go signature has `error` in *both* positions, the
mapped Gom view wraps the Ok payload in `Option`: `nil` in slot 0
materializes as `None` — `option.FromGo(v, v != nil)` — instead of
a hidden nil. `try f()` over it yields `Option GoError`; `match`
sees `Ok (Some e)` / `Ok None` / `Err e`. Every other `(T, error)`
keeps `Result T GoError` — a `*T` payload already exposes its nil
through `Ptr.is_nil`/`Ptr.to_option`; only `error` payloads were
unobservable.

### `(T, error)` admits `Result (Option T) GoError` over a nil-able payload

Symmetrically, an `extern` entry or standard binding may declare
`Result (Option U) GoError` whenever the raw first result is
nil-able — the same gate the lone-result `Option` view uses (`*T`
or `error`, not `[]T`/maps/chans, matching 8.4's existing surface):
the payload `U` must be view-compatible with the raw result's own
view. This is the Ok-side analogue of `Option` over a lone `*T` —
`Result (Option (Ptr File)) GoError` over `func() (*File, error)`
declares "the payload may be absent even when there is no error".

### A declared `Result GoError GoError` is the never-nil claim — checked

`Result GoError GoError` over `(error, error)` stays admissible —
it is exactly the strict claim "the first result is a real error
whenever the second is nil". Like `-> GoError` over a lone `error`,
Go's types cannot prove the claim, so the generated code verifies
it: after the `err` check,

```go
v, err := f()
if err != nil { /* escape / Err */ }
if v == nil {
    panic("gom: pkg.F returned (nil, nil) — its binding declares a never-nil GoError payload")
}
```

A `(nil, nil)` return is a contract violation in the sense of spec
section 8.6 and panics at the boundary. `(nil, err)` keeps the
ordinary `Err` reading — the failure channel wins, the payload is
never materialized. The flag is recorded on the Core consumer —
`NilGuard` on `ResultFromGo` and `EscapeCall` — set by lowering
when the declared Ok payload is the builtin `GoError` (and absent
under `GoResultsErrOnly`, which carries no payload).

### The Ok-side `Option` materialization is a Core conversion

The `error` → `Option GoError` boundary read is `ConvOptNil`, a new
`core.ConvKind` emitted as `option.FromGo(v, v != nil)` — the same
runtime form `OptionFromGo` emits for `GoResultsNil`. Lowering
records it on `ResultFromGo.ValConv` and on the payload `Conv` an
`EscapeCall`'s value flows through, exactly where the `int` →
`int64` `ConvNum` sits today.

## Alternatives

- **Keep `Result GoError GoError` as the derived view, add the
  boundary check unconditionally** — rejected: it makes *every*
  `(error, error)` callee panic on `(nil, nil)`, a return Go code
  produces legitimately (a nil-able first result is ordinary). That
  installs the strict claim as the default — backwards against the
  derived-honest/declared-strict split the family now follows.
- **Keep the derived view, document a Go-side contract** —
  rejected: the hole is *silent*. `Ok(nil)` is undetectable from
  Gom (`GoError` has no nil probe), so the documentation contract
  asks users to police what they cannot observe.
- **`(error, error)` → `(GoError, GoError)` tuple** — rejected:
  the second `error` is the failure channel by Go convention; a
  tuple drops `try`/`|?>` fusion and `Result` semantics for no
  gain — the question is the payload's nil-ability, not the
  shape's.
- **Admit `Result (Option T) GoError` for *every* first result
  (`[]T`, `map`, `chan`, `func`)** — deferred: the existing
  lone-`Option` gate admits only `*T`/`error`, and keeping the two
  gates identical avoids a spec-surface asymmetry. Widening either
  gate is a separate amendment.
- **`Option (Option GoError)` / a dedicated `Maybe` view** —
  rejected: nested Options double the unwrapping for no new
  information; `None` already means "no payload".

## Consequences

- `func F() (error, error)` reads `Option GoError` inside `Ok` —
  honest, observable, and total; `(nil, nil)` is `Ok None`, not a
  panic.
- `try` over the derived view yields `Option GoError`; over a
  declared `Result GoError GoError` it yields `GoError` after the
  boundary check — the fused `EscapeCall` emits the same `v == nil`
  panic between the `err` escape and the payload bind.
- spec 8.3's `(T, error)` row gains the `error`-payload note; 8.4's
  admission list gains `Result (Option T) GoError` for nil-able
  first results and the materialization text gains the
  `option.FromGo`/`v == nil` forms.
- Existing `(T, error)` declarations are untouched: `Result T
  GoError` remains admissible everywhere, and `*T` payloads keep
  their non-Option default (their nil is already observable).
