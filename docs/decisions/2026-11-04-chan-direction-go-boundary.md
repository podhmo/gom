# Channel direction narrows implicitly only at Go-call boundaries

- 状態: 採用（P5-1 で実装・検証した — [検証](../sketch/2026-11-04-p5-1-concurrency-verification.md)）
- 日付: 2026-11-04

## Background

Spec section 10.2 maps Go channel directions — `chan T` → `Chan a`,
`<-chan T` → `RecvChan a`, `chan<- T` → `SendChan a` — and grants an
implicit conversion so a `Chan a` value feeds a Go function's
`chan<-`/`<-chan` parameter without ceremony. The open part was how
far the implicit rule reaches: Go's own assignability admits
`chan T` wherever `chan<- T`/`<-chan T` is wanted, but Gom-to-Gom
calls are not Go's domain.

## Decision

- The implicit narrowing applies to *Go-call boundaries only* —
  arguments checked against a parameter that was registered as a
  live Go-call parameter (`checker.goParamWants`, fed by `goWant`
  around each `IsGo` argument check — including piped arguments to
  arity-1 Go callees, and the rt-backed `Chan.*` members which are
  `IsGo` by construction).
- The check itself is syntactic, not `go/types`: `chanDirConv`
  (internal/check/expr.go) name-matches a builtin `Chan a` source
  against a builtin `SendChan a`/`RecvChan a` parameter and
  requires the element types to agree (equal, or unifiable when a
  generic callee's element is still a meta). The answer matches
  what `AssignableTo` would give for these shapes — `chan T` reaches
  `chan<- T`/`<-chan T`, nothing else does — but the oracle is only
  consulted on the *interface* conformance path, where a channel
  pair correctly returns false.
- The typed argument becomes an `Ascribe{Conv: true}`; lowering
  re-verifies the pair (`lower.chanDirPair`) and emits a
  `core.Conv` of kind `ConvChanDir` — an ordinary conversion node,
  no new IR shape — which codegen prints as the parenthesized Go
  conversion `(chan<- T)(x)` / `(<-chan T)(x)`. Nothing about the
  *value* changes; Go requires the written conversion only because
  the spelled types differ.
- Gom-to-Gom calls are unchanged: `SendChan a`/`RecvChan a`
  parameters require the explicit `Chan.sendOnly`/`Chan.recvOnly`
  views. This keeps direction a deliberate part of a Gom API's
  contract.

## `<-chan struct{}` — the `ctx.Done()` shape

`struct{}` is Gom's `Unit` representation, so `<-chan struct{}`
maps to `RecvChan Unit` and `chan struct{}` to `Chan Unit`. That
makes `ctx.Done()` (and any `chan struct{}` signal channel) arm a
`select` receive case or close through a `chan<- struct{}`
parameter with no special casing — verified in
`testdata/goiface/chanops.go` (`Done`, `CloseSignal`, `NotifyDone`)
and the `chan_go_boundary` golden.
