# Verification: spec-suite root bare side files are load errors

Companion to
[decisions/2026-11-06-spec-suite-root-bare-side-files.md](../decisions/2026-11-06-spec-suite-root-bare-side-files.md).
Wave 10 lane 2 item (c).

## The gap

`discoverCases` walks `suiteDir` with `filepath.WalkDir`:

- For a *subdirectory* it calls `dirCase`, which treats a bare
  `expected.json` / `golden.go` / `case.json` inside the directory
  as the directory-case marker.
- For a *file* it either takes `*.gom` as a single-file case or
  collects `*.{expected,golden,case}.{json,go}`-suffixed names as
  side files (reported as orphans when their `.gom` is missing).

A bare side name directly under the suite root escaped both paths:

1. `dirCase` is only invoked on `path != suiteDir`, so the root's
   own contents never see the marker check — the root is not a
   case directory.
2. `strings.HasSuffix("expected.json", ".expected.json")` is
   false: the bare name is exactly the suffix minus its leading
   dot, one character too short to be a side file.

Result: `testdata/spec/expected.json` was silently ignored —
neither marker, nor case, nor orphan, nor error.

## The fix (option i — report)

`discoverCases`'s file handler now checks, after the suffixed
side-file check, whether a file directly under `suiteDir` has a
bare side-file name. When it does, discovery appends a `testCase`
with `loadErr`:

    gomtest: bare side file expected.json at the suite root: the
    bare name marks a directory case inside a subdirectory; a file
    case's side file is prefixed (<name>.expected.json)

`runCases` reports it through the same `t.Fatal(c.loadErr)` path a
malformed directory group takes — one failing subtest, the rest of
the suite still runs. The `loadErr`-first flow means the case
never reaches `loadManifest`/`sidePath` (which would index a
nonexistent `files[0]`).

A bare name cannot reach the handler from deeper directories:
inside a subdirectory the name is already the marker that makes
the directory a case (`fs.SkipDir` skips its contents), and inside
a namespace directory a marker turns it into a case for the same
reason. The `filepath.Dir(path) == suiteDir` guard keeps that
invariant explicit.

## Fixture

`internal/gomtest/testdata/rootside/`:

    case.json          — bare manifest name (broken case)
    expected.json      — bare expected name (broken case)
    golden.go          — bare golden name (broken case; holds a
                         valid package clause so `gofmt -l .`
                         stays clean)
    ok.gom             — a working single-file case
    ok.expected.json   — its prefixed side file (not an orphan)
    sub/case.json      — a real directory-case marker
    sub/a.gom          — the group's member

`TestDiscoverRootSideFile` (`internal/gomtest/gomtest_test.go`)
pins: five discovered cases, the three bare names each carrying
the bare-side-file `loadErr`, `ok` clean, `sub` a working
directory case, no orphans.

## Verification output

- `go test ./internal/gomtest/ -run TestDiscover -v`:
  all six discovery tests pass, including
  `TestDiscoverRootSideFile`.
- `go test ./internal/gomtest/`, `go vet ./internal/gomtest/`,
  `gofmt -l internal/gomtest/`: clean.
- `go test ./...` (whole repo): run at the end of the lane; see
  the lane report.

## Notes

- `rootside/golden.go` must stay a syntactically valid Go file:
  `gofmt -l .` walks `testdata/` and a parse-erroring `.go` file
  there would fail the check — it is data to the harness, not to
  the Go toolchain (which skips `testdata/` anyway).
- `RunGoldenTests` rejects directory cases at run time, but the
  root bare-name check is suite-agnostic — it lives in shared
  `discoverCases`, so a stray root `golden.go` reports identically
  in both suites.
