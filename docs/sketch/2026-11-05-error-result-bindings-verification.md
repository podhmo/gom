# Verification: lone-`error` declared views and standard bindings

Date: 2026-11-05. Covers
[error-result-bindings](../decisions/2026-11-05-error-result-bindings.md)
and [result-pipe-wrap-frame](../decisions/2026-11-05-result-pipe-wrap-frame.md).

## What the prototype ships

- `GoResultsErrValue` + `ErrorFromGo`: a lone `error` declared
  `-> GoError` emits `v := call; if v == nil { panic("gom: <f>
  returned nil error — its binding declares a never-nil GoError
  result") }`. The shape's mandatory consumer is the checker —
  a bare `CallGo` with `GoResultsErrValue` is a validator
  violation, so the check cannot be dropped.
- `Loader.admitResults` is the single classification extern
  binding and standard bindings share: a lone `error` admits
  `Result Unit GoError` (default), `Option GoError`, `GoError`;
  `(T, bool)`, `(T, error)`, `*T`/`Option`, tuples unchanged.
  `Option Int` over `error` reports
  "`os.Remove` returns error — an `Option` view's payload is
  `GoError`, not Option Int" (E0509).
- `internal/goimport/stdbind.go`: the curated table. Inaugural
  entries — `errors.New`, `fmt.Errorf` → `GoError`;
  `errors.Join`, `errors.Unwrap` → `Option GoError`. Entries apply
  only to package functions in `Loader.member` (methods cannot
  pick them up through `sigOf`); a drifted entry marks the member
  Hidden with the drift reason.
- `rt.StepError`: the `|?>` wrap frame — `Step` (source name or
  `file:line`), `Err`, `Error()` keeps `step: message`,
  `Unwrap()` returns `Err`. Emission is `rt.StepErr(name, err)`
  where `fmt.Errorf("name: %w", err)` used to be.

## Compile-and-run evidence

`testdata/realworld/errwalk` (built by `gom build`, run by
`cmd/gom.TestEndToEnd_RealWorldPrograms`) prints:

```
is-syntax: true          errors.Is(e, strconv.ErrSyntax)
wrapped-is: true         Is through fmt.Errorf %w
as-numerr: true          errors.As into Ptr (Ptr strconv.NumError)
as-func: Atoi            Ptr.get target → *NumError → .Func
unwrap-is: true          Unwrap over the %w layer
base-unwrap-is: true     *NumError.Unwrap() gives ErrSyntax
leaf-unwrap: none        Unwrap on bare errors.New → None
join-is: true            Join of two non-nil → Some
join-text: strconv.Atoi: parsing "x": invalid syntax\nextra
pipe-text: strconv.Atoi: strconv.Atoi: parsing "x": ...
frame-as: true           errors.As into Ptr (Ptr rt.StepError)
frame-step: strconv.Atoi structured step name read back
```

`errwalk panic` runs an `extern go "os" { fn Remove -> GoError }`
claim over a successful remove (nil return): the process dies with
`declares a never-nil GoError result`, exit 2 — the never-nil check
fires as designed. The same program's in-file extern also
demonstrates the documented override: a file's `extern` binds
against the raw signature and overrides the standard binding.

## Spec fixtures and goldens

- `testdata/spec/ok_error_views.gom` — all four bindings plus
  `errors.Is`/`errors.As` compile clean.
- `testdata/spec/ok_extern_error_views.gom` — `errors.New` restored
  to `Result Unit GoError` by extern, `os.Remove` claimed
  `-> GoError`, `os.Open` `(T, error)` unchanged.
- `testdata/spec/E0509_extern_error_shape.gom` and
  `E0509_extern_option_wrong_payload.gom` — the two lone-`error`
  rejections pin the new messages.
- `testdata/golden/error_views.golden.go` — all three boundary
  shapes side by side.
- `result_pipe*.golden.go` regenerated for `rt.StepErr`; rendered
  error text is unchanged (the errwalk `pipe-text` assertion pins
  the identical wording, double name included).

## Walls and leftovers

- `fmt.Errorf` variadic `...any` works end to end — the draft's
  claim holds (the probe compiled and ran before this report).
- `errors.As` into `*error` (`Ptr GoError` target) and `**T`
  (`Ptr (Ptr T)`) both work; the `errors.As`-family "an `any`
  that must be a non-nil pointer" contract is still unstated —
  `ptr-t-no-deref`'s residual, future work.
- `Option (Ptr T)` stays for `*T` results; `Option GoError` is the
  only `Option` over a non-pointer — the widened admission is
  error-shaped, not a general Option-over-anything rule.
