# `fn main () -> Unit` in `package main` is the Go entrypoint

- 状態: 採用（`cmd/gom.TestEndToEnd_BuildAndRun` が shim なしで `gom build` → `go build` → 実行まで検証）
- 日付: 2026-11-04
- Resolves: [problems/2026-11-02-fn-main-not-a-go-entrypoint](../problems/2026-11-02-fn-main-not-a-go-entrypoint.md)

## Context

The problem record: `fn main () -> Unit` in `package main` used to
emit `func main(_ struct{}) struct{}`, which `go build` rejects —
"compile a .gom file into a program you can run" failed at the Go
step for the canonical first program.

The convention "a function named `main` in `package main` is the
program entry" is a language rule, and design.md section 2.1
forbids `internal/codegen` from re-deriving language rules from
names — so the decision cannot live in codegen, even though the
emitted shape (`func main()`) is a codegen concern.

## Decision

A top-level `fn` declaration is the program entry point exactly
when all of these hold:

- the file's package is `main`
- the declared name is `main`
- its single parameter is a **discarded** `Unit` binder — the `()`
  surface (`fn main (_: Unit) -> Unit` qualifies too; a named
  `(u: Unit)` parameter is a usable argument and does not)
- its residual result is `Unit`

`lower` records the verdict on `core.FuncDecl.IsEntry`; the Core
validator (`internal/core/validate.go`) rejects an `IsEntry` flag
that violates the shape; `codegen` emits `func main()` whenever the
flag is set and never inspects the package or symbol name.

Every other `main` — wrong package, a usable parameter, a non-Unit
result — compiles as an ordinary function. In `package main` such a
declaration still emits a Go `func main` with parameters or a
result, which `go build` rejects: the same failure Go itself gives
for a mis-shaped `func main`, no extra machinery.

This tightens the interim codegen rule (`isUnitSig`: any
all-Unit-parameter signature named `main`). That rule silently
dropped *named* Unit parameters from the emitted signature while
the body still bound them — `fn main (u: Unit) -> Unit` produced a
`func main()` whose body referenced the nonexistent `u`. Requiring
the discarded binder makes the convention exactly `() -> Unit`.

## Consequences

- `gom build` of `package main` + `fn main () -> Unit` yields a
  module that `go build` turns into a runnable binary with no shim
  (`cmd/gom` e2e).
- An `(args: List String) -> Int`-style entry shape — or stdlib
  `os.args` — remains future work; the convention is deliberately
  the minimal Go-exact one.
