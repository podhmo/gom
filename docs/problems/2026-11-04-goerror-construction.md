# `GoError` is uninspectable and unfabricatable from inside Gom

- 状態: 未解決
- 日付: 2026-11-04（V-1 実行可能チュートリアルで発見）

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
   bound by `Err e ->`: in `match errors.New s { Ok _ -> <need a GoError>, Err e -> e }`
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
