# Track actual Go module inputs of tutorial subprocesses

- 状態: 採用
- 日付: 2026-11-03

## Review finding

Compiler API dependencies do not include every package used only by generated
Go. The reviewer confirmed that changing stdlib/result/result.go can still
leave `go test ./tutorial` returning cached success. The implementation owner
accepts this P2 finding: dependency tracking must cover executed artifacts,
not only the compiler that generates them.

## Verified decision

Before building the CLI and running each generated chapter, use the same
Go command, working directory, and environment to ask `go list -deps -json`
for actual module packages. Read their source directories, Go/cgo/assembly/
header/embed files, and module manifests in the test process. Directory reads
also invalidate cached results after a source file is added or removed.
Standard-library sources need no extra reads because the test binary itself
is tied to the same Go toolchain.

This extends the [existing dependency-tracking decision](2026-11-03-tutorial-dependency-tracking.md).
Prototype validation repeated the reviewer's invalid runtime-source edit,
plus an edit under rt and a new runtime source file. Each ordinary test run
failed without cached success; all inputs were immediately restored. Unchanged
inputs still use the cache. [Results](../sketch/2026-11-03-tutorial-iteration.md#fresh-review-and-runtime-input-follow-up).
