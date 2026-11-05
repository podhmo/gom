# Verification: the evaluator takes the package unit

Companion to
[decisions/2026-11-06-eval-package-unit.md](../decisions/2026-11-06-eval-package-unit.md).
Wave 10 lane 4 (multi-file eval).

## The gap

`eval.New` took one `*check.File`, so a program whose behavior depends
on a sibling file's top-level `let` could not be exercised by the
eval-vs-compiled comparison harnesses — the evaluator saw one file,
the compiled binary saw the whole package.

## What changed

- `internal/eval/eval.go`: `eval.NewFiles(files []*check.File)` is the
  new construction path; `New(f)` wraps it. The input is sorted by
  `File.Sp.File` (the lexical cross-file order spec §5.1 and the
  init-order decision fix), then every file contributes to one shared
  context: `TypeDecl`s register into one ctor/record metadata set,
  `localT` unions across the unit, `FnDecl`s bind into one global env,
  and `LetDecl`s register lazy initializers in the shared `topLets`.
  `Run` drives every file's let/test decls in unit order before
  applying `main`; `ProgramFiles` is the `Program` twin.
- `internal/eval/eval.go`: `SetLineIndexFor(path, idx)` registers a
  line index per file so `|?>` step labels name each step's own
  file:line; `SetLineIndex` remains the single-file spelling.
- `internal/eval/doc.go`: documents the package-unit contract.
- `internal/build/diff_test.go`: `evalDiff` builds the evaluator with
  `NewFiles`; `bindGoStubs` is variadic and walks every typed file.
- `internal/build/compare_test.go`: `evalAnswerFiles` evaluates all
  `build.CompileFiles` results in one evaluator with per-file line
  indexes; `runCompiledFilesCmd` runs the generated files of a unit
  together in one scratch module; `TestEvalMatchesCompiledFiles` adds
  the first multi-file cases.

## Cross-file semantics pinned

- **Sibling `let`, forward order.** `a.gom` declares `let y = z + 1`
  before `b.gom` declares `let z = 40`; the read of `y` forces `z`
  across the file boundary — same lazy mechanism as the same-file
  forward reference, value `41`.
- **Transitive dependency through a sibling fn.** `y`'s initializer
  calls `b.gom`'s `zed`, whose body reads `b.gom`'s `z` — forced the
  same way Go's init dependency analysis counts it.
- **Lexical file order for independent lets.** `let _ = tap "a"` in
  `a.gom` runs before `let _ = tap "b"` in `b.gom` even when the input
  slice lists `b.gom` first — `NewFiles` sorts, caller order is
  ignored.
- **Shared metadata.** `b.gom`'s `type W` registers its constructor
  for `a.gom`'s construction and payload read; `b.gom`'s
  `type Result` shadows the builtin unit-wide (`localT` union), so
  `try` on the local `Err` hits the E9004 "not a Result" branch.
- **Cross-file cycle.** A hand-built unit with `x`/`y` mutually
  recursive across files reports E9003 (the compiled path rejects it
  earlier as E0907 in `lower.CheckInitCycles`).
- **Per-file step labels.** A `|?>` step in `b.gom` names `b.gom:3`
  under `SetLineIndexFor` and `b.gom:25` (byte offset) without —
  pinning both the lookup and the fallback.

## Eval-vs-compiled differential

`TestEvalMatchesCompiledFiles` compiles each two-file unit through
`build.CompileFiles`, evaluates all typed results in one `NewFiles`
evaluator, writes every generated `.go` file into one scratch module,
runs the binary, and compares:

- `sibling_let_init_order`: `a.gom` (`let y = z + 1`, `answer`,
  `main` printing `answer()`) + `b.gom` (`let z = 40`) → `41`.
- `sibling_let_through_fn_and_type`: `a.gom` calls `b.gom`'s `zed`
  and reads a `Score` payload → `41:s42`.

## Verification output

- `go test ./internal/eval/ -run TestProgramFiles -v`: all eight
  subtests pass (0.007s).
- `go test ./internal/build/ -run 'TestEvalMatchesCompiledFiles' -v`:
  both cases pass (eval ≡ binary output).
- `go test ./internal/eval/ ./internal/build/`: full packages green —
  the single-file `New`/`SetLineIndex` callers are untouched and keep
  passing.
- `gofmt -l .`, `go vet ./...`, `go test ./...`: clean at lane end;
  see the lane report.

## Deferred

- The ready-in-declaration-order tie-break among independent
  variables inside one forcing chain is the evaluator's forcing
  order, not Go's; observable only through side-effect order in
  initializers (decision §6). Porting `lower.CheckInitCycles`'s
  vardep graph to `check.Decl`s is deferred until a differential
  finding needs it.
