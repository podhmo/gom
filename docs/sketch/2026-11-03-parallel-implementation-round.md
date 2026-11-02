# Parallel implementation and executable tutorial verification

- 日付: 2026-11-03
- 状態: 完了（この実装ラウンド。コンパイラ全体には未完了タスクあり）

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

## Reviewed integration milestones

Each implementation used its own managed worktree, a fresh reviewer,
and an explicit follow-up decision by the original implementer.

| Work | Integration | Review result |
| --- | --- | --- |
| P3-1 import session audit / error aliases | `daf5ee4` | No required change; original owner recorded the decision |
| P2-2a type-variable scope | `d97d075` | No required change; normal polymorphism and eager timing verified |
| P2-2b static function aliases | `c5a0c4a` | No required change; aggregate cycles remain P2-2c |
| P2-4a Result pipelines | `9888c3a` | Numeric inflow conversion finding fixed; related void inflow regression also fixed |
| Gist CLI use case | `6e55872` | No required change; real generated binary tested only against local fake servers |
| Explicit void Go calls | `4300eb9` | No required change; explicit Core result shape and Unit normalization verified |
| Twelve executable tutorials / cache dependencies | `5e792f4` | Runtime dependency finding fixed; compiler, CLI, fixture and runtime corruption probes detect failures |
| P3-2a interface assignment / exported fields | `414d451` | All three findings fixed; re-review found no further required change |

The Gist CLI was added at the user's request during this round. It
creates one secret Gist from all supplied UTF-8 files, or updates the
Gist identified by `--gist URL`, and prints the response URL. Fake
servers cover POST/PATCH, preserved remote files, payloads, headers,
input failures, HTTP/JSON failures and redirect rejection. No real
GitHub API was called. Its small host adapter exposes the interop
operations the compiler does not yet support. HTTP interface/field
support removed four adapter functions; remaining byte/container/JSON
operations stay explicit in its README and TODO.

The executable tutorial iteration added two chapters and reproduced
a stale test-cache success after compiler changes. Fresh review then
found the same hole for generated-code runtime dependencies. The fix
tracks actual Go module inputs, and corruption/new-source probes verify
cache invalidation. All twelve chapters and the dependency fix are
integrated.

HTTP boundary review found three additional cases to fix: preserving
the dynamic numeric type when boxing into an interface, channel
assignability, and field types that become known after the receiver is
inferred. Each was repaired with regressions, and re-review verified the
fixes. The field-read decision followed draft/prototype/verification
before adoption; the spec was not silently changed to match a defect.

## Persistent follow-up

The user requested a durable, low-effort place for discrepancies.
[implementation-gaps.md](../implementation-gaps.md) records short
descriptions and source links, including documentation-only drift.
AGENTS.md requires registration on discovery and a checked entry with
verification when resolved. It does not require deciding priority,
cause or the correct side before making a note. TODO still owns work;
decisions, problems and sketch retain their existing roles.

Remaining implementation items stay explicit in TODO.md: aggregate
function-value initialization cycles, fusion of pipelines containing
explicit `try`, further Go interop (including byte and slice/List
boundaries), and the independently reproduced inferred numeric binder
problem. This round does not mark the whole compiler complete.

## Go.Any numeric probe

HTTP boundary review repairs boxing into named interfaces, while the
existing Go.Any path deliberately remains `ConvBoxAny`. A separate
probe on integrated main `5e792f4` confirmed a related existing gap:

```gom
package main
import go "fmt"
fn main () -> Unit = { ignore (fmt.Println (fmt.Sprintf ("%T", 1))); () }
```

Building the current `cmd/gom`, running `gom build` on the temporary
source, then `go run` on its output succeeds and prints `int`. Gom's
`Int` representation is `int64` (spec 8.3). This is recorded separately
in TODO and the persistent gap ledger so the named-interface repair
does not claim to fix the older Go.Any boxing path. No remote API is
involved in this probe.

## Final integrated verification

The integrated compiler, Gist CLI and tutorial pass:

- `make check`: formatting, vet, race-enabled root tests, sumtype
  checking (including the intentional negative canary), staticcheck,
  and nested experiment vet/race tests.
- `tutorial/run.sh`: all twelve chapters, including the expected
  E0301 diagnostic in chapter 10.
- `examples/gist-cli/build.sh /private/tmp/gom-gist-cli-final` and
  the resulting binary's `--help`.
- `git diff --check`: clean.

The first final staticcheck pass flagged a tutorial explanation that
began like a compiler directive (`go:embed`). Rephrasing that comment
resolved SA9009; the entire `make check` was then rerun successfully.
All Gist request tests use local fake servers and dummy tokens.

The gap ledger preserves resolved entries and the known remaining
differences, including Go.Any boxing, Float inference/emission and
slice/List boundaries. The actionable remainder is kept in TODO.md;
this report records one completed implementation/review/tutorial cycle.
