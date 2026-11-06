# `(NamedIface, error)` / `(any, error)` の Ok ペイロード — メソッド持ち interface は派生 `Option`、nil-able 宣言ゲートを interface 全般へ

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-nilable-iface-pair-ok-payload.md)。spec.md §8.3/§8.4 へ反映済み）
- 日付: 2026-11-06
- 関連: [error-pair-ok-payload](2026-11-05-error-pair-ok-payload.md)（`(error,error)` の同族の穴）、[error-result-bindings](2026-11-05-error-result-bindings.md)

## Context

[error-pair-ok-payload](2026-11-05-error-pair-ok-payload.md)
closed the `(error, error)` hole: a nil-able first result whose
nil Gom cannot observe now derives `Result (Option T) GoError`,
and `nilablePresence` admits the declared Option-payload view for
`*T` and `error`. The same hole remains open for the rest of Go's
nil-able interface family:

- `func F() (net.Error, error)` derives `Result net.Error GoError`
  — a `(nil, nil)` return smuggles a nil `net.Error` into `Ok`
  where a later `e.Timeout()` panics as a nil-interface trap, and
  `Result (Option net.Error) GoError` declarations are rejected
  (`nilablePresence` admits only `*T` and the universe `error`).
- `func F() (any, error)` derives `Result Go.Any GoError`; the
  declared `Result (Option Go.Any) GoError` view is likewise
  rejected.
- Named empty interfaces (`type Empty interface{}`) and lone
  interface results (`func F() io.Reader` → `Option io.Reader`)
  sit behind the same gate.

The distinguishing principle from the `(error,error)` family is
already written down: **type-derived views are honest; strict
claims are declared and verified at the boundary.** A nil is
unobservable-dangerous when calling anything on it panics from
Gom — an interface *with methods* — while `*T` exposes
`Ptr.is_nil`/`Ptr.to_option` and a `[]T`/map/chan nil is a
legitimate value with Gom-visible ops. `any` and named empty
interfaces have no methods: a nil inside `Ok` is inert — it flows
back to Go intact (`fmt.Println` prints `<nil>`), so `Ok None` is
not obviously a better default for the derived view.

## Decision

### The nil-able class for declared `Option` views is `*T` or any interface type

`nilablePresence` widens from `{*T, error}` to `{*T}` ∪
{types whose underlying is `*types.Interface`} — `error`, `any`,
named method-bearing interfaces (`net.Error`, `io.Reader`,
`fmt.Stringer`, user interfaces), and named empty interfaces.
`v != nil` reads a genuine presence channel for all of them; the
declaration is meaningful for APIs whose nil result means
absence. Slices, maps, channels, named non-interface types, and
func types stay out — their nils are legitimate values with
Gom-visible semantics, matching the existing gate's intent.

The admission applies symmetrically:

- `(I, error)` admits `Result (Option T) GoError` whenever the raw
  first result's underlying is an interface (as well as `*T`) —
  materialized `option.FromGo(v, v != nil)` via `ConvOptNil`.
- A lone `*T`/interface result admits `Option T` (previously only
  `*T` and `error`) — `Option io.Reader`, `Option Go.Any`.

### `(method-bearing interface, error)` derives `Result (Option T) GoError`

Extending the `(error,error)` rule: when the raw first result's
underlying is an interface **with methods**, the derived view
wraps the payload — `func F() (net.Error, error)` reads
`Result (Option net.Error) GoError`. A nil method-bearing
interface is exactly the unobservable-dangerous case: every Gom
operation on it (a Go method call) panics on nil, and `Ok` claims
a payload is present.

`(any, error)` and `(named-empty-interface, error)` keep the
verbatim `Result T GoError` derived view: the nil is inert — no
method can panic on it — and a `(nil, nil)` return of `any` can
legitimately mean "the nil value", so forcing `Option` would
conflate absence with data. Externs and standard bindings may
still declare `Result (Option Go.Any) GoError` / `Result (Option
Empty) GoError` when the API's nil does mean absence. Lone
interface results also keep their verbatim derived view (`-> I`,
not `Option I`): there is no `Ok` tag asserting a payload's
presence, the nil *is* the value, and wrapping every lone
interface result would change every API for the sake of a probe
the caller may not want.

### `Result I GoError` over `(I, error)` is the never-nil claim — boundary-checked

A declared non-`Option` `Result T GoError` whose payload `T`
spells the raw method-bearing-interface result verbatim is the
same strict claim `Result GoError GoError` makes over
`(error, error)`: "when the second result is nil, the first is a
real value". `lower.GoSig` records that fact — `ResNilable` is set
by `mapSignature`/`BindExtern` when the raw first result is a
method-bearing interface — and lowering emits the same boundary
check (`if err == nil && v == nil { panic }` for `ResultFromGo`;
`if v == nil { panic }` after the escape for `try`/`|?>`). `*T`,
`any`, and named-empty-interface first results do not trigger it:
a `Ptr`'s nil is observable (`is_nil`), and an inert nil any needs
no claim.

## Alternatives

- **Wrap every interface including `any`** — rejected: `Ok (nil
  any)` cannot panic and `(nil, nil)` is a legitimate "nil is the
  answer" return for `any`-typed APIs (caches, serializers);
  deriving `Option` conflates absence with the nil datum. The
  declared view remains available for APIs where nil means
  absent.
- **Wrap slices/maps/chans too** — rejected: their nils carry
  Gom-visible semantics (empty list, working map ops, blocking
  chan) rather than unobservable panics — same reason the
  `(error,error)` decision excluded them.
- **Status quo + spec wording** — rejected: it leaves a nil
  `net.Error` inside `Ok` undetectable — the same silent hole
  class the wave exists to close, now reachable through every Go
  interface type.
- **Reject `Result I GoError` for method-bearing `I` instead of
  guarding it** — rejected: it breaks the derived-honest /
  declared-strict symmetry the family established; APIs do make
  never-nil promises, and the claim is checkable.

## Consequences

- `(net.Error, error)`, `(io.Reader, error)`, `(fmt.Stringer,
  error)` and user-interface pairs derive `Result (Option T)
  GoError`; calls materialize `option.FromGo(v, v != nil)` in the
  Ok slot and `try` yields `Option T`.
- `(any, error)` keeps `Result Go.Any GoError`; `(Empty, error)`
  keeps `Result Empty GoError`. Declared `Result (Option T)
  GoError` is admitted for all interface first results.
- A declared `Result I GoError` over a method-bearing `I` emits
  the boundary panic on `(nil, nil)`; the panic text generalizes
  from "never-nil GoError payload" to "never-nil payload".
- `nilablePresence` covers lone interface results too: `Option
  io.Reader`, `Option Go.Any` declarations become legal
  (`GoResultsNil`).
- spec 8.3's `(T, error)` row gains the interface-payload note;
  8.4's admission list and materialization text record the widened
  nil-able class.
