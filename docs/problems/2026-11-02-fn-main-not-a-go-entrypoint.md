# `fn main` does not produce a Go entrypoint — generated `package main` cannot be a program

- 状態: 未解決
- 日付: 2026-11-02

## Situation

Found while building the P1-11 end-to-end executable test
(`cmd/gom.TestEndToEnd_BuildAndRun`).

The language's calling convention lowers `fn f (x: A) -> B` to
`func f(_ A') B'` — Unit is `struct{}`, so `fn main () -> Unit`
becomes

```go
func main(_ struct{}) struct{} { ... }
```

In `package main` this does **not** compile:

```
./main.go: func main must have no arguments and no return values
```

So `gom build` of the canonical first program
(`testdata/golden/hello.gom`) emits Go that `go build` rejects — the
generated output is well-formed but not a runnable program. The e2e
test works around it by adding a hand-written `main.go` shim in the
generated package.

## Why it matters

- "Compile a .gom file into a program you can run" is the
  introductory use case; today it fails at the Go step for any file
  that declares `main` in `package main`.
- It cannot be fixed inside `internal/codegen`: the rules forbid
  codegen from judging language semantics, and "a function named
  `main` in `package main` lowers to Go's entrypoint" is a language
  rule, so it has to be expressed in Core IR or earlier — which makes
  it a spec-level question.

## Plausible directions (not decided)

- Spec rule: `fn main` in `package main` with a designated signature
  (e.g. `() -> Unit` or `(args: List String) -> Int`) is lowered to
  `func main()` — decided in the language, executed by lower/core.
- Library approach: an `os.args`/entry helper in `stdlib` plus a
  documented convention, no special-casing.
- Tooling approach: `gom build` emits a `main.go` wrapper next to the
  package output.

Related: [decisions/generated-go-conventions](../decisions/2026-11-02-generated-go-conventions.md),
[design.md](../design.md) section 4.5 (main/entry handling is not yet
specified), the P1-11 build-driver sketch
([sketch](../sketch/2026-11-02-p1-11-build-driver.md)).
