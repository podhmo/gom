# Exhaustiveness checker for sealed interfaces (B-3)

- 状態: 採用
- 日付: 2026-11-02
- 検証: [sumtype-check-selection](../sketch/2026-11-02-sumtype-check-selection.md)

## Background

design.md section 2.4 represents internal ASTs as sealed interface +
type switch and plans a go-sumtype-style tool to keep the switches
exhaustive. [reference-verification](../sketch/2026-11-02-reference-verification.md)
nominated `alecthomas/go-check-sumtype`; B-3 had to verify it on Go
1.27, decide how it is pinned, and prove a missing variant breaks CI.

## Decisions

- Use `github.com/alecthomas/go-check-sumtype` (v0.5.0 pinned),
  invoked as `go run <pkg>@v0.5.0 -default-signifies-exhaustive=false`
  so go.mod stays dependency-free. One shared entry point:
  `scripts/check-sumtype.sh`, called by `make check-sumtype` and the
  CI step.
- Sealed interfaces get `//sumtype:decl` on the type declaration.
  Generic sealed interfaces are still annotated for documentation,
  but the check is vacuous on them (the tool excludes generic named
  types from the variant set — verified, not just documented).
- Every exhaustive type switch keeps a `default` whose body is a
  single `panic(...)` statement — the existing AGENTS.md convention —
  and additionally `-default-signifies-exhaustive=false` stops a
  non-panicking `default` from ever silencing the check.
- The "a new variant breaks CI" guarantee is a canary module,
  `testdata/sumtype/`: `nonexhaustive/` deliberately misses a variant
  and `check-sumtype.sh` asserts the tool reports
  `missing cases for Shape_Rect`. `exhaustive/` is the positive
  control.

## Consequences / open items

- When P1-2a (`task/p1-2a`) merges, annotate every sealed interface in
  `internal/syntax`, `internal/check` and `internal/types` with
  `//sumtype:decl`; until then the check covers the experiment
  packages only (plus the canary). Two partial probes in
  `internal/syntax/expr_test.go` (`TestSelectArmsAndStrPartsAreSealed`)
  must be rewritten as type assertions at that point — the fork checks
  `_test.go` and v0.5.0 has no per-switch opt-out (verified in the
  sketch report).
- If a generic sealed interface is introduced that needs real
  coverage, revisit: both candidate tools skip generic variants; the
  fallback is a small custom `go/analysis` analyzer (see
  reference-verification "Other approaches").
- `go vet -vettool` and golangci-lint `gochecksumtype` remain
  equivalent alternatives if the runner story ever changes.
