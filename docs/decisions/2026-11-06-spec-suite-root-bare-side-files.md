# A bare side file at the spec-suite root is a load error

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-spec-suite-root-bare-side-files-verification.md)）
- 日付: 2026-11-06

## Context

[gomtest-multifile-cases](2026-11-05-gomtest-multifile-cases.md)
made a bare side file (`expected.json` / `golden.go` / `case.json`)
the marker that turns a *subdirectory* of a suite into a directory
case. The same bare name directly under the suite root was neither
a marker nor an orphan and dropped silently: `dirCase` never runs
on `suiteDir` itself, and the bare name is one character too short
for the `<name>.expected.json`-style side-file suffix check, so it
never reached `sideFiles` either.

## Options

- **(i) Report it as a load error** — the same channel malformed
  directory groups use: a synthetic `testCase` whose `loadErr`
  fails the case at `runCases`, leaving the rest of the suite
  running.
- **(ii) Document the ignore.** The file is harmless; a stray file
  just does nothing.
- **(iii) Treat the root as a directory case** when it holds a bare
  side file — the suite's whole member list would compile as one
  unit.

## Decision

**Option (i): report it.** A stray `expected.json` at the suite
root almost always means a misplaced fixture — either a directory
case whose members are missing, or a `x.expected.json` that lost
its `x.` prefix. Silently accepting it hides the mistake exactly
where a fixture convention is being violated, which is the
posture the malformed-group rules (named side file inside a case
dir, subdirectory inside a case dir, empty group) already take.

Option (ii) was rejected: the ignore serves no use case — there is
no layout in which a root-level bare side file is meaningful.
Option (iii) was rejected: the root contains every case, not one;
reading a marker there would silently change the unit boundaries
of the entire suite, and a misplaced file would become a
whole-suite semantics change instead of an error.

Details:

- The check lives in `discoverCases`'s file walk: a file directly
  under `suiteDir` whose name equals a bare side-file name becomes
  a `testCase` with `loadErr` set — the error names the file and
  explains that the bare name marks a directory case inside a
  subdirectory while a single-file case's side file is prefixed.
- Ordering is unchanged: the `<name>`-suffixed check runs first,
  so `x.expected.json` at root keeps its orphan path.

## Consequences

- `testdata/spec/expected.json` (and `golden.go` / `case.json`)
  now fails the suite with a `gomtest:` load error naming the
  file, instead of passing unnoticed.
- `internal/gomtest/testdata/rootside/` pins the three bare names
  next to a working single-file case and a working subdirectory
  case (`TestDiscoverRootSideFile`).
- `gomtest.go`'s `discoverCases` comment and `doc.go` state the
  rule; design.md's testdata layout note mentions it.
- No language-facing change: this is a harness convention, so
  spec.md is untouched (same level as the directory-case
  decision).
