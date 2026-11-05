# Sketch: golden directory cases — verification log

Verification for
[decision: golden directory cases](../decisions/2026-11-06-gomtest-golden-dir-cases.md):
a directory case in the golden suite pins one `.golden.go` per
generated output file inside the case directory — member `a.gom`'s
`a.go` → `a.golden.go`, its test half `a_test.go` →
`a_test.golden.go` — the flat `<name>.golden.go` convention applied
per output. `GenerateFunc` widened to
`func(fs []File) ([]GeneratedFile, error)`, mirroring
`DiagnosticsFunc`.

## Why per-output goldens and not the concatenated `golden.go`

`gofmt -l .` in CI (`make fmt-check`) parses every `.go` file in the
repo, `testdata/` included, and exits non-zero on parse errors. A
`<case>/golden.go` holding N member outputs has N `package`
clauses — unparseable, breaking the check the `.golden.go` suffix
was chosen to provide ([test-harness](../decisions/2026-11-02-test-harness.md):
golden files stay gofmt-clean and editor-highlighted). A
`golden.txt` sidesteps gofmt but abandons that invariant and leaves
`golden.go` as a marker naming nothing. Per-member files keep every
invariant at the cost of relaxing `dirCase`'s named-side-file error
for `*.golden.go` only.

## Fixture format, as implemented

- `testdata/golden/<case>/` holds member `*.gom` files, a bare
  `case.json` marker (`{}` suffices — the file also carries per-case
  options), and one `<member>.golden.go` per generated output.
- `RunGoldenTests` maps each `GeneratedFile{Path}` to its golden by
  substituting `.golden.go` for `.go`: flat `x.go` → `x.golden.go`
  (identical to the old `sidePath` behavior), dir
  `pkgcase/a.go` → `pkgcase/a.golden.go`. A flat case must return
  exactly `<name>.go` as its single output — a misnamed `Path`
  fails instead of silently shifting the golden — and a dir case's
  outputs must sit directly inside the case dir.
- Member↔golden correspondence is strict both ways: an output with
  no golden is the existing missing-golden error (`-update` writes
  it), and a `*.golden.go` in the case dir that no output claimed
  is an error (`reportStrayGoldens` — the walk-level orphan check
  never descends into a case dir, so the runner checks itself).
- `dirCase` no longer load-errors member-named `*.golden.go` files;
  named `expected.json`/`case.json` inside a case dir still error.

## Verification

- **Flat cases unchanged**: `go test ./internal/build -run
  TestGolden` passes all 45 pre-existing flat goldens with zero
  diffs — the single-output degenerate path lands byte-identical
  goldens. `go test ./internal/build -run TestSpec` unchanged too
  (the spec suite does not consume `GenerateFunc`).
- **Real dir case** `testdata/golden/sibling_decls/` (`a.gom`
  `let base = 41`, `b.gom` `fn use () -> Int = base + 1`,
  `case.json` `{}`): `go test -update` wrote `a.golden.go`
  (`var base int64 = 41`) and `b.golden.go` (`func use` returning
  `base + 1`) — `b`'s output compiles only because the members
  shared one package scope, so the fixture proves the unit path
  end to end, and both goldens stay gofmt-clean `.go` files.
- **Runner mechanics** (`internal/gomtest`): a `z.golden.go` dropped
  into `goldenpkg/pkgcase/` errors "golden file z.golden.go in
  directory case pkgcase has no generated output"; the dir case
  reaches `generate` as one `[a.gom b.gom]` call
  (`TestRunGoldenTestsDirCaseSmoke`); `-update` creates member
  goldens inside a fresh case dir (`TestGoldenUpdateDirCase`).
- **`dirCase` relaxation**: `pkgcase/{a,b}.golden.go` load as member
  goldens (the case runs rather than fatalling on "named side
  file"); `brokendircase/named` still errors on `a.expected.json`.

## The cases added

| case | pins |
| --- | --- |
| `testdata/golden/sibling_decls/` | first real golden dir case: per-member goldens + cross-file package scope (`b.gom` reads `a.gom`'s `let`) |
| `internal/gomtest/testdata/goldenpkg/pkgcase/` | discovery + runner smoke: `case.json` marker, per-member comparison, stray-golden reporting |

## Observations / deferred

- A member `a_test.gom` and a member `a.gom`'s test half both map to
  `a_test.golden.go` — two outputs claiming one golden fails the
  comparison deterministically rather than silently colliding; no
  dedicated error yet.
- A flat case producing more than `x.go` (e.g. test mode on a flat
  case) is a run error; a flat `x_test.golden.go` convention is a
  possible later extension.
- `DiagnosticsFunc` keeps `func([]File)` without options — see the
  `tests`-option decision for why the spec suite does not need it.
