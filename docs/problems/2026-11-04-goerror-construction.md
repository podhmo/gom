# `GoError` is uninspectable and unfabricatable from inside Gom

- 状態: 一部解消 — `GoError` values are inspectable now (`e.Error()`
  resolves and emits `e.Error()`, and error-typed package vars such
  as `strconv.ErrSyntax` work as `errors.Is` targets). Fabrication —
  producing a `GoError` from nothing — is still open.
- 日付: 2026-11-04（V-1 実行可能チュートリアルで発見）

## Update: inspection is fixed

`Loader.MethodOf` now admits the builtin `GoError` receiver —
the one Go-typed receiver that has no `types.Named` entry in
`l.named` — and resolves it through the universe `error`
interface's method set. `check.methodOf` admits the same receiver
shape. As a result:

- `match r { Err e -> e.Error() }` compiles and emits `e.Error()`
  (locked by `TestE2EGoErrorMethod` in
  `internal/goimport/pipeline_test.go` and `TestMethodOfGoError` in
  `internal/goimport/loader_test.go`).
- `errors.Is(e, strconv.ErrSyntax)` works end to end — error-typed
  *package vars* like `strconv.ErrSyntax` now load, so sentinel
  targets are obtainable (the same batch's package-var/const fix;
  `TestE2EGoPkgVars`).
- Other interface-typed receivers were never actually blocked:
  named interfaces (`io.Reader` etc.) reach `l.named` through the
  ordinary `types.Go` path and their method sets already worked.
  The universe `error` was the sole gap.

What remains is fabrication, "why it matters" 2: `errors.New s`
still returns `Result Unit GoError`, so the program still cannot
*produce* a `GoError` from nothing — error-typed package vars like
`strconv.ErrSyntax` now exist as `GoError`-typed expressions, but
they are Go's own sentinels, not fresh errors. The constructor
directions below (a `Go.error` builtin, an `extern` binding, or
revisiting the single-`error`-return mapping) are all still open.

## Original report (inspection part retained for the record)


## Situation

Spec 8.3 maps Go's `error` to the builtin `GoError` type, and a Go
function returning only `error` maps to `Result Unit GoError`. Two
holes follow from that mapping:

1. **No way to inspect.** `e.Error()` on a `GoError` fails E0401
   ("GoError has no field or method `Error`") — the method set of
   the underlying `error` interface is not exposed. The only way to
   see an error's message is to splice it (`"${e}"`) or print it.
   `errors.Is` / `errors.As` are unusable in practice: they need a
   *target* error value, and there is no way to obtain one (see 2).
   Verified: `tutorial/` probes — `match r { Err e -> e.Error() }`.

2. **No way to fabricate.** `errors.New "msg"` returns
   `Result Unit GoError`, so the error value lives only in the `Err`
   position. There is no expression of type `GoError` except one
   bound by `Err e ->` (or a Go package var like
   `strconv.ErrSyntax`, which is Go's own sentinel, not a fresh
   error): in `match errors.New s { Ok _ -> <need a GoError>, Err e -> e }`
   the `Ok` arm cannot produce one either — the only `GoError`-typed
   expression is another `Err e` binding. Users cannot write their
   own `Err` payload of type `GoError` without already holding a
   `GoError`. (Contrast: `Result Int String` works fine because
   `String` errors are trivially constructible — step 09 of the
   tutorial uses exactly that workaround.)

## Why it matters

- A Go call failing inside Gom gives a `GoError` the program can
  propagate but never examine: no `errors.Is(err, fs.ErrNotExist)`,
  no message extraction except via display.
- Wrapping in `Err(...)` for your own `Result _ GoError` functions
  is impossible — you must switch to a custom error type (String,
  an ADT) even when the GoError payload would do.
- `todo ()` as a hole would paper over it but is itself broken
  (TODO.md Bugs: builtin calls fail at `gom build`).

## Plausible directions

- Expose the `error` method set on `GoError` (method calls on
  interface-typed Go values — the same gap blocks any
  interface-typed receiver), and
- provide a constructor: a builtin `Go.error : String -> GoError`,
  an `extern` comma-ok binding for `errors.New`, or a documented
  `errors.New`-shaped standard binding that unwraps to `GoError`.
- Spec 8.4's `extern` mechanism could declare `errors.New` as
  `fn New (msg: String) -> GoError` rather than `Result Unit GoError`
  — decide whether single-`error` returns should even map to
  `Result` when the function *creates* rather than *fails*.

## Update (P3-7, 2026-11-05): what the real-world fixture confirms

The [realworld audit](../sketch/2026-11-05-p3-7-realworld-interop-audit.md)
exercised this in programs, not probes:

- `errors.Is (e, os.ErrNotExist)` / `(e, io.EOF)` work end to end —
  sentinel *vars* are the reliable `GoError` source today (`wc`,
  `csvtojson` rely on them).
- The `Err e -> e` / `Ok _ -> todo ()` peel is a working fabrication
  idiom — `cal` and `jsoncfg` use it as `mkerr` to originate their own
  `GoError` inside `Result _ GoError` functions. So fabrication is
  *possible*, only unergonomic and undiscoverable.
- `fmt.Errorf` with `%w` wraps correctly (wrap chain verified through
  `|?>` segments), so a `Go.error : String -> GoError` builtin is
  *not* strictly needed for wrapping — the gap is specifically
  originating an *unwrapped* `GoError` value as a first-class
  expression.
- `errors.As` remains unusable: it compiles (`any` param admits
  `Ptr`) but the required `**T`/`*error` target cannot be produced —
  see [ptr-t-no-deref](2026-11-05-ptr-t-no-deref.md).
