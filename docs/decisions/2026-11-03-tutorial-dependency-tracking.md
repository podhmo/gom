# Track executable tutorial inputs and compiler dependencies

- 状態: 採用
- 日付: 2026-11-03

## Problem

The tutorial test builds the compiler in a subprocess. Its own Go package
imports only standard-library packages, so compiler implementation changes
need not invalidate its cached test result. Files read by the subprocess
also need explicit dependency tracking.

## Decision and verified prototype

Read tutorial source, expected output, and module fixtures in the test
process so Go test records those file dependencies. Compile each
runnable source through the compiler API and compare that generated Go to
the output of the real `gom build` command before executing it. This gives
the test an actual compiler-package dependency and checks the API/CLI
wiring while retaining end-to-end CLI coverage. Use the same directly read
fixtures for expected outputs and diagnostic input during the checks. Read
the CLI main-package sources in the test process as well, because that
package cannot be imported and is built only by the subprocess. A review
follow-up extends this to actual module inputs of both CLI and generated Go
through [Go dependency discovery](2026-11-03-tutorial-go-module-inputs.md).

An initial go:embed prototype was rejected by Go because each tutorial
chapter is a different Go module; embeds cannot cross module boundaries.

The prototype deliberately broke compiler, CLI, and tutorial source files
in temporary worktree edits. Ordinary `go test ./tutorial` rejected every
case and passed after restoration; unchanged inputs still use the cache.
[Verification report](../sketch/2026-11-03-tutorial-iteration.md). No language
behavior changes.
