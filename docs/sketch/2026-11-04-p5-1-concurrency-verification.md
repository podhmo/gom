# P5-1 concurrency verification

- 日付: 2026-11-04

Verification for the three P5-1 implementation decisions:

- [decisions/2026-11-04-builtin-chan-task-rt.md](../decisions/2026-11-04-builtin-chan-task-rt.md)
  — `Chan`/`Task` as compiler-owned, rt-backed member tables
- [decisions/2026-11-04-chan-direction-go-boundary.md](../decisions/2026-11-04-chan-direction-go-boundary.md)
  — implicit direction narrowing at Go-call boundaries,
  `struct{}` ↔ `Unit` for `<-chan struct{}`
- [decisions/2026-11-04-eval-concurrency-compiled-only.md](../decisions/2026-11-04-eval-concurrency-compiled-only.md)
  — `go`/`select`/channel-`for` stay `E9001` in the evaluator

## Method

- Spec fixtures (`testdata/spec`) through `build.Compile`, one per
  section-10 rule; generated `*.expected.json` reviewed by hand.
- Golden fixtures (`testdata/golden`) reviewed line by line for the
  emitted shapes.
- Runtime: `rt` Go tests plus an end-to-end `gom test` run
  (`cmd/gom` `TestTestConcurrency`) — generated code compiled and
  executed by the real Go toolchain. Every case is deterministic:
  buffered channels or a sole-receiver rendezvous, at most one
  ready non-default select arm.

## Chan / Task member surface

`internal/stdlib/builtin.go` wires the tables; `e.modules["Chan"]`
and `["Task"]` resolve `Chan.make`…`Chan.sendOnly`, `Task.spawn`,
`Task.await` to `IsGo` symbols on `github.com/podhmo/gom/rt` with
paired `check.Signature`/`lower.GoSig` oracles.

- `Chan.send`'s parameter is `SendChan a` — a `Chan a` argument
  reaches it through the boundary narrowing (the `IsGo` call), not
  a new assignability rule.
- `Chan.recv` carries `GoResultsOK`, so lowering fuses the `(v, ok)`
  pair into `option.FromGo` — closed channel ⇒ `None`, verified in
  `chan_ops.golden.go` and the `buffered channel` e2e test.
- `Task.spawn`'s Go signature is `func(struct{}) T` — the `Unit`
  parameter is part of the calling convention; `rt.SpawnTask`
  publishes result-or-panic through a shared `taskBox` so handle
  copies observe the same outcome (`TestSpawnAwaitTask`, and
  `TestAwaitTaskRethrowsPanic` for the rethrow).

## Direction narrowing at Go boundaries

- `chan<-`/`<-chan` parameters accept `Chan a` arguments. The gate
  is `checker.goParamWants` — `goWant` registers each `IsGo`
  parameter around its argument check, and `chanDirConv`
  (internal/check/expr.go) admits exactly the builtin
  `Chan a` → `SendChan a`/`RecvChan a` pair with agreeing elements
  (syntactic `chanElem` name-match + `types.Equal`/meta unify — the
  same answer `go/types` `AssignableTo` gives for these shapes; a
  `RecvChan a` to `chan<-` is `E0401`, see
  `E0401_go_chan_recv_as_send.gom` and the symmetric
  `E0401_go_chan_send_as_recv.gom`). `AssignableTo` itself is only
  consulted on the interface conformance path.
- The conversion is real IR: `Ascribe{Conv: true}` lowers through
  `convKind`/`chanDirPair` to `core.Conv{Kind: ConvChanDir}`, and
  codegen prints the parenthesized Go conversion
  `(chan<- T)(ch)`/`(<-chan T)(ch)` (`chan_ops.golden.go`,
  `chan_go_boundary.golden.go`). The value is unchanged; only the
  spelled type differs.
- Gom-to-Gom calls keep explicit views: `SendChan` parameter versus
  `Chan` argument is `E0401` (`E0401_gom_chan_direction.gom`);
  `Chan.recvOnly`/`sendOnly` produce the views (`views` in
  `chan_ops.golden.go`).
- The same conformance now reaches piped arguments of arity-1 Go
  callees — `ch |> goiface.CloseSignal`, `ch |> Chan.recv`,
  `xs |> goiface.Sum64` (`ok_pipe_go_boundary.gom`), while a Gom
  `SendChan` parameter still rejects a piped `Chan`
  (`E0401_pipe_gom_direction.gom`).

## `<-chan struct{}` / `ctx.Done()`

`struct{}` maps to `Unit`, so `<-chan struct{}` is `RecvChan Unit`
and `chan struct{}` is `Chan Unit`. Verified through
`testdata/goiface/chanops.go` (`Done`, `CloseSignal`,
`NotifyDone`): `wait (goiface.Done ())` compiles, a select arm over
`done` emits `case <-done:` (`chan_go_boundary.golden.go`), and the
e2e `notify done` test rendezvous-closes the signal channel.

## go / select / for emission

- `go { ... }` emits `go func() { ... }()` with the Unit discard
  (`go_spawn.golden.go`); a non-Unit body is `E0701`, a literal's
  default type is what the diagnostic reports (`got Int`).
- Select arms map 1:1 to Go cases — `case v1, ok := <-jobs:` +
  `option.FromGo`, `case ch <- v:`, `default:` — channel
  expressions are evaluated to temporaries once, in source order
  (`select_arms.golden.go`).
- `for x in ch` emits `for v := range ch` (`drain_for` in
  `chan_ops.golden.go`), terminating at close — `for drains until
  close` e2e.

## Evaluator boundary

- `check.GoExpr`/`check.SelectExpr` stay `CodeUnsupported` (E9001);
  `for` over a host-supplied channel value (`VHost` wrapping
  `chan`) is `E9001` "for over a non-List iterable" — pinned in
  `eval_test.go` (`for over a host channel unsupported`).
- `Chan.*`/`Task.*` evaluate only via host-function bindings like
  any other `CallGo` — the syntax-level constructs have no such
  escape hatch, which is the decision's actual scope.

## Section-10 spec fixtures

| Rule | Fixture |
| --- | --- |
| `go` body must be `Unit` | `E0701_go_body_value` |
| `try` cannot cross a `go` boundary | `E0702_try_in_go` |
| select arm type unification | `E0401_select_arm_mismatch` |
| receive arm needs a receivable channel | `E0401_select_recv_sendonly` |
| send arm needs a sendable channel | `E0401_select_send_recvonly` |
| at most one `default` | `E0703_select_two_defaults` |
| empty select | `E0705_empty_select` (pre-existing) |
| receive arm binds a variable/`_` | `E0101_select_recv_pattern` (parser; checker-level E0704 pinned in `check_regressions_test.go`) |
| `for` needs a receivable channel | `E0401_for_send_only` |
| direction narrowing, Go boundary | `ok_go_chan_direction`, `E0401_go_chan_recv_as_send` |
| direction narrowing absent Gom-side | `E0401_gom_chan_direction` |
| `Chan`/`Task` surfaces | `ok_chan_ops`, `ok_task`, `E0201_chan_unknown_member` |
| `<-chan struct{}` / `ctx.Done()` | `ok_chan_unit` |
