# Verification report: `gom test` (P4-3)

- 日付: 2026-11-03
- 対象: [decision: gom-test-message-stream](../decisions/2026-11-03-gom-test-message-stream.md),
  [test-file-emission-layout](../decisions/2026-11-03-test-file-emission-layout.md),
  [inline-tests](../decisions/2026-11-01-inline-tests.md)

Under test: `gom test` (`cmd/gom/test.go`), `go test -json` folding
(`internal/build/gotest.go`), the `test`/`summary.tests` messages
(`internal/diag`), `@test`/`test` routing to `_test.go`, and generated
`assert_eq` display functions (`internal/lower` + `internal/codegen`).
This extends the working notes in
[2026-11-03-p4-3-gom-test](2026-11-03-p4-3-gom-test.md).

## Manual end-to-end runs (real `go test` in a temp module)

Temp module whose `go.mod` has a `replace` pointing at this worktree;
`gom test` was run against real `.gom` inputs:

- **pass / fail / counts**: `test "rect area"` becomes
  `func TestRectArea` and yields
  `{"kind":"test","name":"rect area","func":"TestRectArea","ok":true,"span":{...}}`.
  A failing `assert_eq` reports `ok:false` and `output` containing
  `area.gom:18: gom: assertion failed:` plus `actual:`/`expected:` — the
  `.gom` line survives via `//line`. Summary:
  `"tests":{"run":3,"failed":1,"skipped":0}` with `ok:false`, exit 1.
- **display functions**: `Node (Leaf 1) (Leaf 2)` (ADT, nested-operand
  paren rule identical to eval `writeOperand`), `{ x = 2, y = 2 }`
  (record), `Some 1` / `Ok 1` / `Err "a"` (Option/Result), `[1, 2]`
  (list), `(1, "x")` (tuple) all render in eval `Value.String` form.
  Recursive types (`Node Tree Tree`) display via recursive `gomShow`.
- **`@test` helper**: `@test fn unit_square` lands in `area_test.go`
  only — absent from `area.go` (the design 4.8 example shape).
  `gom build` still type-checks test bodies (`assert_eq` type mismatch
  → E0401) while emitting no test code at all.
- **Result-bodied failure**: a test failing through `try` propagation is
  reported via `rt.AssertOk` as `rec.gom:19: nonpositive` — `.gom`
  position preserved.
- **duplicate names**: two `test "same"` blocks rename to `TestSame` /
  `TestSame2` and both report `name:"same"`.
- **package granularity**: `gom test rec.gom` also ran tests generated
  from sibling `.gom` files, reported with no `span` and `name == func`.
- **stale cleanup**: deleting a file's tests removes its
  generated-marker `_test.go`; a handwritten `_test.go` (no marker) is
  untouched.
- **toolchain failure**: a directory without `go.mod` produces
  `{"kind":"diagnostic","code":"E0001","source":"go",...}` and exit 1;
  test-binary build failures are likewise E0001 with
  `file`/`line`/`col`/`raw` in `data`.
- **`--format text`**: `test "name" ok/FAIL (0.00s)`, failures show
  `--> file:line:col` plus captured output; trailer
  `error: N of M tests failed`.

## Automated coverage

- `internal/build/gotest_test.go`: `FoldGoTest` for pass/fail/skip,
  build-output/build-fail, pending tests folded to failures when the
  binary dies mid-run, package-level output (init panic).
- `internal/diag/testresult_test.go`: `MarshalTestResult` wire shape,
  `summary.tests` presence rules (absent when `Tests` nil; counts
  present even when empty), `ExitCode`/`WriteJSONLines`/`WriteText`.
- `internal/diag/schema_test.go`: pins the `test` definition's required
  set to `{"kind","name","func","ok","elapsed"}`.
- `internal/codegen/show_test.go`: `gomShow` emitted only into
  `_test.go`, `rt.AssertEq` receives display strings, record rendering
  is `{ name = v }`.
- `cmd/gom/test_test.go`: runs `gom test` against the real toolchain —
  `test` objects, no `@test` leakage into production output, Result
  failures, compile errors skip `go test`, stale cleanup, text output,
  E0001 mapping.
- `internal/build/compare_test.go` `TestGeneratedTestFileRuns`: the
  generated `_test.go` passes `go test` under the real toolchain.
- Goldens regenerated with `-update` (`internal/codegen/testdata`,
  `testdata/golden/`); the escape-arm `//line` re-anchoring diff was
  reviewed.

## Remaining notes

- `gom test` always runs the package's whole test binary — failures from
  sibling files affect the exit code. Intended granularity (see the
  decision).
- A test that panics is folded as a failed test with the panic trace in
  `output` (no terminal event arrived).
- Display generation is test-side only: `"${v}"` interpolation still
  falls back to `fmt.Sprint` for composites —
  [problems/2026-11-04-interp-composite-display](../problems/2026-11-04-interp-composite-display.md)
  stays open; the `ShowDecl` machinery could be reused for a fix.
- `assert_eq` shows both values; a structured *diff* display is not
  implemented. `-parallel` / `-run` pass-throughs are not implemented.
