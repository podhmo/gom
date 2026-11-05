# `for` iterates `Go.Slice a` — the checker admits what `ForList` already lowers

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-for-over-go-slice.md)。spec.md §1.1/§8.3 と SKILL.md へ反映済み）
- 日付: 2026-11-05

## Context

`for pat in iter { body }` today admits only the builtin `List` and
receivable channels; a `Go.Slice a` iterable is E0401
("a List or a channel"). Yet the rest of the pipeline was built for
the slice family from the start: `core.ForList` documents that it
"ranges over a List, Array or Go.Slice", `lower` maps any
non-channel `for` to `ForList`, and codegen emits
`for _, x := range s` — plain Go slice range, nil iterates zero
times. The open question
([open-questions](../open-questions.md): "`for` が `Go.Slice` を反復できるようにすべきか")
calls the gap "意図したが checker に届いていない".

The `Go.Slice` member surface
([decisions/go-slice-ops](2026-11-05-go-slice-ops.md)) is read-only —
a `for` loop is exactly the missing read-shaped iteration. Without
it, iterating a `Go.Slice` means `Go.Slice.fold` (a callback the
body cannot `try` through and that allocates a closure per element)
or `Go.Slice.to_list` (a full snapshot copy) — both heavier than the
loop the emitted code already spells.

## Decision

`for x in s` admits `iter : Go.Slice a`; the pattern binds `a`.
Semantics are Go's `range []T`: index order, nil iterates zero
times — matching `Go.Slice.length`/`fold`'s totality on nil.

Scope, deliberately:

- **In:** the `Con{Qual: "Go", Name: "Slice"}` spelling — what a Go
  `[]T` parameter or member result surfaces as.
- **Out, unchanged:** `Go.Map k v` (Go map iteration order is
  deliberately unspecified — admitting it would bake a
  nondeterministic order into a language whose `for` otherwise
  means ordered iteration; `Go.Map.to_list` is the documented
  "順序不定のスナップショット" surface), `Array a` (the type has no
  constructors or members yet — no value can reach a `for`), named
  Go slice types behind `*types.Go` handles (`type S []int` — needs
  an underlying-kind answer the current oracle does not shape;
  revisit if it becomes reachable), `Go.String`/`Go.Bytes` (rune
  iteration is a different element question — defer).

## Mechanism

- `checkFor` (`internal/check/expr.go`): the `*types.Con` arm
  accepts `Go.Slice a` alongside `List a` — element `a`, same
  irrefutable-pattern rule. The E0401 expected-text becomes
  "a List, a Go.Slice, or a channel".
- `evalFor` (`internal/eval/eval.go`): a `*VHost` wrapping a Go
  slice (or array — `reflect` gives both) iterates its elements,
  and `hostElem` boxes each by the pattern's static element type —
  scalars to their `Value` spelling, the rest opaque. Without
  this, eval would `CodeUnsupported` a program the compiled path
  runs, or `binMismatch` on a scalar element — an eval/compiled
  divergence.
- Lower and codegen need no change — `ForList` was already the
  lowering.

## Consequences

- spec §1.1 item 7 gains the iterable set; §8.3's `Go.Slice`
  paragraph gains the `for` line.
- The open question closes; `docs/sketch/2026-11-05-for-over-go-slice.md`
  records the eval-vs-compiled verification.
