# テストハーネスの配置とファイル規約 (internal/gomtest)

- 状態: 採用
- 日付: 2026-11-02
- 検証: [test-harness-verification](../sketch/2026-11-02-test-harness.md)

## Background

B-4 builds the harness that runs the file-based suites under
`testdata/` (`spec/`: `.gom` + expected-diagnostics JSON, `golden/`:
generated Go code) plus a `-update` flag and a skip mechanism
(design.md sections 3 and 7). Two things needed deciding: where the
harness lives, and the on-disk convention for cases, expectations and
skips. Neither changes user-visible language behavior, so this is a
design.md-level decision (spec.md untouched).

## Decisions

- **Dedicated package `internal/gomtest`**, not `internal/build`.
  `build` ties the pipeline together and will depend on every phase;
  tests of `syntax`/`check`/`pattern`/`codegen` also need the harness,
  so hosting it in `build` would force import cycles (e.g. a check
  test → build → check). `gomtest` depends only on `diag`. The
  end-to-end suite run lives in `internal/build/harness_test.go`,
  which injects the compile function — today a "not implemented" stub,
  later the real pipeline (P1-11).
- **Case layout**: suites scan recursively for `*.gom`; the subtest
  name is the relative path without extension (`/` nests subtests).
  Files/dirs starting with `_` or `.` are helpers, leaving room for
  multi-file fixtures.
- **Expected files**: spec cases pair with `<name>.expected.json`
  holding `{"diagnostics": [...]}` in the spec section 12 object
  shape; golden cases pair with `<name>.golden.go` (the `.go` suffix
  keeps editor highlighting and makes `gofmt -l` check that golden
  output stays canonical — generated code comes from go/printer so
  this is free).
- **Skip**: an optional `<name>.case.json` manifest, today a single
  key `{"skip": "<reason>"}` reported via `t.Skip`. Expectations stay
  in the tree while skipped, so cases land before features. Reserved
  for future per-case options.
- **Matching semantics**: only fields present in an expected object
  constrain the actual diagnostic. `severity`/`code`/`message`/
  `source` match exactly; `span` resolves through `diag.LineIndex` on
  the span's file under the suite dir and each present
  `line`/`col`/`offset` key must equal (so expected files can pin
  byte offsets only, line/col only, or both); `data` is a subset match
  after JSON normalization; `notes` and `fixes` are ordered
  subsequences. The expected list itself must match the actual count
  exactly — a spec test asserts the complete diagnostic set. Actual
  diagnostics are sorted with `diag.List.Sort` first, so expectations
  are authored in output order.
- **Golden comparison**: literal string equality after CRLF→LF
  normalization and ignoring trailing-newline count. go/printer output
  is already canonical; fuzzier matching would hide real diffs.
- **`-update`** rewrites both `*.expected.json` (rendered to the wire
  shape so the file is a faithful spec-§12 object) and `*.golden.go`,
  creating missing files; skipped cases are never touched. Standard
  golden-update caveat applies: review diffs before committing.

## Consequences

- `gomtest` carries a small wire-rendering subset (`wireDiagnostic`
  etc.) for `-update`. P1-10 owns the real output encoding; when it
  lands, the two should be deduplicated (the harness could call the
  real renderer).
- Phases plug in by passing a compile function:
  `gomtest.RunSpecTests(t, "", func(f gomtest.File) ([]diag.Diagnostic, error))`.
  `File.Path` is the suite-relative path compilers should use as
  `Span.File` so positions line up with expectations.
- Seed cases (`testdata/spec/E0301_nonexhaustive_match.*`,
  `ok_shapes.*`, `testdata/golden/hello.*`) demonstrate the layout,
  all skip-marked.
- Needs design.md section 3 updates once adopted: `gomtest/` row in
  the `internal/` tree and the case conventions under `testdata/`.
