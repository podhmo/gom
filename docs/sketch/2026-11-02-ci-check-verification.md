# Verification report: CI checks and make check (B-2)

- 日付: 2026-11-02
- 対象: [decision: ci-check-scope](../decisions/2026-11-02-ci-check-scope.md)

`.github/workflows/ci.yml` and the `Makefile` are the artifact for the
decision. This report records what was exercised in the worktree
before marking it 採用.

## Happy path

- `make check` (Go 1.27.0 from PATH, `GOTOOLCHAIN=auto`): `gofmt -l .`
  empty, `go vet ./...` clean, `go test -race ./...` ok for the root
  module, and the experiments loop vetted + tested
  `experiments/p0/fn-repr` (3 packages, all ok).
- `.github/workflows/ci.yml` parses as YAML (`yaml.safe_load`) and runs
  the identical commands in the same order as `make check`.

## Failure paths exercised

- Parseable but unformatted file (`package  diag`): `gofmt -l .` lists
  it and `make fmt-check` fails.
- Unparseable file: gofmt reports on stderr without listing; the
  `|| exit 1` guard still fails the check (this would otherwise pass
  silently until vet/test caught it).
- Compile error introduced in `experiments/p0/fn-repr/arity`:
  `make check-experiments` fails with the vet error labeled by module
  directory.

## Notes

- `gofmt -l .` at the root really does descend into `experiments/` —
  confirmed by the unformatted-file test and by gofmt's documented
  walk (it skips only `.`/`_`-prefixed names and non-`.go` files).
- `go -C <dir>` (Go 1.20+) runs vet/test inside each experiments module
  without subshell `cd`, keeping the loop a plain `for` in both the
  Makefile and the workflow.
