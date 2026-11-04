# The evaluator keeps concurrency constructs compiled-only

- 状態: 採用（P5-1 で実装・検証した — [検証](../sketch/2026-11-04-p5-1-concurrency-verification.md)）
- 日付: 2026-11-04

## Background

The tree-walking evaluator (`internal/eval`) exists for the
semantics-comparison testing of design section 7: small programs are
evaluated directly from the typed AST and their results compared
against the generated Go code. That role makes it monomorphic, pure,
and resource-bounded by construction — every observation the
comparison makes must be deterministic.

P5-1 had to decide whether `go`, `select`, and channel iteration
become real concurrency inside the evaluator (goroutines plus a
`reflect.Select` interpreter) or stay `CodeUnsupported` (E9001)
diagnostics as they were scaffolded.

## Decision

Concurrency is compiled-only in the evaluator:

- `go` and `select` evaluate to E9001. They are expression forms, so
  no host-function escape hatch can ever stand in for them — unlike
  a Go *call*, a Go *control-flow construct* has no boundary a
  harness can bind.
- `for x in ch` over a channel value evaluates to E9001 ("for over a
  non-List iterable"), including channels a host function returns as
  `VHost`.
- `Chan.*`/`Task.*` members are ordinary `CallGo` nodes. They stay
  unbound in eval (`E9001` "unbound Go call") unless a harness binds
  them as host functions — in which case a deterministic,
  single-threaded use (e.g. a buffered channel's send/recv through
  host stubs) still evaluates, because it is just function calls.
  Only the *syntax-level* constructs are unreachable.

## Why not implement eval concurrency

- Goroutine scheduling is nondeterministic by definition; the only
  programs an eval implementation could compare against compiled
  output are exactly the trivially-ordered ones — buffered channels
  and fixed order — which host-stubbed `Chan` calls already cover.
- A faithful `select` needs Go's pseudo-random ready-case choice and
  blocking semantics reproduced on eval's value representation — a
  large, error-prone surface whose payoff is comparing programs the
  harness could simply compile and run.
- P5-1's own testing rule (only deterministic programs compare
  eval-vs-compiled) means nothing the eval implementation would add
  can produce a *failing* comparison today.

## Consequences

- `internal/eval/doc.go` lists the boundary as deliberate.
- `eval_test.go` pins `go`/`select` and `for`-over-host-channel as
  E9001.
- Compiled behavior is covered instead by `gom test`-driven
  end-to-end tests (`cmd/gom` `TestTestConcurrency`) and rt-level Go
  tests — the layer where the semantics actually run.
