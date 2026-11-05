# `error` 単独の戻り値に対する宣言ビューと標準バインディングの置き場所

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-error-result-bindings-verification.md)。spec.md §8.3/§8.4 へ反映済み）
- 日付: 2026-11-05

## Context

[go-calls-as-tuples](2026-11-01-go-calls-as-tuples.md) and
[go-interop-boundary](2026-11-01-go-interop-boundary.md) map a lone Go
`error` result to `Result Unit GoError` by *type alone*. The type
cannot separate "creating an error value" (`errors.New`,
`fmt.Errorf`) from "a fallible operation" (`os.Remove`) — a Go
`error` is both a value and the nil-means-success flag, and Go's
type system has no never-nil spelling. The consequence is recorded
in [problems/goerror-construction](../problems/2026-11-04-goerror-construction.md):
nothing in Gom can *fabricate* a `GoError` except peeling an `Err e`
binding, and `errors.Is`/`errors.As` have no first-class targets of
their own.

The same situation already has an answer for `(T, bool)`: the
default stays by-type (a tuple), exceptions are *declared* — by an
`extern` entry or by a standard binding (`os.LookupEnv`'s Option).
The open question this settles: what form the declared exception
takes for `error`, and where the standard-binding list lives. The
[extern-override-binding](2026-11-03-extern-override-binding.md)
decision rejected `error` → `GoError` outright (E0509) because a
nil-able `error` presented as `GoError` would be a lie.

## Decision

### A lone `error` result admits three declared views

| declared result | meaning | Core shape |
| --- | --- | --- |
| `Result Unit GoError` | a fallible operation — the type-derived default (unchanged) | `GoResultsErrOnly` |
| `GoError` | a *never-nil claim*: the callee creates, not fails | `GoResultsErrValue` (new) |
| `Option GoError` | a nil-able error *value* — `errors.Unwrap`, `errors.Join` | `GoResultsNil` (the `*T` gate widens to `*T` or `error`) |

`Option` over `error` is the same reading as `Option` over `*T`:
the result's nil is the absence channel, materialized by
`option.FromGo(v, v != nil)`. The `*T`-only gate was an artifact of
pointer syntax, not a principle.

### The never-nil claim is checked at the boundary

`-> GoError` is a claim Go's types cannot prove. Rather than trust
it, the generated call verifies it:

```go
v := errors.New(m)
if v == nil {
    panic("gom: errors.New returned nil error — its binding declares a never-nil GoError result")
}
```

A violated claim is a contract violation in the sense of spec
section 8.6 — like a nil ADT value received from Go — and panics at
the boundary instead of silently smuggling a nil `error` into Gom,
which has no nil. The check is uniform: it also guards the
compiler-curated standard bindings below, where it doubles as drift
insurance. `GoResultsErrValue` is a *single-result* shape whose
mandatory consumer is the new `ErrorFromGo` node — a bare
`GoResultsErrValue` `CallGo` is a validator violation, so the check
cannot be dropped.

### Standard bindings live in a compiler-curated table in `internal/goimport`

The standard-bindings list (the open-questions entry) is a Go-import
concern: the loader already produces each member's two signature
views, and a standard binding is exactly the declared-override an
`extern` entry would produce. `internal/goimport/stdbind.go` holds
the table — package path → member name → declared result view —
applied at member derivation through the *same* result-admission
classification `BindExtern` uses, so the two mechanisms cannot
drift apart. A drifted table entry (a Go release changing a bound
signature) marks the member Hidden with a drift reason — the same
channel any unrepresentable member reports through — rather than
panicking or silently falling back.

Inaugural entries:

| member | declared view | why |
| --- | --- | --- |
| `errors.New` | `fn (String) -> GoError` | documented constructor — never nil |
| `fmt.Errorf` | `fn (String, List Go.Any) -> GoError` (variadic) | never nil; the derived `...any` parameters already work — `Go.Any` stays inference-only |
| `errors.Join` | `fn (List GoError) -> Option GoError` (variadic) | documented nil-able (nil when every input is nil) |
| `errors.Unwrap` | `fn (GoError) -> Option GoError` | nil unless the chain continues |

An explicit `extern` entry still binds against the *raw* Go
signature, so a user declaration overrides a standard binding —
`extern go "errors" { fn New (msg: String) -> Result Unit GoError }`
restores the type-derived default for callers that want it.

## Alternatives

- **A dedicated builtin (`Go.error : String -> GoError`)** —
  rejected as *the* form. It fabricates from a string only; it gives
  users no way to declare their own never-nil Go functions, and it
  adds a parallel surface for what is already the `(T, bool)` →
  `Option` exception shape. Nothing prevents adding it later as a
  convenience.
- **Trust the claim (emit no check)** — rejected. A `-> GoError`
  over `os.Remove` is a lie that would surface as a stray nil
  `error` values downstream; the spec's contract-violation family
  (8.6) says boundary lies panic.
- **`errors.Join`/`errors.Unwrap` as never-nil** — wrong: both are
  documented nil-able. `Option GoError` is the honest view.
- **Ship the standard bindings as a `.gom` extern prelude** —
  deferred. It needs module plumbing no other surface requires
  (every compile would parse/resolve an extra file); the
  compiler-side table reuses the extern admission machinery and can
  always be re-exposed as source later.
- **`(T, error)`-shaped multi-results** — untouched. A pair whose
  second element is `error` stays `Result T GoError`; no declared
  views are added for it.

## Consequences

- `errors.New("m")` and `fmt.Errorf("%w", e)` are `GoError` values —
  `Err (errors.New m)` fabricates; the `mkerr` peel idiom is
  obsolete.
- `errors.Is`/`errors.As` gain honest targets: `Ptr.new` at
  `Ptr GoError` gives `errors.As` the `*error` out-pointer it needs.
- `errors.Unwrap`/`errors.Join` become matchable on presence —
  `match errors.Unwrap(e) { Some inner -> ..., None -> ... }`.
- `try` / `|?>` are unaffected: the new views are not `Result`, so
  the escape-fusion conditions (GoResultsErr/ErrOnly) do not fire,
  and a `GoError` step under `|?>` is rejected by the existing
  step-must-return-Result rule.
- Code that matched `errors.New(...)`'s old `Result Unit GoError`
  view (the peel idiom — `cal`/`jsoncfg` in testdata/realworld)
  must be updated; the standard binding is the behavior change
  users see, which is why it is a spec (8.4) change, not design.
