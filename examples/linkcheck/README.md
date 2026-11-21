# Link checker CLI, written in Gom

This example extracts `http`/`https` URLs from the given text files and
**HEAD-requests every unique URL concurrently**, then reports the ones
that did not answer alive, in first-seen order. It exists to demonstrate
the three concurrency constructs of the language — `go { }`, channel
types, `select { }` — plus `Task`, none of which the tutorial or
`testdata/realworld` covers. Checking links sequentially would be
correct but slow, and an overall deadline cannot be written without
`select`: concurrency is what the program is *for*.

## Build and run the tests

Go 1.27+ is required. From the repository root:

```sh
examples/linkcheck/build.sh /tmp/linkcheck
/tmp/linkcheck --help
go test ./examples/linkcheck -v
go test ./...
```

The test compiles every `.gom` file of this directory as one package
using the real Gom compiler pipeline, runs the Gom `test` blocks,
builds the generated Go into a CLI, and executes the CLI against local
`httptest` servers — including a slow server for the deadline path and
a request-counting one for the concurrency bound and the HEAD method.
No external HTTP request is ever made.

## CLI contract

```text
linkcheck [--concurrency N] [--timeout D] [--all] [--] FILE...
```

- `FILE...`: one or more UTF-8 text files. Every `http://`/`https://`
  URL is extracted (Markdown links and bare URLs alike); duplicates are
  checked once and reported with every referencing file. Use `--`
  before a filename starting with `-`.
- `--concurrency N`: at most N requests in flight at once (default 8),
  enforced by a buffered channel used as a semaphore.
- `--timeout D`: an overall deadline for the check phase, written as a
  Go duration (default `30s`). URLs that never finished are reported as
  `FAIL timed out`. Each single request also has a fixed 10-second
  timeout of its own.
- `--all`: also print the `ok` lines; by default only failures print.
- Exit 0: every URL answered with status < 400. Exit 1: any broken,
  unreachable, or timed-out URL — or an input/usage error.
- Result lines go to stdout: `ok|FAIL <status-or-error> <url> [refs]`.
  A one-line tally goes to stderr.

## Layout

The program is one Gom package split along the operation tree
(`parse → read → extract → check → report`):

| File | Responsibility | Concurrency inside |
| --- | --- | --- |
| `main.gom` | `main`/`run` orchestration, exit status | none |
| `args.gom` | `Command`/`Options`, argument grammar, usage | none |
| `files.gom` | UTF-8 reads of every input file | `Task.spawn`/`Task.await` per file |
| `extract.gom` | URL regexp, dedup, `Target` with first-seen index | none |
| `check.gom` | `Verdict`/`Outcome`, HEAD probe, the pipeline | `go`/`Chan`/`select`/`time.After` |
| `report.gom` | verdict rendering, tally, exit code | none |

Pure parts (argument grammar, URL extraction, order restoration,
verdict classification, counting) carry `test` blocks next to their
definitions; process and network behaviour is covered by
`integration_test.go`.

## The concurrency story the code tells

- **Workers are `go` blocks, and a `Result` flows through the channel
  as a value.** `go { }` is not a `try` boundary (E0702), so each
  worker sends a finished `Outcome`; `try` stays inside `head`, an
  ordinary `fn`.
- **A buffered channel is the semaphore.** `Chan.make concurrency`
  bounds in-flight requests; the launcher acquires a slot before
  spawning, which bounds live goroutines too.
- **`select` + `time.After` is the global deadline.** The timer channel
  is made once *outside* the receive loop — a `select` evaluates its arm
  expressions on every entry, so calling `time.After` inside it would
  restart the deadline at each receive.
- **Fan-in is unordered; order is a pure function.** Outcomes carry the
  target's first-seen `index`; `ordered_outcomes` places them back and
  marks the gaps `Timeout`. That is the function the `test` blocks pin
  down.
- **Nothing mutable is shared.** The linter's sharing rules (W0701/W0702)
  never fire: only channels and immutable values cross goroutine
  boundaries — each `head` call builds its own `http.Client` rather than
  sharing a `Ptr` — and `results` is buffered to `n` so a worker can
  always finish even after the deadline.

Findings and the evaluation against the example plan are in
[the implementation report](../../docs/sketch/ja/2026-11-21-linkcheck-example.md).
