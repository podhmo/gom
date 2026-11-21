# Recursive grep, written in Gom

This example walks directories, greps each file's lines on a fixed
pool of workers, and prints `path:line:text` — with **cancellation**:
`--max-count` or Ctrl-C closes a `done` channel that unwinds every
stage. It exists to demonstrate what `examples/linkcheck` could not
tell: a *multi-stage pipeline* (vs a single fan-in), a *fixed worker
pool* (vs spawn-per-job + semaphore), `select` *send* arms and the
`default` arm, `Chan.close` cascading shutdown, and `signal.NotifyContext`
for graceful stop.

## Build and run the tests

Go 1.27+ is required. From the repository root:

```sh
examples/rgrep/build.sh /tmp/rgrep
/tmp/rgrep --help
go test ./examples/rgrep -v
go test ./...
```

The test compiles every `.gom` file of this directory as one package
using the real Gom compiler pipeline, runs the Gom `test` blocks,
builds the generated Go into a CLI, and executes the CLI against a
temporary directory tree — including a binary file, a missing path,
a `--max-count` run over 50 files to show cancellation bounds the
run, and a real SIGINT against a matcher wedged on a FIFO read to
show the second-Ctrl-C fallback.

## CLI contract

```text
rgrep [--concurrency N] [--max-count N] [-i] [--] PATTERN [PATH...]
```

- `PATTERN`: a Go regular expression. `-i`/`--ignore-case` prefixes
  `(?i)`. A pattern starting with `-` needs `--`.
- `PATH...`: files or directories to search (default `.`).
  Directories are walked recursively; non-UTF-8 files are skipped
  with a warning on stderr.
- `--concurrency N`: size of the matcher worker pool (default 4).
- `--max-count N`: stop printing after N matches *total* (unlike
  grep's per-file `-m`) and cancel the remaining scan.
- Output: `path:line:text` on stdout, `rgrep: <warning>` on stderr.
  Match order is unspecified.
- Exit 0: at least one match. Exit 1: none. Exit 2: any warning or a
  usage/pattern error. Exit 130: interrupted by SIGINT.

## Layout

One package split along the pipeline:

| File | Responsibility | Concurrency inside |
| --- | --- | --- |
| `main.gom` | `main`/`run`, regex compile, exit code | none |
| `args.gom` | `Command`/`Options`, argument grammar | none |
| `grep.gom` | `Match`/`Item`, `grep_text`/`grep_file` | none (pure core) |
| `walk.gom` | recursive walker → `paths` | `send`-arm `select` + `done` |
| `pipe.gom` | pool, closer, printer, cancellation | `go`/`Chan`/`select`/`signal` |

## The concurrency story the code tells

- **Every send is `select`-guarded by `done`.** A producer blocked on
  a full channel wakes on `done` and unwinds — that is the only place
  cancellation needs to act, because a *receive* always ends when the
  upstream channel is closed by the exiting stage above it. So
  receives stay `for x in ch` or plain `select`, and only sends carry
  the extra arm.
- **`close` is the shutdown cascade.** The walker closes `paths`;
  each matcher's `for` then ends and it signals `fin`; `wait_workers`
  closes `results` after the last `fin` — closing earlier would panic
  a still-running sender.
- **`done` has one closer.** `cancel` runs only on the main goroutine
  (cap reached, or SIGINT arm of the printer's `select`), because the
  idempotent-looking `select { _ <- done; default -> close done }`
  is not actually safe under two concurrent callers.
- **`default` arms do non-blocking peeks.** `is_done` lets a matcher
  skip reading the next file after cancellation; `cancel` uses it to
  make closing idempotent.
- **SIGINT becomes a channel.** `ctx.Done ()` from
  `signal.NotifyContext` drops straight into a `select` arm; the
  printer turns it into the same `done` cancel as `--max-count`.
  `signal.Reset` then re-arms the default action so a *second* Ctrl-C
  still kills the process — a cancel cannot reach a worker wedged
  inside a blocking `ReadFile`.
- **Warnings flow through `results` too.** A `Warn` variant keeps
  stderr writes on the single printer goroutine, so output never
  interleaves.

Findings and the evaluation against the example plan are in
[the implementation report](../../docs/sketch/ja/2026-11-21-rgrep-example.md).
