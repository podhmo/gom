# Parallel implementation and executable tutorial verification

- 日付: 2026-11-03
- 状態: 進行中

## Starting point

The round starts at `89655ba` on `main`, with a clean working tree.
Go is `go1.27.3 darwin/arm64`. The AST, compiler phases, Go import
loader, evaluator, and ten tutorial chapters already exist; this round
continues the actionable work in `TODO.md` rather than recreating them.

Baseline verification:

- `gofmt -l .`: empty output.
- `go vet ./...`: passed.
- `go test ./...`: passed.
- `tutorial/run.sh`: all ten chapters passed, including the expected
  E0301 diagnostic in chapter 10. The Go command printed a module stat
  cache write permission message, but the driver completed successfully.

## Work and review protocol

Independent implementation agents own separate worktrees for P2-2
(polymorphic functions, partial application, closures and the known
top-level binding bug), P2-4 (`try` and Result pipelines), and P3-1
(automatic Go type import). Each reads the TODO upkeep skill and runs
formatting, vet, and the full test suite before reporting completion.

A fresh reviewer examines each completed branch. The original implementer
then decides whether each finding requires a change and records the
reason or adds a regression test and fix. Only reviewed changes are
squash-merged into the integration checkout. Newly discovered decisions
follow draft → prototype → recorded verification → adopted/rejected.

After integration, a fresh agent runs and extends the executable tutorial,
records reproducible compiler defects in TODO.md, and hands them back for
implementation and fresh review. Unresolved concerns belong in
`docs/problems/`; distant work belongs in `docs/open-questions.md`.

## Initial findings

- P2-4: spec 9.2 describes `|?>` as a Result-valued expression. Existing
  lowering escapes from the enclosing function instead, while eval uses
  the Result-valued interpretation. The implementation must follow the
  spec and test continuation outside a failed pipeline.
- P3-1: TODO/design still mention `go/packages`, but the adopted
  [loading decision](../decisions/2026-11-03-import-go-loading-mechanics.md)
  deliberately uses `go list -export` and one shared `go/importer`
  session. Audit observable requirements before replacing that mechanism.
- The tutorial README still says `ignore` is unimplemented even though
  chapter 1 already uses it. Update the tutorial from executed evidence.
