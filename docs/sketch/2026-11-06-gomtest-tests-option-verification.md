# Sketch: `case.json` `tests` option — verification log

Verification for
[decision: the tests option](../decisions/2026-11-06-gomtest-tests-option.md):
`case.json` `{"tests": true}` puts a golden case's compile into
`gom test` mode (`lower.Env.Tests`), so members emit their
`_test.go` halves as ordinary generated outputs pinned by
`*_test.golden.go` files — the fixture surface that lets the
sibling `TestX_<tag>` naming rule
([test-name-file-tag](../decisions/2026-11-05-test-name-file-tag.md))
be pinned by testdata rather than only by the driver test.

## Mechanics, as implemented

- `loadManifest` parses `"tests"` next to `"skip"`; a non-boolean
  value is a manifest error (`"tests": "yes"` fails like a
  non-string `skip`).
- `RunGoldenTests` hands the flag to the compile function as
  `CaseOptions{Tests: c.tests}` — the harness's pipeline-agnostic
  option channel; `internal/build`'s `goldenGenerate` maps it to
  `build.Options.Lower.Tests`.
- A member's `Output.TestSource` becomes a `GeneratedFile` named
  `<member>_test.go`, which the golden runner maps to
  `<member>_test.golden.go` — the per-output rule of
  [golden dir cases](2026-11-06-gomtest-golden-dir-cases.md) with no
  additional format machinery.
- `DiagnosticsFunc` untouched: diagnostics do not depend on test
  mode, so the spec suite needs no options.

## Verification

- **The collision rule as fixtures** —
  `testdata/golden/test_name_collision/` (`a.gom` `test "x"` +
  `test "only a"`, `b.gom` `test "x"`, `case.json`
  `{"tests": true}`): `go test ./internal/build -run TestGolden
  -update` emitted four member goldens. `a_test.golden.go` pins
  `func TestX_64f1a489(t *testing.T)` and `func
  TestOnlyA(t *testing.T)`; `b_test.golden.go` pins `func
  TestX_c83a3948(t *testing.T)` — the two collided names carry
  distinct deterministic fnv32a-of-suite-relative-path tags and the
  unshared name stays bare, matching
  `TestCompileFilesTestNameCollision`'s assertions textually. The
  main halves (`a.golden.go`/`b.golden.go`) hold just the package
  clause — test decls never leak into production output.
- **Tags are stable**: the tag hashes `File.Path`
  (`test_name_collision/a.gom`), a suite-relative path, so the
  pinned names are machine- and checkout-independent.
- **Plumbing pinned in gomtest**: `TestLoadManifest` covers the
  `"tests"` key (true, false, non-bool error, combined with skip);
  `TestRunGoldenTestsTestsOption` shows `CaseOptions.Tests`
  reaching the compile function for `testscase/` (`{"tests":
  true}`) but not `pkgcase/` (`{}`).
- **No flat-case drift**: `builtins.gom` — the only flat golden
  case with a `test` block — still pins only `builtins.golden.go`
  (its manifest sets no `tests`), so the default stays `gom build`
  output.
- `go test ./internal/gomtest ./internal/build` all pass; the two
  pre-existing flat+dump suites untouched.

## Observations / deferred

- A flat case with `"tests": true` produces `x.go` + `x_test.go`
  and fails the one-output flat check — the option is meaningful
  for directory cases; flat test-mode goldens would need a
  `x_test.golden.go` convention (a later extension if wanted).
- `TestCompileFilesTestNameCollision` stays: it asserts the
  `Output.Tests` mapping (name ↔ func ↔ span), which a golden file
  cannot see.
