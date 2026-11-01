# Verification report: test harness internal/gomtest (B-4)

- 日付: 2026-11-02
- 目的: B-4「テストハーネス」の実装が意図どおり動くことを、パイプラインが存在しない現時点で確認する
- 関連: [decision: test-harness](../decisions/2026-11-02-test-harness.md)（draft）、TODO B-4、design.md 3/7 章、spec.md 12 章

The `internal/gomtest` package and its unit tests
(internal/gomtest/*_test.go, fixtures under
internal/gomtest/testdata/) plus the suite wiring in
internal/build/harness_test.go are the prototype for the draft
decision.

## What was verified

- **Discovery**: recursive `*.gom` walk (`sub/beta` becomes a nested
  subtest), `_`/`.`-prefixed helpers ignored, orphan side files
  (`.expected.json`/`.golden.go`/`.case.json` without `.gom`)
  reported, malformed manifests surface as case load errors.
- **Matching semantics** (`spec_test.go` table tests over ~40 cases):
  scalar fields exact; absent fields unchecked; `span` resolves
  through `diag.LineIndex` on the real fixture source with per-key
  `line`/`col`/`offset` comparison (verified against hand-computed
  positions — caught a one-off in this report's first draft: the
  offset of `\n` is the *end* column of its line, not the start of the
  next); `data` subset after JSON normalization (`[]int` ≡ `[1,2]`,
  `map[string]int` ≡ `{"a":1}`); `notes`/`fixes` ordered subsequences;
  unreadable span files degrade gracefully — offsets still compare,
  line/col keys report a clear mismatch. Count mismatch dumps the
  actual diagnostics for debugging.
- **Skip**: `<name>.case.json` `{"skip": "..."}` → `t.Skip` with the
  reason visible under `go test -v`; skipped cases never invoke the
  compile function.
- **-update**: spec side renders actual diagnostics to the spec §12
  wire shape (kind/code/severity/message/span/data/notes/fixes in spec
  field order, resolved line/col/offset) and the written file compares
  clean on a second pass; golden side writes the file (always with a
  single trailing newline so it stays gofmt-clean).
- **Golden**: literal match after CRLF→LF + trailing-newline
  normalization; mismatch prints a unified-style diff with context.
- **Wiring**: `go test ./...` runs `TestSpec`/`TestGolden` in
  internal/build over the real `testdata/` suites — all three seed
  cases skip with reasons and the suites pass.
- **Repo checks**: `gofmt -l .` empty (note this also verifies
  `testdata/golden/*.golden.go` is gofmt-clean, a free canonicalness
  check), `go vet ./...`, `go test ./...`, and `make check`
  (experiments modules included) all pass on Go 1.27.1.

## Not covered yet

- Case-level failure paths (`t.Fatal` on compile error / missing
  expected file) cannot be observed from a parent `*testing.T`; they
  are thin glue over the tested functions.
- The real end-to-end run exists only as the stub in internal/build;
  phases register real compile functions as they land.

## Result

The harness works before the pipeline exists. The decision can move
from draft to 採用 after review; adopting it only requires the
design.md section 3 additions noted there (no spec.md impact).
