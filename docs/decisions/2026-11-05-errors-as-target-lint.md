# `errors.As` の target 契約 — 型付きバインディングではなく警告リント

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-errors-as-target-lint-verification.md)。spec.md §8.4 へ反映済み）
- 日付: 2026-11-05
- 関連: [error-result-bindings](2026-11-05-error-result-bindings.md)（標準バインディングの置き場所）、[ptr-ops-surface](2026-11-05-ptr-ops-surface.md)（`Ptr.new` は `errors.As` の out-pointer 用と明記済み）

## Context

`errors.As(err, target)` has a contract Go's type system does not
express: `target` must be a *non-nil pointer* whose element type
either implements `error` or is an interface type — anything else
panics at run time. Through the derived view Gom sees
`(GoError, Go.Any) -> Bool`, and `Go.Any`'s implicit boxing accepts
*every* argument, so `errors.As e 42` typechecks today and panics
only at run time. `errors.Is` has no such trap (its second argument
is an ordinary error value); `As` is the one boundary where the
shape of the *argument* — not just its type's mapping — carries
a runtime contract (wave8 error-boundary review).

## Decision

### `lintErrorsAsTarget` reports W0502 on provable violations

A post-inference lint — the same sweep `lintChanMake` and
`lintGoSliceCopy` run in — inspects every `errors.As` Go call
(callee package `errors`, name `As`) and warns when the target
argument is *provably* invalid:

- the argument's type is not `Ptr _` — Go requires a non-nil
  pointer target; anything else panics;
- the argument is a literal `Ptr.nil ()` (possibly under
  ascribes) — nil panics by the same contract;
- the argument's type is `Ptr E` where `E` is a concrete shape
  that is neither an interface nor able to implement `error`:
  the builtin concrete types (`Int`/`Float`/`String`/…, `List`,
  `Result`, `Chan`, `Go.Slice`, `Go.Map`, …), function and tuple
  types, and declared record types. `Option` and declared sums
  stay out of this set — their Go representations *are*
  interfaces, so `errors.As` accepts them legally (and simply
  never matches). Named Go types (`types.Go`), `GoError`,
  `Go.Any`, nested `Ptr` elements, aliases with parameters,
  unsolved metas and type variables stay silent — the checker
  cannot prove them wrong cheaply, and a false positive on the
  common `Ptr <Go error type>` shape would be worse than the
  hole.

The warning is advisory: the lint never gates a later phase, and
the contract itself stays Go's — the lint only surfaces a
guaranteed panic while the line is still in front of the user.

## Alternatives

- **Typed standard binding `errors.As : GoError -> Ptr a ->
  Bool`** — rejected. Standard bindings are concrete signatures;
  a `Ptr a` entry needs the generic-instantiation mechanism
  externs do not currently support (spec 8.4: declared signatures
  may not contain type variables). Worse, `Ptr a` alone does not
  express the contract — `Ptr Int` still typechecks and still
  panics; the binding would *narrow* the surface without removing
  the trap, while claiming a precision it does not have.
- **Full static check via the interface oracle** — rejected.
  `InterfaceAssignable(E, error)` cannot separate "E is an
  interface" (legal target regardless) from "E is concrete and
  does not implement error" (panic), so even the oracle cannot
  make the check total on named Go types. Erroring where it can
  decide and staying silent elsewhere would make two identical
  programs fail or pass on oracle plumbing, not on the contract.
- **Status quo (document-only contract)** — rejected: the failure
  is a guaranteed panic invisible at compile time, and the
  provable cases are cheap to catch — exactly what a warning
  exists for.

## Consequences

- `errors.As e 42` and `errors.As e (Ptr.make 1)` warn W0502;
  `errors.As e (Ptr.new ())` over a `Ptr <Go error type>` stays
  silent — the intended spelling.
- spec §8.4 gains the lint note: `errors.As` keeps its derived
  `(GoError, Go.Any) -> Bool` surface under the lint, since
  `Go.Any` cannot spell the contract — a Go-boundary contract,
  so it lives beside the other boundary rules rather than in §10.
