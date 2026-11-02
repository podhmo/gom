# Executable tutorial iteration and cache verification

- 日付: 2026-11-03
- Base: `9888c3a`
- Go: `go1.27.3 darwin/arm64`

## Tutorial execution

The initial `tutorial/run.sh` passed all ten existing chapters. All remaining
`let _ = e` discards in chapters 2–9 were changed to `ignore e`; this includes
the back-pipe example, whose entire expression remains parenthesized.

Two executable chapters were added, with exact stdout fixtures:

- `11-polymorphic-functions`: one top-level lambda used at String and Int,
  a monomorphic alias of a polymorphic declaration, partial application,
  a captured closure, and a recursive function referenced through aliases.
- `12-result-values`: a non-Result pipeline input, Result values inside Unit,
  skipped steps after Err, continuation outside a failed pipeline, and
  explicit try returning Err from the nearest function. The expected output
  excludes the skipped step and the statement after the failed try.

`tutorial/run.sh` passed all twelve chapters after these changes. The README
now describes implemented ignore syntax and the distinction between a failed
pipeline value and explicit try. The larger Gist CLI has a separate link;
its files were verified to exist on the integrated main branch before linking.
No new language rule was chosen or changed.

## Cache defect reproduction

Before changing the harness:

1. `go test ./tutorial` passed; a second invocation returned `(cached)`.
2. In this worktree only, append a syntactically invalid top-level line to
   `internal/codegen/codegen.go`.
3. `go test ./tutorial` still returned `(cached)` success.
4. Restore the compiler source immediately.

The test imported only standard-library packages and built the compiler in
an external command; the compiler implementation was absent from its Go
package dependency graph. A separate malformed Gom-source probe did fail in
the original harness, so this report does not claim the same reproducible
cache gap for every existing fixture read.

## Prototype and verification

[The decision](../decisions/2026-11-03-tutorial-dependency-tracking.md) started
as draft `96f65f3`; prototype `3cdbe0e` adds actual compiler API compilation.
Each runnable chapter's API-generated Go must exactly equal the real CLI's
output, which is then executed with the Go toolchain. The intentional error
chapter must report its expected code through both API and CLI. This keeps
end-to-end command coverage and gives the test actual compiler dependencies.

An attempted fixture embed failed with `cannot embed ... in different module`:
each tutorial chapter owns a separate go.mod. Instead, source/expectation/module
fixtures and CLI main-package sources are read in the test process, registering
them with Go's test-cache input tracking. Modules used for execution come from
the chapter's own go.mod, with its repository replacement made absolute.

After the prototype, normal tests still returned `(cached)` when unchanged.
Temporary malformed edits were then tested and restored in finally blocks:

| Changed input | Result of ordinary `go test ./tutorial` |
| --- | --- |
| `internal/codegen/codegen.go` | Build failure before tutorial execution |
| `cmd/gom/cli.go` | Real gom subprocess build failure |
| `tutorial/11-polymorphic-functions/main.gom` | E0101 from actual API compilation |

Every damaged-input probe exited nonzero and none returned cached success.
After restoring all sources, `go test ./tutorial -count=1` passed.

## Final checks

- `gofmt -l .`: empty output.
- `go vet ./...`: passed.
- `go test ./...`: passed, including the API/CLI output comparisons.
- `tutorial/run.sh`: all twelve chapters passed.
- `go test ./tutorial -count=1`: passed after all temporary edits were restored.
- `git diff --check`: passed.

## Scope and follow-up

The reproducible cache defect was added to TODO.md and fixed here. No further
language defect was uncovered by these twelve chapter executions; this does
not close the separately recorded compiler/interop follow-ups. Tutorial assets
remain runnable programs, with no generated Go or binaries committed.

## Fresh review and runtime-input follow-up

The fresh reviewer found one P2 issue: after a successful cached run,
malformed Go appended to `stdlib/result/result.go` still produced cached
success. Compiler API imports do not cover packages used only by generated
Go. As the implementation owner, I accept the finding and require a fix;
the earlier three probes alone were insufficient to establish full generated
program dependency tracking. Other tutorial content and API/CLI comparisons
were accepted by the reviewer.

The finding was added to TODO.md, then addressed under the draft
[Go module input decision](../decisions/2026-11-03-tutorial-go-module-inputs.md)
(`4c2e271`); prototype `687e0b1` replaces the CLI-only source glob with actual
`go list -deps -json` discovery for both the CLI build and every generated
chapter. Discovery uses the subprocess's Go command, directory, and environment.
The test process reads module source directories and reported Go/cgo/assembly/
header/object/embed inputs, plus go.mod and available go.sum manifests,
including replacement modules. Directory reads cover newly added source files.
Standard-library packages remain tied to the test binary's Go toolchain.

Unchanged normal tests still return `(cached)`. The following worktree-only
probes each made ordinary `go test ./tutorial` exit nonzero instead of returning
cached success; all original sources were restored in finally blocks:

| Probe | Actual failure |
| --- | --- |
| Append invalid Go to `stdlib/result/result.go` | Generated Go build rejects the malformed Result implementation |
| Append invalid Go to `rt/rt.go` | Generated Go build rejects the malformed runtime implementation |
| Add malformed `stdlib/result/tutorial_cache_probe.go` | Generated Go build rejects the new source file |

Normal tests passed after restoring the first two edits; the added file was
removed immediately after its probe. No runtime source or probe file remains
in the committed diff. The implementation-gaps ledger item was registered by
the orchestrator and will be checked against this verified fix on integration.

After the review fix, `gofmt -l .` was empty; `go vet ./...`, `go test ./...`,
`tutorial/run.sh` (all twelve chapters), `go test ./tutorial -count=1`, and
`git diff --check` all passed again.
