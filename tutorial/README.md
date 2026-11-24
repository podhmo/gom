# Gom tutorial

A progressive, executable tour of the Gom language as it works
*today*. Every numbered directory is a real program: `gom build`
compiles it to Go, and the `go` toolchain runs the result.

Gom is a statically typed, expression-oriented functional language
that transpiles to Go. The generated code is not meant to be clean,
idiomatic Go, but it is meant to be readable enough for a human to
inspect and debug. To run a program you need two steps:

```sh
cd tutorial/01-hello
gom build main.gom   # writes main.go next to main.gom
go run .           # or: go run main.go
gom run .          # shortcut: build + link + run in one step
```

Other toolchain commands: `gom check FILE` type-checks without
emitting Go, `gom test DIR` runs `test` blocks (step 17),
`gom explain E0301` expands a diagnostic code, `gom fmt -w FILE`
formats, `gom fix FILE` applies safe suggested fixes.

Steps share this repository's Go module, so the generated code's
`rt`/`stdlib` imports resolve directly. `run.sh`/`go test` instead
assemble a scratch module per step (`module tutorialrun/<step>` with
`replace github.com/podhmo/gom => <repo root>`) for `go run`.
`main.go` is generated, not committed.

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
| 13 | `13-loops` | `for pat in xs` over `List`/`View`/tuples, `List.slice`/`List.view`, and `@tailrec` |
| 14 | `14-stdlib` | Builtin `List`/`Option`/`Result` module surfaces and `import "cmp"` comparison dictionaries |
| 15 | `15-mutable` | `Ref` cells captured by closures and `Array` build buffers |
| 16 | `16-scope` | `Scope.run`/`defer`/`on_err` cleanup and `run_result`/`defer_err` |
| 17 | `17-tests` | `test` blocks, `@test` helpers, `assert`/`assert_eq`, and `gom test` |
| 18 | `18-concurrency` | `go { }`, `Chan`/`RecvChan`/`SendChan`, `select` receive/send/`default` arms, `Task` |
| 19 | `19-go-boundary` | `Ptr.new` + field writes for Go structs, `Go.conv`, `Go.nil`, `Go.Slice`, `Go.Map`, `..xs` spread |
| 20 | `20-go-callbacks` | `Go.funcN`/`Go.fnN` conversions, `@method` interfaces, `Go.Any`/`Go.try_as` |
| 21 | `21-sugar` | `type alias`, record spread `~..opts`, and labeled lambdas |

## What you will notice

A few places where the language is not finished yet — each is a
known issue, linked from `TODO.md` or `docs/problems/`:

- Use `ignore e` when intentionally discarding a non-Unit value. For
  example, `ignore (fmt.Println "x")` discards the `Result Int GoError`
  produced by Go's `(int, error)` return. Discarding also discards the
  error; handle the Result with `match` or `try` when failure matters.
- `type Meters = Int` looks like an alias but defines a variant
  named `Int` — the real spelling is `type alias Meters = Int`
  (step 21, `docs/problems/2026-11-02-type-alias-body.md`).
- A bare function alias is monomorphic even when its target is a
  polymorphic function. Top-level lambda values can be polymorphic;
  local let bindings cannot (step 11).
- A failed `|?>` produces an Err value and skips the remaining pipeline
  steps. Code after the pipeline continues, including inside a Unit
  function; use `try` when the enclosing function should return Err
  immediately (step 12).
- Every step is a single file for simplicity, but one directory is
  one package: `gom build DIR` compiles all of its `.gom` files
  together — the programs under `examples/` are built that way.
  `.gomi` interface files (spec 7) additionally decide what a
  non-`main` package exports; `gom info` drafts them.

## Larger use cases

- The [Gist upload CLI](../examples/gist-cli/README.md) combines argument parsing,
  files, JSON, HTTP, and errors in a larger Gom program. Its integration tests
  compile the CLI and run it against local fake servers for creation and update;
  they do not call GitHub.
- The [link checker](../examples/linkcheck/README.md) is a concurrent CLI built
  from step 18's pieces: a worker pool over a buffered-channel semaphore,
  `select` + `time.After` for the global deadline, and first-seen ordering
  restored by a pure function.
- The [recursive grep](../examples/rgrep/README.md) adds a multi-stage
  pipeline, `select` *send* arms and `default` peeks, `Chan.close` cascading
  shutdown, and `signal.NotifyContext` for Ctrl-C cancellation.

## Layout

```
tutorial/
  NN-step/
    main.gom        the program
    expected.txt    its exact stdout when run
  10-errors/
    badmatch.gom        intentionally non-exhaustive match
    expected-check.txt  the diagnostic code `gom check` must report
  run.sh            builds gom, runs + diffs every step
  tutorial_test.go  the same driver under `go test ./tutorial/`
```
