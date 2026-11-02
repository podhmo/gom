# P3-1 Go import session audit

Date: 2026-11-03. Go: 1.27.3 darwin/arm64.

## Scope and loading mechanism

The implemented `go list -export` + shared `go/importer.ForCompiler` mechanism
was already adopted by [the loading decision](../decisions/2026-11-03-import-go-loading-mechanics.md).
The TODO and design still named the earlier go/packages proposal. This audit
checks the intended behavior before adding another dependency. No network
research was needed; the evidence is executable regression tests.

`internal/goimport/session_test.go` creates a separate dependency module and a
consumer module with a local `replace`. No published module or network is used.

## Results

- Both import orders (`model` then `service`, and the reverse) keep the same
  `go/types` named object through a dependency edge. The original type, an alias
  exported by another package, and that package's function parameter/result
  pass `types.Identical`. Different packages' equally named types stay distinct.
- Repeated loads return the same resolver package and symbol. Gom-view handles
  preserve the alias identity and distinguish the unrelated named type.
- An unsupported function-typed parameter remains in the member table. Merely
  importing the package produces no error; referencing the symbol produces one
  E0501 with the unsupported-signature reason.
- Generated Gom wrappers call the module dependency and are called by Go tests.
  Named structs, named interfaces (nil and concrete), and Go.Slice (nil and
  shared backing storage) survive the Go → Gom → Go round trip.
- Existing standard-library loader and execution tests continue to pass.

## Reproduced and fixed gap

`type Failure = error; func Parse(bool) (Value, Failure)` mapped to
`Bool -> (Value, GoError)` instead of `Bool -> Result Value GoError`.
An annotated Gom wrapper failed E0401 before code generation. `mapType` already
unaliases the error type, but `isError` classified the result shape before doing
so. Calling `types.Unalias` there fixes the mismatch without changing a named
new type into an alias. Alias chains and a sole error return are tested too;
the latter produces `Result Unit GoError` and a successful nil-error round trip.

The draft and failing tests were committed first (`689f27a`), followed by the
prototype (`efb7dd5`). `go test ./internal/goimport -count=1` passes with the fix.

## Boundaries

This audit completes the loading requirements of P3-1 using the adopted
mechanism. It does not complete interface assignability/conversions (P3-2),
List boundary copying or Go generics (P3-3), extern overrides (P3-5), or `.gomi`
public API export (P4-1). The reverse calls in this test are same-package Go
calls to generated functions; cross-package exported API remains separate work.

## Completion checks

- `gofmt -l .`: empty output.
- `go vet ./...`: passed.
- `go test ./...`: passed, including the executable tutorial suite.
- `go generate ./internal/tool/decisionsdoc`: regenerated the decision index;
  its consistency test passed in the full suite.
- `git diff --check`: passed.

## Independent review

A fresh reviewer (`review_go_import`) inspected the implementation and confirmed
that alias normalization preserves distinct named types, and that the module
replace, both import orders, use-site E0501, alias-chain, and bidirectional
execution tests cover the change. The reviewer reported no actionable findings
and independently passed gofmt, vet, and tests. As the implementation author,
I agree that no additional change is needed: the fix only normalizes transparent
aliases before the existing result-shape classification. This follow-up changes
only this report, so the full suite was not rerun.
