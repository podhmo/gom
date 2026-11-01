# Go nullary functions and method values are `-> T` — capturable but not applicable

- 状態: 未解決
- 日付: 2026-11-02

## Situation

A Go function taking no arguments maps to a zero-parameter Gom
function type `-> T` (e.g. `strings.Title` …, a method value
`e.Error` gets `-> String`). Gom's only nullary-callable shape is
`Unit -> T`, applied as `f ()`. The two types are not equal, and the
checker applies a value by unifying its result with `arg -> ?r`, so:

```gom
match strconv.Atoi("x") {
  Ok _ -> ()
  Err e -> {
    let f = e.Error   // f : -> String — this binds fine
    let _ = f ()      // E0401: expected String, got Unit -> ?7
    ()
  }
}
```

`let f = e.Error` itself compiles — the reference lowers to an
adapter `func() string { return e.Error() }` whose `-> String` type
renders `func() string` — but there is no way to *call* the bound
value: `f ()` reports E0401, and `f` never unifies with a declared
`Unit -> String` slot either. Package-level nullary functions hit
the same wall (`let g = rand.Int; g ()`). Calling `e.Error()`
directly works — only the value form is stuck.

## Why it matters

- The mismatch is silent at the reference site: the type the member
  carries (`-> T`) is internally consistent end to end — it lowers
  and emits valid Go — so the failure lands one step later, at the
  first application, where the E0401 message ("expected String, got
  Unit -> ?7") does not mention the real cause.
- Spec 8.1 exposes Go functions as values; a value that can be bound
  but never applied is a half-feature users will reasonably write.

## Plausible directions

- Map Go nullary signatures to `Unit -> T` (one Unit parameter) at
  the goimport boundary: `f ()` then saturates the call naturally
  and lowering emits `f()`. The Go view already knows Arity is 0, so
  the boundary only needs to erase the synthetic Unit parameter.
- Or teach `checkGomApply`/`overApply` that a 0-parameter Func
  saturates on a `()` argument — treats `-> T` as callable by `()`.
  Wider blast radius: it changes what `Unit -> T` means everywhere.
- Or, minimal: detect the shape in the checker and report a clearer
  diagnostic than the generic E0401 ("a Go zero-argument function
  value cannot be applied").
