# The P1-11 build driver: error boundaries, compilation unit, and CLI shape

- 状態: 採用
- 日付: 2026-11-02

## Context

P1-11 wires the phases into `internal/build` (the pipeline driver) and
`cmd/gom` (`build` / `check` / `dump`). The phase APIs leave four
questions to the driver: where to stop on errors, what the unit of
compilation is, what happens to `.gomi` inputs, and where output goes.

Verification: [sketch: build driver](../sketch/2026-11-02-p1-11-build-driver.md).

## Error boundaries: a phase runs only on error-free input

`Compile` runs parse → resolve → check → pattern → lower → codegen and
stops at the first phase whose diagnostics contain an error. Warnings
never stop a phase.

Rationale — the phase contracts all assume error-free input:

- `syntax.Parse`: "the result of a parse with errors is never passed
  to later phases" — the recovered tree contains `Hole` placeholders.
- `check.Check`: "when the diagnostic list contains errors … later
  phases must not consume it". Verified: `pattern.CheckFile` panics on
  an errored `check.File` (a `fn` whose body the parser never produced
  leaves a nil `Body`, and `walkExpr` hits `panic("unreachable")`).
- `lower.File` emits `E0901` "contract gap" diagnostics for input that
  violates its contract; running it on a file that failed earlier
  phases would turn user errors into compiler-bug-looking output.

Running `check` on a resolve-errored file was also considered — check
is deliberately "total over resolver gaps" — but it produces cascade
diagnostics that mislead: an unresolved `let x = y` earns a
follow-on `E0407` "type annotation required: the type of `x` could not
be determined" (the fix is defining `y`, not annotating), and a member
selection on a failed import earns `E0401` "module `pkg` has no
member" on top of the `E0203` already reported at the import. The
Go-toolchain alternative of showing every phase's errors in one run
was rejected for now — per-file stop is simpler and never wrong; the
design doc's "以降のフェーズはエラーのない入力だけを受け取る" reads
the same way.

## Compilation unit: one file

Each input file compiles independently through the whole pipeline —
`syntax`/`resolve`/`check`/`pattern`/`lower`/`codegen` are all
single-file APIs (resolve documents "resolution is single-file"), and
one `.gom` produces one `.go`.

The CLI accepts `.gom` files and directories; a directory expands to
its `*.gom` files (sorted, non-recursive). `gom check` also accepts
`.gomi` files and includes a directory's `*.gomi` in the expansion.

**Limitation**: files of one package do *not* share a top-level scope
— `a.gom` referencing `b.gom`'s definitions reports `E0201`. Sharing
requires either a package-wide resolve pass or sibling `.gomi`
loading; filed in open-questions as future work.

## `.gomi` inputs

`gom check` accepts `.gomi` and validates the interface itself
(`syntax.ParseInterface` → resolve → check). Interface declarations
have no bodies, so the expression-level phases — pattern, lower,
codegen — do not run on them; verification found `pattern.CheckFile`
panics on a bodiless `fn` (its `walkExpr` exhaustiveness switch hits
`panic("unreachable")`), which is what prompted making this boundary
explicit rather than phase-by-phase. `gom build` refuses `.gomi`
with a command-line error — an interface produces no Go output.
Implementation↔interface conformance is P4-1 (`internal/iface`),
unchanged by this decision.

## Output naming

- `gom build foo.gom` writes `foo.go` next to the input.
- `-o dir/` places every output under `dir` (created if missing).
- `-o file.go` with a single input names the output file; with
  several inputs `-o` must be a directory.
- `-o -` is rejected: stdout carries the spec-12 diagnostic stream,
  and generated code on it would corrupt the stream.
- `gom build` does not write `_test.go` — spec section 11.6 assigns
  test-code output to `gom test`; the bytes stay on the compile
  result for it.

## Message stream

Per spec section 12 both `check` and `build` emit the whole report —
`diagnostic` objects then one `summary` — to **stdout**, in JSON Lines
by default or text with `-format text` (also `--format=text`).
Command-line failures (unreadable inputs, bad flags, output-write and
emitter failures) go to stderr and exit 2. Exit codes are
`diag.ExitSuccess` (0) / `ExitError` (1) / `ExitInternal` (2):
`report.ExitCode()` for diagnostic-driven results, 2 for driver
failures.

`gom dump -phase=syntax|resolve|check|core FILE` is a developer tool
outside the spec's command table. It prints the tree dump of the
requested phase's artifact to stdout and routes the diagnostic report
to **stderr** so the dump stays machine-clean; exit codes are the
same. Diagnostics never suppress the dump — a partially valid tree is
exactly what the tool exists to inspect.

## The import seam

`build.Options` forwards the phases' own extension points without
interpretation:

| field | feeds | default |
| --- | --- | --- |
| `Loader resolve.Loader` | `resolve.Config.Loader` | nil — every `import`/`import go` reports E0203 |
| `Modules` | `resolve.Config.Modules` | nil |
| `Sigs check.SignatureSource` | `check.Check`'s src | nil — builtins only |
| `Lower *lower.Env` | `lower.File` env (`Types`, `FuncArity`, `GoSig`) | nil env; `Lines` is always set from the input's own source |
| `Pattern *pattern.Options` | `pattern.CheckFile` | zero options; `Resolver` defaults to the input's source |

P1-9's `internal/goimport` is expected to provide one value that
satisfies `Loader`/`SignatureSource`/`lower.Env.GoSig` — the driver
needs no change to accept it. Test harnesses inject `MapLoader` +
`MapSource` stubs the same way.
