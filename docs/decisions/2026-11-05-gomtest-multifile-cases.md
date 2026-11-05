# Multi-file spec cases: directory cases in internal/gomtest

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-gomtest-multifile-cases-verification.md)）
- 日付: 2026-11-05
- Supersedes: [test-harness](2026-11-02-test-harness.md) のうち `DiagnosticsFunc` のシグネチャ（`func(f File)` → `func(fs []File)`、1 case = 1 コンパイルユニット）

## Background

`testdata/spec` fixtures are 1 case = 1 `.gom` file, so rules that
only exist at package granularity cannot be expressed as spec cases:
cross-file initialization cycles (`lower.CheckInitCycles` over the
unit's Core files), sibling `test` name tagging
([test-name-file-tag](2026-11-05-test-name-file-tag.md)), shared
package scope
([package-compilation-unit](2026-11-05-package-compilation-unit.md)).
These rules are covered today only by `internal/build` driver tests
(`TestCompileFilesInitCycleAcrossSiblings` and friends), which the
spec-fixture workflow (`-update`, expected-JSON pinning, per-rule
fixtures) cannot see or regenerate.

This decision fixes the on-disk format of a multi-file spec case and
how `internal/gomtest` discovers it. It is a harness convention, so
it is a design.md-level decision; spec.md is untouched (no
user-visible language behavior changes).

## Options

- **(i) A directory per case.** `testdata/spec/<case>/` holds the
  member `*.gom` files and one `expected.json`. A marker is needed to
  tell a case directory apart from the namespace directories already
  in use (`testdata/spec/eq_limit/` keeps a slow case out of the fuzz
  seed glob; `internal/gomtest/testdata/discover/sub/` nests a case).
- **(ii) A naming convention on flat files** —
  `E0907_x__a.gom` + `E0907_x__b.gom` grouped by the `__` separator,
  `E0907_x.expected.json` on the group prefix.
- **(iii) A manifest** listing a case's member files explicitly
  (e.g. `x.case.json` carrying `"files": [...]`).

## Decision

**Option (i), with the bare side files as the marker.** A directory
inside a suite is a *directory case* if and only if it directly
contains at least one bare side file — `expected.json`, `golden.go`,
or `case.json` (the side-file names minus their `<name>` prefix).
Details:

- **Members**: every non-ignored `*.gom` / `*.gomi` file directly in
  the directory, in filename order (`os.ReadDir` order — no manifest
  needed for ordering; Go package semantics are order-insensitive and
  diagnostics sort anyway). `.gomi` members compile as interface
  files, which is what `build.CompileFiles` already does with a mixed
  input batch.
- **Case name**: the directory's suite-relative path — the subtest
  name and the `Span.File` prefix of member diagnostics
  (`<case>/a.gom`), so expected files pin per-file positions.
- **Side files**: the group's expectation is the bare
  `expected.json`; skip lives in the bare `case.json`. Member-named
  side files (`a.expected.json`) inside a directory case are a
  load error — a group has exactly one expectation. Subdirectories
  inside a case directory are a load error; a case dir is atomic.
- **No marker, no case**: without a bare side file a directory stays
  a pure namespace and its `*.gom` files remain individual cases —
  `eq_limit/` and `sub/` keep working unchanged, and single-file
  cases are completely untouched.
- **Marker choice**: `expected.json` is the natural marker because a
  spec group needs it anyway — most directory cases carry no extra
  bookkeeping file at all. `case.json` alone also marks (a skip-only
  or `-update`-seeded group still parses as a case), and `golden.go`
  marks for the golden suite's symmetry — though golden directory
  cases are rejected at run time for now (see Consequences).
- **Duplicate names**: `x.gom` and a directory case `x/` produce the
  same case name; the later one in walk order is a load error.

**Harness API**: `DiagnosticsFunc` becomes
`func(fs []File) ([]diag.Diagnostic, error)` — a case *is* a
compilation unit, so the compile function takes the whole member
list. Single-file cases pass a one-element slice; `internal/build`'s
suite wiring calls `build.CompileFiles`, which degenerates to
`Compile` for a one-file unit, and `internal/syntax`'s parse suite
loops the members.

## Rejected alternatives

- **(ii) flat `case__part.gom`**: adds a name-parsing pass and a new
  ambiguity (`x.gom` vs `x__a.gom` both claiming case `x`), leaks the
  grouping into member names (`Span.File` would read
  `E0907_x__a.gom`), and the directory layout it replaces already
  works — the ignored-name convention (`_`/`.`) was reserved exactly
  for this.
- **(iii) manifest-listed members**: an explicit `files` list buys
  member ordering nothing (compile semantics are order-free) and adds
  a second place that can disagree with the directory contents
  (listed-but-missing, present-but-unlisted). Sorted directory
  contents are deterministic and cannot drift.

## Consequences

- `testdata/spec/<case>/{a.gom,b.gom,expected.json}` pins
  package-unit rules (E0907 across siblings, sibling-scope E0202,
  E0904 mutual `@tailrec`) in the same wire format as flat cases;
  `-update` regenerates them.
- The `internal/build` driver tests stay — the spec fixtures are
  additive coverage of the same rules.
- Golden directory cases are not supported yet: a group's output is
  one Go file *per member*, so a golden group needs a per-member or
  concatenated golden format of its own. `RunGoldenTests` fails a
  directory case explicitly until that is decided.
- doc.go gains the directory-case rules; design.md's `testdata/`
  notes mention them.
