# `|?>` semantics differ structurally between eval and lowering

- 状態: 一部解決（2026-11-03、主要な値意味論の不一致を解消）
- 日付: 2026-11-02

## Situation

`internal/lower` implements `x |?> f` per docs/design.md 4.6 and the
try-boundary decision: the inflow `Result` is unwrapped by an
`EscapeResult` (passthrough, `Wrap == ""`), the step runs on the
payload, its own `Result` is escaped with `Wrap == stepName`, and the
success payload is re-wrapped in `Ok`. A direct Go `(T, error)`
operand or step fuses to `EscapeCall`. No intermediate `Result` is
materialized; an invalid-tag value reaches `DPanic` through the
escape consumers.

`internal/eval` implements the same operator at the *value* level:
if the inflow is `Err`, the whole form evaluates to that `Err` and
the step is skipped; if `Ok`, the step runs and its `Result` is the
form's value.

## Why it is a problem

For well-formed values the two readings agree, but they differ at the
edges:

- an invalid-tag `Result` (state 0, reachable only from Go) panics in
  compiled code via `DPanic` inside the escape consumers, while the
  evaluator's `isOk`/`isErr` dispatch treats it as neither — its
  behaviour for such values is untested and likely divergent;
- the wrap-once rule is enforced structurally in compiled code
  (`Wrap` set exactly on the step's own escape), while the evaluator
  relies on convention — a step that returns `Err` holding an
  already-wrapped GoError could be wrapped twice in one
  implementation and not the other;
- error *message text* (wrap positions for nameless steps) already
  diverges — see `2026-11-02-eval-pipe-try-step-name.md`.

Because `|?>` is the main error-propagation surface, eval-vs-compiled
drift here silently undermines the interpreter-as-oracle role the
comparison harness depends on.

## Possible responses

- Re-implement eval's `|?>` on the same unwrap/escape/rewrap model so
  the two share semantics, including an invalid-tag panic case.
- Or accept value-level reading as the language semantics and relax
  the compiled invalid-tag panic — which would weaken the foreign
  value guarantee in decisions/value-representation.
- Decide explicitly which artefact is normative for `|?>` edge cases
  before P1-8 codegen relies on the compiled reading.

## Resolution

The normative spec 9.2 value semantics are preserved. Lowering now exits a
compiler-created local pipeline boundary, so a failing pipeline does not
leave the user function. Pipelines containing explicit `try` use decision
trees to preserve its user-function target. Invalid Go-side Result values
still panic. See [decision](../decisions/2026-11-03-result-pipe-local-boundary.md)
and [verification](../sketch/2026-11-03-result-pipe-local-boundary.md).

## Remaining concern

The evaluator's host-supplied `VVariant` representation has no Go-style
state tag. Malformed host values are not yet cross-checked against the
compiled invalid-value panic behavior. This concern is separate from the
now-covered value semantics and from P2-4b's optimization work.
