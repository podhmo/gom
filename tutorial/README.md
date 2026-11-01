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
| 1 | `01-hello` | `package main`, `fn main () -> Unit`, `import go "fmt"`, and discarding a `(T, error)` call with `let _ =` |
| 2 | `02-values` | `let`, Int/Float/Bool/String, `if` as an expression, `"${x}"` interpolation |
| 3 | `03-functions` | `fn` definitions, currying and partial application, lambdas, functions as arguments |
| 4 | `04-match` | Sum types (`type X = \| A \| B`), nested constructor patterns, `if` guards, `==` on ADTs |
| 5 | `05-lists-records` | `List` literals, `[x, ..rest]` recursion, records and `{ r with f = v }`, tuple destructuring |
| 6 | `06-option-result` | `Option`/`Result`, `try` escapes, `|?>` pipelines |
| 7 | `07-go-interop` | `(T, error)` → `Result`, `(T, bool)` → tuple, `_` placeholders, method calls, `|?>` step-name wrapping |
| 8 | `08-pipes-labels` | `\|>`, `<|`, `>>`, `<<`, `~name:` labeled arguments and punning |
| 9 | `09-capstone` | A small port parser combining Go calls, `Result`, `|?>`, and `match` |
| 10 | `10-errors` | Intentionally broken: read the E0301 diagnostic and its suggested fix |

## What you will notice

A few places where the language is not finished yet — each is a
known issue, linked from `TODO.md` or `docs/problems/`:

- The spec's discard form `ignore e` does not parse yet; use
  `let _ = e` instead (see `docs/problems/2026-11-04-ignore-discard-syntax-unparsed.md`).
- `fmt.Println("x")` in a block needs the `let _ =` wrapper because
  `Println` returns `(int, error)` — Gom treats Go's `(T, error)`
  result as `Result T GoError`, never a statement.
- `List.map`, `Option.get`, `Chan`, `Ref` and friends are stdlib
  modules that do not exist yet (task P4-8), so step 5 recurses by
  hand.
- `type Meters = Int` looks like an alias but defines a variant
  named `Int` — a known grammar trap
  (`docs/problems/2026-11-02-type-alias-body.md`).
- One directory is one package, but cross-file references inside a
  package do not resolve yet (each `.gom` compiles standalone), so
  every step is a single file.

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
