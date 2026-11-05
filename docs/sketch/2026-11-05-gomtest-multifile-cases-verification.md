# Sketch: gomtest multi-file (directory) cases — verification log

Verification for
[decision: multi-file spec cases](../decisions/2026-11-05-gomtest-multifile-cases.md):
a suite subdirectory holding a bare side file (`expected.json`,
`golden.go`, `case.json`) is one case over its member `*.gom`/`*.gomi`
files, compiled as a package unit; `DiagnosticsFunc` takes the whole
member list (`func(fs []File)`), so `internal/build`'s suite wiring
feeds `build.CompileFiles` and a one-file case degenerates to
`build.Compile`.

## Fixture format, as implemented

- `testdata/spec/<case>/{a.gom, b.gom, expected.json}` — `expected.json`
  is simultaneously the group's expectation and the marker that makes
  the directory a case. Member spans name the suite-relative path
  (`<case>/a.gom`), so per-file positions pin verbatim through the
  existing `diag.DirResolver`.
- Marker set: `expected.json` / `golden.go` / `case.json` — a dir with
  only `case.json` still parses as a case (skip-only or `-update`-seeded).
- Members: non-ignored `*.gom`/`*.gomi` files directly in the dir,
  `os.ReadDir` order (filename order). `.gomi` members compile as
  interface files — `build.CompileFiles` semantics, no new behavior.
- Malformed groups are load errors, not silent drops: member-named
  side files (`a.expected.json` inside a case dir), nested
  directories, a marker with zero members, and a `x.gom`/`x/` name
  collision.
- Unmarked directories stay pure namespaces — `testdata/spec/eq_limit/`
  and `internal/gomtest/testdata/discover/sub/` keep working as flat
  case parents.

## Verification

- **Discovery unit tests** (`internal/gomtest`,
  `testdata/discoverpkg/`, `testdata/brokendircase/`):
  `TestDiscoverDirectoryCases` pins member set/order, `isDir`,
  `sidePath` → `<case>/expected.json`, and the `case.json` skip;
  `TestDiscoverDirectoryCasesBroken` pins the four load-error shapes;
  `TestRunSpecTestsDirCaseSmoke` shows the compile func receiving
  `[a.gom b.gom]` for the group and one-element slices for namespace
  cases, and skipped groups bypassing compile entirely.
- **Flat cases unchanged**: `go test ./internal/build -run TestSpec`
  passes all ~190 pre-existing fixtures through the new
  `CompileFiles` path with zero expected-file diffs, as does
  `internal/syntax`'s parse suite (per-member `Parse`/`ParseInterface`
  loop). `go test ./...` is clean end to end.
- **`-update` flow**: writing `{"diagnostics": []}` placeholders as
  markers and running `go test ./internal/build -run TestSpec -update`
  rendered the real expectations — the update path resolves through
  `sidePath` to the bare `expected.json`.

## The cases added

`testdata/spec/`, all `-update`-generated then inspected:

| case | pins |
| --- | --- |
| `E0907_init_cycle_siblings/` (`let x = y + 1` / `let y = x + 1`) | two E0907, one per member file's span — the `TestCompileFilesInitCycleAcrossSiblings` shape as a spec fixture |
| `E0904_tailrec_mutual_siblings/` (`@tailrec even` / `odd`) | E0904 at `a.gom`'s `even`, `data.through = "odd"`, `path = [even odd even]` — sibling mutual recursion |
| `E0202_duplicate_let_siblings/` | E0202 at `b.gom` with `first_defined_at` naming `a.gom` — shared package scope |
| `E0605_package_clause_siblings/` | E0605 at the *later* member `b.gom` — shows filename order is deterministic where it matters |
| `ok_sibling_let/` | empty diagnostics — `b.gom` reads `a.gom`'s `let base` through the shared scope |
| `ok_sibling_iface/` | `m.gomi` member compiles as an interface file in its own scope beside the sibling `.gom` unit; empty diagnostics |

## Observations / deferred

- Golden directory cases are rejected explicitly by `RunGoldenTests`
  (`t.Fatalf`) — a group emits one Go file per member, so a golden
  group needs a format of its own. Deferred until needed.
- Fuzz seeds glob `testdata/spec/*.gom` flat
  (`internal/build/fuzz_test.go`, `internal/format/fuzz_test.go`), so
  directory members never become seeds — same treatment `eq_limit/`
  already relies on; no seed-path change needed.
- `eq_limit/` (named side files only, no marker) correctly remains a
  namespace of one flat case — the marker rule distinguishes "a dir of
  cases" from "a case dir" without renaming anything existing.
- The internal driver tests (`files_test.go`) stay: the fixtures are
  additive spec-workflow coverage, not a replacement — the drivers
  also assert output withholding and emitted-name details
  expected.json cannot express (e.g. `TestX_<tag>` func names on
  `Output.Tests`).
