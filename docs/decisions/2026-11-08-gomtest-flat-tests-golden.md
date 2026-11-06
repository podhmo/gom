# Flat golden cases pin their `tests` output to `<name>_test.golden.go`

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-08-gomtest-flat-tests-golden-verification.md)）
- 日付: 2026-11-08

## Background

[gomtest-tests-option](2026-11-06-gomtest-tests-option.md) let
`case.json` set `{"tests": true}` so a golden case compiles in
`gom test` mode and pins the emitted `_test.go` half — but only
for directory cases, where each member output `<case>/<base>_test.go`
maps to `<case>/<base>_test.golden.go`. For a flat case the harness
enforced exactly one generated file, so `tests: true` on `x.gom`
(which yields `x.go` plus `x_test.go`) failed the output-count
check. The earlier decision recorded the flat extension as later
work.

This decision picks the flat convention — harness convention;
spec.md untouched.

## Options

- **(i) `<name>_test.golden.go` next to `<name>.golden.go`.** The
  same output→golden rule directory cases already use: substitute
  `.golden.go` for `.go` in the generated path. `x.gom` +
  `{"tests": true}` yields `x.go` → `x.golden.go` and `x_test.go` →
  `x_test.golden.go`. No new filename shape; the mapping stays one
  mechanical rule for flat and directory cases.
- **(ii) A distinct suffix** (`x.test.golden.go`, `x_gotest.golden.go`).
  Separates the test half visually but invents a second suffix
  scheme, and discovery would pair `x_test.golden.go`-shaped files
  differently from their directory-case counterparts — one rule
  becomes two.

## Decision

(i). The flat output set under `tests: true` is `{x.go, x_test.go}`;
each output maps to its golden by the existing `.go`→`.golden.go`
rule, so the expectations are `x.golden.go` and `x_test.golden.go`
next to `x.gom`.

Consequences inside the harness:

- A flat case may generate up to two outputs (its `.go` plus, under
  `tests`, its `_test.go`); more remain directory-case territory
  and fail.
- A `_test.go` output without `tests: true` in the manifest is a
  generator error, not a golden name.
- Discovery pairs a flat `x_test.golden.go` with `x.gom` only when
  the case's manifest sets `tests: true`; otherwise it reports as
  an orphan side file like any other unmatched expectation.
- An `x_test.golden.go` left behind after the test half disappears
  from the generated set (e.g. the case's `test` decls are removed
  while `tests` stays set) is a stale golden, flagged like a
  directory case's stray golden. Dropping `tests` from the manifest
  instead reports the file as a suite-level orphan, since the
  pairing exemption no longer applies.
- `-update` writes both flat goldens, matching the directory-case
  behavior.

## Consequences

- `testdata/golden/test_flat.gom` + `test_flat.case.json` pin the
  convention against real generation; `internal/gomtest` unit tests
  pin the mapping, pairing, and update behavior.
- The `gomtest-tests-option` decision's "later extension" sentence
  is discharged; this file records what the extension picked. It is
  not a supersession — the earlier decision stays correct about the
  directory convention.
