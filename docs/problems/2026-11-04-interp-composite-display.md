# `"${v}"` on composite values renders differently in the evaluator and generated Go

- 状態: 未解決
- 日付: 2026-11-04

## Situation

String interpolation splices lower to the synthesized builtins
`display`/`str_concat`. `internal/eval`'s `display` renders scalar
values naturally and composites in the evaluator's *comparison*
format — `(1, 2)`, `[1, 2]`, `{ name = "n" }`, `Ok 42`
(internal/eval/eval.go `display`, value.go `String`).

Generated code has no display functions for composites yet
(docs/design.md section 4.6 records `Show` as deferred). Codegen
therefore falls back to `fmt.Sprint(v)` for anything that is not a
scalar builtin, which renders the *Go representation* — `main.T{...}`
field order, `{1 2}` struct literals, `result.Ok/Err` wrapper values —
not the evaluator's format. Scalars (Int/Int32/Int64/Float/Bool/Char/
String/Unit) go through strconv and match the evaluator exactly;
the divergence is limited to ADTs, records, lists, tuples, function
values, and Go types.

## Why it matters

- `"${Ok 1}"` evaluates to `Ok 1` under the evaluator but prints the
  Go struct shape in the compiled binary — the same program reports
  different observable output on the design.md section 7 comparison
  surface.
- User-facing strings that splice composite values are common in
  debugging output — exactly where V-1 tutorial code will reach for
  them.
- The evaluator-vs-binary comparison suite
  (internal/build/compare_test.go) keeps composite splices out of
  `answer` for this reason; the limitation is enforced by avoidance,
  not by a diagnostic.

## Plausible directions

- Generate a `display`/`String` method per declared type at codegen
  (each ADT/record/list/tuple knows its own rendering), or
- give stdlib representations `String()` methods and route composite
  `display` through `fmt.Stringer` where available, or
- spec the interpolation format for composites as "implementation
  defined" for P1 and revisit with `Show`/`Display` in P2.
