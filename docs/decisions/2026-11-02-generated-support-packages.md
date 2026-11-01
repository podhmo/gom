# Generated-code support packages: `rt/` and `stdlib/`

- 状態: draft
- 日付: 2026-11-02

## Context

design.md section 4 places the Go-side representations of builtin
types in `stdlib` and the generated-code glue in `rt`, but left the
exact split open. P1-8 needed concrete packages to generate calls
against; this records what exists and the dependency rule that keeps
it acyclic.

## Layout

- `rt/` — helpers only generated code (and tests) call:
  - `InvalidVariant(typeName, v)` — the panic payload of every
    decision-tree `default` arm and invalid `Result`/`Option` value
    (spec 8.6: values a Go caller can produce but Gom cannot are
    contract violations reported with the type name);
  - `Tuple2`–`Tuple8` — the `(a, b, ...)` value representation,
    `V0`/`V1` fields;
  - `Task[T]` + `SpawnTask`/`Await` — `Task a` on a done-channel
    handle; a task panic re-panics on the awaiting goroutine;
  - `ResultFromGo` / `ResultFromErr` / `OptionFromGo` — the
    non-fused boundary conversions (`(T, error)` → `Result`,
    `(v, ok)` → `Option`, also used by `select` receive arms);
  - `Assert` / `AssertEq` / `AssertOk` — the `assert*` builtins and
    Result-bodied test check (AssertEq uses `reflect.DeepEqual`
    until generated per-type comparators exist, design.md 4.8);
  - `Todo[T]()` — the `todo` hole.
- `stdlib/` — the Go-side representations of Gom's builtin types,
  each written in the shape generated code targets:
  - `stdlib/result` — `Result[T, E]` flat2 (unexported 3-state tag,
    `Value`/`Error` fields, `Tag()`/`IsOk()`/`Unwrap()`/`ToGo()`;
    discriminant constants `ResultTagOk`/`ResultTagErr`/
    `ResultTagInvalid`);
  - `stdlib/option` — `Option[A]` sealed interface +
    `Option_Some`/`Option_None`, marker `isOption() A` (the P0-1
    generic-marker fix);
  - `stdlib/list` — `List[A] []A` slice representation with the
    copying boundary (`FromSlice`/`ToSlice`, spec 3.4).

## Dependency rule

`stdlib/*` packages are leaves: they must not import `rt`. `rt`
imports `stdlib` (the boundary helpers return Result/Option values),
so any `stdlib → rt` edge is a cycle. Packages that need the
invalid-variant panic carry a 3-line local copy of the message
format (`invalidVariant` in `stdlib/result`, `stdlib/option`).

## Tag constants

`ResultTag_Ok`/`ResultTag_Err`/`ResultTag_Invalid` were renamed to
`ResultTagOk`/`ResultTagErr`/`ResultTagInvalid` — the underscore
form reads unlike Go and the emitted case labels are
`result.ResultTag` + the exported constructor name.
