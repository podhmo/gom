# Gom tutorial

A progressive, executable tour of the Gom language as it works
*today*. Every numbered directory is a real program: `gom build`
compiles it to Go, and the `go` toolchain runs the result.

Gom transpiles to readable Go. To run a program you need two steps:

```sh
cd tutorial/01-hello
gom build main.gom   # writes main.go next to main.gom
go run .           # or: go run main.go
```

Each step directory is its own Go module — `go.mod` replaces
`github.com/podhmo/gom` with this repository so the generated code's
`rt`/`stdlib` imports resolve. `main.go` is generated, not committed.

To run everything at once:

```sh
tutorial/run.sh                # builds gom, runs + diffs every step
GO=~/go/bin/go1.27.1 tutorial/run.sh   # pick a specific toolchain
go test ./tutorial/            # the same drive as a Go test
```

## Steps

| Step | Directory | Shows |
| --- | --- | --- |
| 1 | `01-hello` | `package main`, `fn main () -> Unit`, `import go "fmt"`, and discarding a `(T, error)` call with `ignore e` |
| 2 | `02-values` | `let`, Int/Float/Bool/String, `if` as an expression, `"${x}"` interpolation |
| 3 | `03-functions` | `fn` definitions, currying and partial application, lambdas, functions as arguments |
| 4 | `04-match` | Sum types (`type X = \| A \| B`), nested constructor patterns, `if` guards, `==` on ADTs |
| 5 | `05-lists-records` | `List` literals, `[x, ..rest]` recursion, records and `{ r with f = v }`, tuple destructuring |
| 6 | `06-option-result` | `Option`/`Result`, `try` escapes, `|?>` pipelines |
| 7 | `07-go-interop` | `(T, error)` → `Result`, `(T, bool)` → tuple, `_` placeholders, method calls, `|?>` step-name wrapping |
| 8 | `08-pipes-labels` | `\|>`, `<|`, `>>`, `<<`, `~name:` labeled arguments and punning |
| 9 | `09-capstone` | A small port parser combining Go calls, `Result`, `|?>`, and `match` |
| 10 | `10-errors` | Intentionally broken: read the E0301 diagnostic and its suggested fix |
| 11 | `11-polymorphic-functions` | Top-level polymorphism, monomorphic function aliases, partial application, captured closures, and recursive alias chains |
| 12 | `12-result-values` | Result-valued pipelines inside a Unit function, skipped steps, continuation after failure, and explicit `try` |

## What you will notice

A few places where the language is not finished yet — each is a
known issue, linked from `TODO.md` or `docs/problems/`:

- Use `ignore e` when intentionally discarding a non-Unit value. For
  example, `ignore (fmt.Println "x")` discards the `Result Int GoError`
  produced by Go's `(int, error)` return. Discarding also discards the
  error; handle the Result with `match` or `try` when failure matters.
- `List.map`, `Option.get`, `Chan`, `Ref` and friends are stdlib
  modules that do not exist yet (task P4-8), so step 5 recurses by
  hand.
- `type Meters = Int` looks like an alias but defines a variant
  named `Int` — a known grammar trap
  (`docs/problems/2026-11-02-type-alias-body.md`).
- A bare function alias is monomorphic even when its target is a
  polymorphic function. Top-level lambda values can be polymorphic;
  local let bindings cannot (step 11).
- A failed `|?>` produces an Err value and skips the remaining pipeline
  steps. Code after the pipeline continues, including inside a Unit
  function; use `try` when the enclosing function should return Err
  immediately (step 12).
- One directory is one package, but cross-file references inside a
  package do not resolve yet (each `.gom` compiles standalone), so
  every step is a single file.

## Larger use case

The [Gist upload CLI](../examples/gist-cli/README.md) combines argument parsing,
files, JSON, HTTP, and errors in a larger Gom program. Its integration tests
compile the CLI and run it against local fake servers for creation and update;
they do not call GitHub.

## Layout

```
tutorial/
  NN-step/
    main.gom        the program
    expected.txt    its exact stdout when run
    go.mod          scratch module for `go run .` (replace -> repo root)
  10-errors/
    badmatch.gom        intentionally non-exhaustive match
    expected-check.txt  the diagnostic code `gom check` must report
  run.sh            builds gom, runs + diffs every step
  tutorial_test.go  the same driver under `go test ./tutorial/`
```
