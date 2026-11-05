# W0501: warn on Unit-result Go calls that copy a `List` argument

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-p3-7-interop-ops-verification.md)。spec.md §8.5 へ反映済み）
- 日付: 2026-11-05

## Context

`sort.Strings xs` compiles and silently does nothing: the `List` is
copied into a `[]string` at the boundary, the copy is sorted and
thrown away ([problems](../problems/2026-11-05-go-param-mutation-noop.md)).
The signature alone cannot tell a mutating `[]T` parameter from a
read-only one, so a *sound* lint cannot exist — the question is a
heuristic with a small, explainable false-positive band.

## Decision

New lint `W0501` (category 05 Go interop, Phase lint, warning) in
the post-inference sweep beside W0701/W0702:

> For a `CallGo` application whose call result type is `Unit`, warn
> when an argument's type is the builtin `List` — the Go callee's
> `[]T` parameter is a copy, so if the callee's contract is
> in-place mutation the call has no observable effect.

- The `Unit`-result shape is what makes the FP band small enough to
  ship: every *read-only* `[]T` consumer the audit looked at
  returns a value (`strings.Join`, `sort.SearchStrings`,
  `strconv.*` helpers), so the result-type test removes the known
  false positives — including `strings.Join`, the case a naive
  "`List` argument to a Go call" lint would flag.
- It fires where `List` is the *argument* type — including
  `..xs` spreads into variadic parameters — and regardless of how
  the result is used, because a `Unit`-returning mutator only works
  through the parameter. `sort.Strings xs` warns. What silences it
  is a `Go.Slice` *argument* reaching a `[]T` slot verbatim: the
  extern-declared `Go.Slice` parameter receiving a `Go.Slice`
  value (the escape hatch), a `..gs` spread (which expands the
  slice itself), and a `...[]T` variadic element position (which
  shares backing). A `List` argument still warns even into a
  `Go.Slice`-declared parameter — that crossing copies too —
  and a `Go.Slice` argument warns when the checker materialized
  it into a `List` first (the `Slice`-marked Ascribe is the copy
  evidence; `SendSlice(ch, v)` keeps its warning).
- Limits recorded: nested `List` inside a tuple/record parameter
  also copies but does not warn (top-level only, v1); a mutator
  that returns a real result escapes the shape entirely
  (`signal.Notify` is the known false positive — the warning
  wording is conditional and true); a `..gs` spread of `Go.Slice`
  into `...any` still boxes-and-copies but stays silent — the
  verbatim `Go.Slice` shapes win over spelling out the box.
- The spec fix wording goes with it: `List` is immutable by
  contract, boundaries copy; in-place mutation needs a `Go.Slice`
  binding via `extern`.

## Alternatives

- *Known-mutating denylist* (`sort.Strings`, `sort.Float64s`, ...):
  unbounded — every mutating Go function in every package needs an
  entry — and it silently fails for the user's own Go code. The
  `Unit`-result heuristic covers the same real cases with no list
  to maintain.
- *Warn on every `List` argument to a Go call*: `strings.Join` and
  friends make it noise on day one — the false-positive that kills
  the lint.
- *Make it an error*: it is a heuristic — `signal.Notify`-style
  false positives and legitimate "mutate the copy" calls
  (a `fmt`-style debugging helper that sorts its argument for
  display) must keep compiling.
- *No lint, spec wording only*: the spec already says the boundary
  copies; the audit's point is that the surprising call *still
  looks right*. The warning is the cheap fix.
