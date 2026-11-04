# `Chan`/`Task` ship as compiler-owned member tables backed by rt

- 状態: 採用（P5-1 で実装・検証した — [検証](../sketch/2026-11-04-p5-1-concurrency-verification.md)）
- 日付: 2026-11-04

## Background

Spec section 10 fixes the `Chan` and `Task` operation surfaces, but
the stdlib modules are otherwise derived from embedded `.gomi`
interface files. Two of the required signatures cannot be spelled in
Gom:

- `Chan.send`/`recv`/`close` must accept a plain `Chan a` *and* the
  matching directional type (`SendChan a`/`RecvChan a`). Gom
  parameter types admit no such "either" — the only place `Chan a`
  reaches a `SendChan a` parameter is the implicit channel-direction
  conversion of spec section 10.2, which exists at Go-call
  boundaries only.
- `Chan.recv` must fuse the two Go results `(v, ok)` into a single
  `Option a` — a result-shape fact, not a type spelling.
- `Task.spawn`'s parameter `Unit -> a` renders on the Go side as
  `func(struct{}) T`; the unit *parameter* is calling convention,
  invisible to the Gom signature.

## Decision

`internal/stdlib/builtin.go` carries hand-built member tables for
`Chan` and `Task`. Every member is a `resolve.Symbol` with `IsGo:
true` and `Package: github.com/podhmo/gom/rt`, forwarding to a small
generic helper in `rt/` (`MakeChan`, `ChanSend`, `ChanRecv`,
`ChanClose`, `ChanRecvOnly`, `ChanSendOnly`, `SpawnTask`,
`AwaitTask`). Each entry pairs the Gom-facing `check.Signature`
(rigid type variable `a`, `GoConstraintAny`) with the Go-facing
`lower.GoSig` (arity + `GoResults` shape — `ChanRecv` declares
`GoResultsOK` so lowering emits the `option.FromGo` pair
conversion).

Making each member an `IsGo` symbol is what makes the call a
genuine Go boundary, so the section-10.2 implicit narrowing applies
*exactly* where the spec grants it — `Chan.send ch v` on a
`Chan Int` typechecks because the parameter is `SendChan a` at a Go
call, while a user-defined `SendChan` parameter still requires
`Chan.sendOnly`.

## Runtime shape

- `rt.Task[T]` is `{done chan struct{}, box *taskBox[T]}` — the
  result lives in a shared cell so copies of the handle observe the
  same outcome; `SpawnTask` publishes value-or-panic into the box
  before closing `done`, and `Await` re-panics a captured panic.
- Channel helpers are thin direction-typed wrappers around Go's own
  primitives; no new representation is introduced — `Chan a` *is*
  `chan T`.

## Alternatives

- `.gomi` like the other stdlib modules — rejected: the signatures
  above are not expressible, and fudging them (e.g. `send` on
  `Chan a` only) would force users through `sendOnly` for every
  builtin call, contradicting the spec surface.
- A runtime narrowing helper or a new IR node — rejected: narrowing
  is pure assignability, the channel *value* is unchanged. What
  landed instead is a new `ConvKind` (`ConvChanDir`) on the
  existing `core.Conv` node — `Ascribe{Conv: true}` from the
  checker lowers through `convKind`/`chanDirPair` and codegen
  prints the parenthesized `(chan<- T)(ch)` conversion Go requires.
