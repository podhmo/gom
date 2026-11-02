# Zero-result Go calls: prototype and regression verification

- 日付: 2026-11-03
- 状態: verified; decision adopted

## Reproduction and ownership

The Gist CLI exposed `fn stop () -> Unit = os.Exit 1`, which generated
`return os.Exit(...)` and failed Go compilation. The loader classified
zero Go results as `GoResultsOne`. This report verifies
[the explicit-result decision](../decisions/2026-11-03-void-go-call-results.md).
No network or real GitHub operation is needed.

## Prototype

`GoResultsNone` is emitted from the authoritative Go signature. Lowering
normalizes it to a discard CallGo followed by an explicit Unit atom, both
for direct calls and function-value/placeholder adapters. The existing
pipeline path now uses the same normalization rather than guessing from
`GoResultsOne` plus a Unit type. Core validation accepts None only in a
discard binding, requires Unit, and rejects value/tail use. Codegen is
unchanged.

`GoResultsOne` still means an actual single Go result. Anonymous
`struct{}` return signatures remain hidden with a reason; they are not
silently treated as void. The fallback used by hand-built typed ASTs maps
builtin Unit to zero results and preserves file-local Unit identity.

## Executable verification

`internal/build/void_test.go` compiles Gom and invokes its generated
functions from Go, then calls Go fixture functions and methods. It checks:

- top-level initializer, function tail, Unit binding and argument;
- two arguments with observable left-to-right effects;
- both branches of if/match;
- one-argument Go function value and `_` placeholder adapters;
- named-receiver method calls and a direct nullary runtime.Gosched call;
- Go callers passing nil slices, nil interfaces and named values through
  generated Gom functions back to Go;
- the original os.Exit tail compiles, without calling it in the test.

The exact effect trace is `TtbuauLR2inyzvcmdswN`. Core tests reject bare
None tails, non-discard bindings and non-Unit None calls. The loader test
checks Gosched's None shape and keeps a one-result struct signature
unsupported. The spec fixture has no diagnostics, and the generated golden
shows calls followed by `return struct{}{}` rather than returned calls.

The existing `TestPipeGoInflowConversions` passes, including Unit input,
explicit try and block input. Its golden remains unchanged.

## Scope

Captured nullary Go function values still have the previously documented
checker limitation in
[go-nullary-fn-value](../problems/2026-11-02-go-nullary-fn-value.md):
`let f = runtime.Gosched; f ()` reports E0401 before lowering. This change
does not broaden those checker rules. Direct nullary calls are covered.
No new language-level restriction or source syntax is introduced.

## Final checks

On Go 1.27.3 darwin/arm64, `gofmt -l .` produced no output,
`go vet ./...` passed, and `go test ./...` passed. The spec/golden suite,
zero-result loader/Core tests, and the executable void/pipeline tests
passed separately during prototype verification. `git diff --check` is
clean. Codegen and existing pipeline golden files needed no changes.

## Fresh review and implementation decision

The fresh `review_void_go_calls` agent reviewed `b8d9c51` and reported no
change requiring action. The review confirmed the appended enum preserves
existing values, discard-to-Unit normalization and Core validation. Its
additional ordinary-pipe, composition and branch-argument execution probes
preserved the exact effect order `PACIBC`. Formatting, vet, the full test
suite and diff checks passed independently during review.

As the implementation owner, I judge that no additional code change is
needed. This follow-up records the review only; the already passing full
checks are not rerun for the prose-only change. The integration owner will
mark the corresponding item resolved in `docs/implementation-gaps.md`,
retaining links to this verification report and the adopted decision.
