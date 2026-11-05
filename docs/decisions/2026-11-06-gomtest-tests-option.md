# The `case.json` `tests` option: test-mode generation in golden cases

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-gomtest-tests-option-verification.md)）
- 日付: 2026-11-06

## Background

[test-name-file-tag](2026-11-05-test-name-file-tag.md) gives
sibling files declaring the same `test "name"` distinct emitted
functions (`TestX_<fnv32a-of-path>`), and
[gomtest-multifile-cases](2026-11-05-gomtest-multifile-cases.md)
made package-unit spec fixtures possible — but that rule still
cannot be pinned by a fixture: emitted function names are not
diagnostics, so `expected.json` cannot express them. The only
coverage is the driver test
`internal/build.TestCompileFilesTestNameCollision`, invisible to
the spec-fixture workflow (`-update` regeneration, per-rule
fixtures, review-by-diff). The verification sketch lists the gap
explicitly.

Pinning needs two things: a way to compile a fixture *in test
mode* (`lower.Env{Tests: true}` keeps `test`/`@test` declarations
in the pipeline so codegen emits the `_test.go` half), and an
expectation surface for the emitted names. This decision picks
both. Harness convention — spec.md untouched.

## Options

- **(i) A new side-file kind** (`expected-funcs.json`) listing the
  emitted `TestX` names. Needs a new extension in the marker set,
  `sidePath`, discovery, a matcher, and an `-update` path — a
  second wire format pinning one slice of information the
  generated `_test.go` already contains verbatim.
- **(ii) A func-name section in `expected.json`.** The spec
  suite's `DiagnosticsFunc` returns diagnostics only — no outputs
  — and compiles without `lower.Env.Tests`, so `test` decls are
  dropped before codegen. Asserting emitted names there means
  changing the diagnostics contract to carry outputs: the wrong
  suite for emission assertions.
- **(iii) Keep the driver test and document it.** The rule stays
  real but fixture-invisible — exactly the residual the wave-9
  round recorded as follow-up work.
- **(iv) `case.json` `{"tests": true}` + the golden files.** The
  manifest is already the per-case options channel ("reserved for
  per-case options", only `"skip"` defined). A `tests` key asks
  the golden case's compile function for test mode; the emitted
  `_test.go` output lands in the member's `*_test.golden.go` file
  ([golden dir cases](2026-11-06-gomtest-golden-dir-cases.md)),
  which pins `TestX_<tag>` — and the surrounding emission —
  verbatim through the existing golden compare/`-update`
  machinery.

## Decision

**Option (iv).** `case.json` gains a `tests` boolean: when true,
the golden runner hands the case to `GenerateFunc` with
`CaseOptions{Tests: true}`, and the compile function builds with
`lower.Env{Tests: true}` — `gom test` mode. Members' test halves
surface as ordinary generated outputs (`<case>/a_test.go`), pinned
by `a_test.golden.go` per the golden dir-case rule.

Details:

- **Manifest**: `{"tests": true}` — `loadManifest` parses it
  alongside `"skip"`; a non-boolean value is a manifest error like
  a non-string `skip`. The key doubles as the directory-case
  marker, so the collision fixture needs no other bookkeeping.
- **`GenerateFunc`**: `func(fs []File, opts CaseOptions)
  ([]GeneratedFile, error)` — `CaseOptions` is the harness's
  pipeline-agnostic name for the manifest options; the caller maps
  `opts.Tests` to `lower.Env.Tests`. Only the golden suite takes
  options today: diagnostics do not vary with test mode (test-mode
  changes lowering retention and codegen, not checking), so
  `DiagnosticsFunc` keeps its signature — a spec fixture needing
  test-mode lowering diagnostics can revisit.
- **Scope**: `tests` is meaningful for directory cases — sibling
  scope is where test-mode emission gets interesting (shared
  package namespace, file tags). A flat case with `tests: true`
  produces `x_test.go` alongside `x.go`, which the flat
  one-output check rejects first (`case x generated 2 files,
  want 1`) — a flat test-mode golden can be a later extension
  if one is ever wanted.
- **Driver test stays**: `TestCompileFilesTestNameCollision` keeps
  asserting what a golden cannot read — the `Output.Tests`
  mapping (test name ↔ func ↔ span) and tag distinctness. The
  fixture pins the emitted names textually; the two layers are
  additive.

## Rejected alternatives

- **(i) `expected-funcs.json`**: a new side-file kind and matcher
  to re-pin, structurally, what the golden text already pins
  verbatim — more machinery for less coverage (names only, no
  bodies or imports).
- **(ii) section in `expected.json`**: the spec suite cannot see
  outputs; making it carry them bends the diagnostics contract
  around one rule.
- **(iii) document the driver test**: keeps emitted-name rules
  outside the fixture workflow — the recorded residual stays
  residual.
- **Always-on test mode for the golden suite**: compiling every
  golden case with `Tests: true` would grow every test-bearing
  case's pinned surface (e.g. `builtins.gom`'s `test "eq"` gains a
  `_test.go` golden) and redefine the suite from `gom build` to
  `gom test` output — the `_test.go` half is deliberately codegen's
  own suite's concern. Opt-in keeps the default.

## Consequences

- `testdata/golden/test_name_collision/` pins the sibling-tag rule
  as fixtures: `a.gom` + `b.gom` both `test "x"` + `case.json`
  `{"tests": true}` → `a_test.golden.go` / `b_test.golden.go` show
  `TestX_<tag>` distinct and `TestOnlyA` bare, regenerable with
  `-update`.
- `loadManifest`/`testCase` carry the new key (additive, in
  `gomtest.go` — flagged as the parallel-lane overlap).
- Any later per-case option lands in `CaseOptions` the same way.
