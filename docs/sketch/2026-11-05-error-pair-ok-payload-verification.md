# Verification: `(error, error)` Ok-payload views

Date: 2026-11-05. Covers
[error-pair-ok-payload](../decisions/2026-11-05-error-pair-ok-payload.md).

## What the prototype ships

- `internal/goimport/mapping.go`: a two-result signature with
  `error` in both positions maps to `Result (Option GoError)
  GoError` — the honest nil-able payload. Every other `(T, error)`
  keeps `Result T GoError`.
- `Loader.admitResults`: `(T, error)` admits `Result (Option U)
  GoError` when the raw first result is nil-able (`*T` or `error`,
  the same gate the lone-result `Option` view uses) and `U` is
  view-compatible with the first result's own view. `Result
  GoError GoError` stays admissible over `(error, error)` as the
  never-nil claim. `Result (Option Int) GoError` over
  `(int, error)` is E0509 — the raw payload is not nil-able.
- Core IR: `NilGuard` on `ResultFromGo` and `EscapeCall` records
  the declared never-nil claim; the validator requires
  `GoResultsErr` underneath (a lone `error` carries no payload to
  guard). `ConvOptNil` is the `option.FromGo(v, v != nil)`
  materialization on the Ok side.
- Emission:
  - materialized: `v, err := f(); if err == nil && v == nil {
    panic("gom: f returned (nil, nil) — its binding declares a
    never-nil GoError payload") }; result.FromGo(v, err)`;
  - fused `try`/`|?>`: `err != nil` escapes first, then `if v ==
    nil { panic }`, then the payload bind — the same `option`
    wrap applies via `ConvOptNil` for the `Option` view.

## Compile-and-run evidence

`testdata/realworld/errwalk` (built by `gom build`, run by
`cmd/gom.TestEndToEnd_RealWorldPrograms`) prints for `pair`:

```
pair-0: none        TwoErrs 0  → (nil, nil)   → Ok None
pair-1: some soft   TwoErrs 1  → (soft, nil)  → Ok (Some soft)
pair-neg: err fatal TwoErrs -1 → (nil, fatal) → Err fatal
pipe-pair: some soft           |?> step reads Ok (Some soft)
try-opt: none                  try yields Option GoError = None
```

The declared strict view panics at the boundary in both the plain
`try` and the fused `|?>` step:

```
$ errwalk trypair   # extern BothNil: Result GoError GoError → (nil, nil)
panic: gom: goiface.BothNil returned (nil, nil) — its binding declares a never-nil GoError payload
$ errwalk pipepair  # extern StrictPair: Result GoError GoError → (nil, nil)
panic: gom: goiface.StrictPair returned (nil, nil) — its binding declares a never-nil GoError payload
```

Go → Gom direction (`testdata/gocall`, `TestEndToEnd_GoCallsGom`):
`boundary.Errpair` arrives in Go as
`result.Result[option.Option[error], error]` — `(nil, nil)` is
`Ok` carrying `Option_None`, a soft error `Option_Some`, a fatal
one `Err`. A Go caller hands `Ok (Some e)` / `Ok None` / `Err e`
back through `ErrpairKind` and the three render distinctly.

## Fixture coverage

- `testdata/spec/ok_error_pair_views.gom` — the derived view:
  match/`try`/`|?>` over `(error, error)`.
- `testdata/spec/ok_extern_error_pair_views.gom` — the two
  declared views admitted over `(error, error)`.
- `testdata/spec/E0509_extern_option_pair_shape.gom` —
  `Result (Option Int) GoError` over `(int, error)` rejected:
  the payload is not nil-able.
- `testdata/golden/error_pair_views.gom` — the generated shapes
  above.
- `internal/goimport/extern_test.go` — admission table for the
  new arms.
