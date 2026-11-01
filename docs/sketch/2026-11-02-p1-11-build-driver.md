# P1-11: build driver and `gom build`/`check`/`dump` — implementation and verification report

- 目的: implement `internal/build` (the pipeline driver) and
  `cmd/gom` `build`/`check`/`dump`, and verify end to end — generated
  Go compiled and run by the real toolchain.
- 関連: [decisions/build-driver](../decisions/2026-11-02-build-driver.md),
  [spec.md](../spec.md) section 12, TODO.md P1-11.
- 実装: `internal/build/{doc,build}.go`,
  `internal/build/{harness,build}_test.go`, `cmd/gom/{main,cli}.go`,
  `cmd/gom/{main,cli}_test.go`; date 2026-11-02, go1.27.1
  linux/amd64.

## What was built

`build.Compile(Input, *Options) (*Result, error)` runs one file
through parse → resolve → check → pattern → lower → codegen, in that
one-way order, and returns the collected diagnostics plus every phase
artifact the run reached (`File`, `Resolved`, `Typed`, `Core`,
`Output`). Options are pure forwarding of the phases' seams —
`Loader`, `Modules`, `Sigs`, `Lower`, `Pattern` — which is the slot
P1-9's `internal/goimport` plugs into. `CheckOnly` stops after the
pattern check (`gom check`).

`cmd/gom` parses flags, expands file/dir arguments, calls Compile per
file, writes `.go` for `build`, and emits one `diag.Report` per
invocation — JSON Lines or text to stdout for `check`/`build`, and to
stderr for `dump` so `-phase` output stays clean. Exit codes are
`diag.ExitSuccess`/`ExitError`/`ExitInternal` exactly as spec 12
requires; driver I/O failures print to stderr and return 2.

## Verification results

| check | result |
| --- | --- |
| spec suite (`internal/build.TestSpec`) | 8/8 pass through the real driver — every fixture is now live, none skipped |
| golden suite (`TestGolden`) | `hello.gom` → `hello.golden.go` regenerated from real codegen output and compared literally |
| driver unit tests | all pass — per-boundary artifact assertions below |
| cmd/gom tests | all pass — JSON/text streams, exit codes, output naming, dir expansion, `.gomi` handling |
| e2e (`TestEndToEnd_BuildAndRun`) | `gom build` → write module → `go build` → run binary → prints `gom answer: 42` |
| `gofmt -l`, `go vet`, `go test`, `make check` | clean (see commit report) |

## Error boundaries — what verification changed

The draft decision said "a phase runs only when input is error-free".
Two findings shaped the details:

1. **`.gomi` stops after check, not pattern.** `pattern.CheckFile`
   panics (`walkExpr` hits `panic("unreachable")`) on a `check.File`
   whose `fn` decls have nil bodies — interface declarations never
   have bodies. The boundary moved: interface inputs run
   parse → resolve → check and stop; the expression-level phases have
   nothing to analyze. The decision doc was updated to say so.
   `pattern` could instead learn to skip nil bodies — left as a
   phase-contract question, not a driver workaround.
2. **Best-effort artifacts are still returned but never consumed
   downstream.** resolve and check return trees alongside errors;
   the driver hands them to `Result` (for `gom dump`) but does not
   feed them to the next phase — check on a resolve-errored tree
   produces cascade diagnostics that blame the wrong thing.

## Fixture work

| fixture | change |
| --- | --- |
| `E0201_undefined_ctor`, `E0201_undefined_name`, `E0202_duplicate_param`, `E0203_import_missing`, `E0301_nonexhaustive_match`, `E0802_test_only_ref`, `ok_shapes`, `golden/hello` | `.case.json` `skip` removed — all run through the real driver |
| `E0704_select_recv_pattern` | **renamed to `E0101_select_recv_pattern`** — the parser rejects `Some x <- ch` with E0101 before check could reach the E0704 condition; E0704 remains reachable via non-source inputs (pattern-phase unit tests) |
| `E0202_duplicate_param.gom` | input changed to a form the parser accepts (`fn f (x: Int) (x: Int) -> Int = x`); expected regenerated — resolve reports the duplicate with `first_defined_at` |
| `E0301_nonexhaustive_match.gom` | added the `is_zero` helper the guard references; expected regenerated (same missing-pattern list, notes, and fix) |
| `hello.golden.go` | regenerated: `//line` directives, `func main(_ struct{}) struct{}` (see the `fn main` problem doc) |

## The fmt stub

`harness_test.go` injects `go:fmt` via `resolve.MapLoader` +
`check.MapSource` (`Println` typed `(args: ..Go.Slice a) -> Unit` —
the fresh type variable accepts any single element type). This is
what `ok_shapes`'s `import go "fmt"` needs until P1-9 lands the real
`internal/goimport`; the stub is the same seam the real loader will
fill. `E0203_import_missing` still passes because `MapLoader` misses
report `reason` data identical to the expected shape.

## e2e proof

`cmd/gom.TestEndToEnd_BuildAndRun`: writes `prog.gom` (an ADT +
recursive `match` evaluator), runs `gom build -o mod`, then `go
build` in `mod` (whose `go.mod` `replace`s `github.com/podhmo/gom`
with this worktree, `GOWORK=off`), runs the binary, and asserts
`gom answer: 42` — the compiled Gom `eval (Add (Num 40) (Num 2))`
returned 42.

A hand-written `main.go` shim calls `answer(struct{}{})` — generated
`fn` is not a Go entrypoint; that is filed as
[problems/2026-11-02-fn-main-not-a-go-entrypoint](../problems/2026-11-02-fn-main-not-a-go-entrypoint.md),
not silently patched in codegen.

## Limitations recorded

- One file compiles alone; sibling `.gom` files of the same package
  do not share a top-level scope (cross-file refs → E0201). Open
  question filed.
- No caching/incremental builds — deferred to P4 by design.
- `gom build` writes only the package source; `TestSource` stays on
  the result for `gom test` (spec 11.6 assigns test output there).
- `import go` resolves only through an injected `Loader`; the real
  driver wiring arrives with P1-9. Without a loader every import is
  E0203.
