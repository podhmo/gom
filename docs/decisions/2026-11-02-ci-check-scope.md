# Scope of CI checks and `make check` (B-2)

- 状態: 採用
- 日付: 2026-11-02
- 検証: [ci-check-verification](../sketch/2026-11-02-ci-check-verification.md)

## Background

B-2 adds GitHub Actions CI (`.github/workflows/ci.yml`) and a local
`make check` running the same checks (AGENTS.md: `gofmt -l .` empty,
`go vet ./...`, `go test ./...` with the race detector). The one open
choice was whether the nested Go modules under `experiments/` —
invisible to `./...` in the root module — get checked too.

## Decisions

- The check set is exactly `gofmt -l .` must be empty, `go vet ./...`,
  `go test -race ./...`. No lint mega-setup, no coverage upload —
  B-2's stated scope.
- `gofmt -l .` runs once at the repository root. gofmt is
  module-agnostic, so this covers the root module and every nested
  `experiments/` module in a single pass. gofmt's own failure (e.g. an
  unparseable file, which it reports on stderr without listing) also
  fails the check.
- `go vet` / `go test -race` run per module: the root module plus each
  `experiments/**/go.mod` found by `find`. Experiments are
  verification code rather than shipped artifacts, but they feed P0
  decisions and would silently rot without CI; the loop is cheap while
  experiments stay small.
- `make check` mirrors the workflow step-for-step so a green local run
  predicts a green CI run. It uses `go`/`gofmt` from `PATH`
  (overridable via `GO`/`GOFMT`); Go >= 1.27 is required and
  `GOTOOLCHAIN=auto` (the default) fetches the toolchain `go.mod`
  names when the installed one is older.
- CI pins Go via `go-version-file: go.mod`, so the toolchain version
  moves with `go.mod` instead of drifting in the YAML.

## Consequences

- Adding a new experiment needs no CI or Makefile change as long as its
  `go.mod` sits under `experiments/`.
- If an experiment ever turns flaky or slow, the experiments step —
  not the root checks — is where to gate or exclude it.
