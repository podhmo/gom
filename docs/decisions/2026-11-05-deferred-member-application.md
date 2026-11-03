# Deferred member application: `x.M(args)` on an unresolved receiver

- 状態: 採用
- 日付: 2026-11-05
- 検証: [sketch/2026-11-05-deferred-member-application.md](../sketch/2026-11-05-deferred-member-application.md)

## Background

`x.M(args)` cannot pick a calling convention while the receiver's
type is still a meta — a record field means curried Gom application,
a Go method means the tuple convention of spec 8.1 — and the two
conventions disagree about everything downstream `CallInfo`
describes: argument unpacking, `_` placeholders, `..e` spreads, and
labels. The checker's selection table already deferred the *member
lookup* itself (pendingSels, revisitSelects); P3-4 needed the
application sitting on top of the member to defer with it. The
motivating case is `let later = fn x -> x.AddDate(_, 2, 3)`, where
the `_` is legal only if `.AddDate` turns out to be a Go method —
which only `later t0` decides.

## Decision

An application whose callee is a member selection pending on an
unresolved receiver meta is itself deferred (pendingApplies):

- `checkDeferredApply` checks arguments *provisionally*: labels are
  kept, `_` mints a `Hole` node without E0504, `..e` mints a `Spread`
  without E0505, and the surface argument tuple keeps its elements'
  markers so the Go replay can unpack it the way the eager path
  unpacks surface syntax. Everything else (ordinary expressions)
  goes through `checkExpr` normally — provisional markers occur only
  as direct arguments or inside that argument tuple.
- The provisional node carries `CallInfo{Kind: CallUnknown}` — a
  truthful "not yet known" record walkers may inspect — until the
  replay replaces it.
- `revisitSelects` replays each pending apply as soon as its member
  resolves, in pendingSels order — and loops to a fixpoint, because
  a replay can itself bind a receiver an *earlier* pending member
  waits on (`g (x.M(1))` where `g`'s own body selects `r.N`). A
  member never resolved by the end of `finalize` replays under the
  Gom convention: the withheld diagnostics still fire and the result
  meta joins the ordinary E0407 reporting.
- The deferred apply's *result* meta defers interface conformance
  the way a pendingSels member meta does (`deferInterfaceSource`):
  `let s : fmt.Stringer = x.M(1, 0, 0)` inside the unannotated
  lambda must route through pendingIfaces, or the provisional want
  binds the meta before the replay knows the real result type.
- The replay consumes the recorded markers (`pa.useMarker`) and
  `flagStrayMarkers` reports every provisional marker that did not
  reach a legal position — a `_` under the Gom convention, or either
  marker nested below the Go argument list's own level. The marker
  node stays in the tree (the same recovery shape the eager pass
  leaves — a quiet meta covering an already-reported diagnostic).
- Generalization treats the metas an open pending apply pins —
  receiver, member standing type, result, provisional arguments — as
  shared: the binding stays monomorphic so a later use binds the
  receiver concretely. This is the same reason `0.f` waits.

## Consequences

- `fn x -> x.AddDate(_, 2, 3)` compiles to the same closure the
  eager path emits for an annotated receiver: non-hole arguments are
  bound to temporaries at lambda creation (cute semantics), the hole
  remains the one parameter.
- Convention diagnostics are not reorderable against the convention
  decision by construction: E0504/E0505/E0404–E0408 are emitted by
  whichever replay the resolved member picks, never by the
  provisional pass.
- A member that resolves to a non-function (a field `n: Int`)
  replays under Gom and takes the ordinary curried-over-application
  diagnostics — no phantom method classification.
- Boundary: a receiver that stays a meta through finalize gets the
  Gom replay and the ordinary unresolved-parameter diagnostics; no
  method classification is invented. Making `x.M` itself
  polymorphic-in-the-receiver is the typeclass question
  (open-questions), not this mechanism.
- Known divergence: a consumer can still bind the standing result
  meta through the *callee* unification — `f "a"` where
  `f = x.Print(_, ..["b"])` binds `String -> ?ret` before the replay
  produces `any -> Unit`, and unify's `Go.Any` acceptance is
  want-side only. Recorded as a spec question in
  [problems/2026-11-05-any-hole-deferred-result](../problems/2026-11-05-any-hole-deferred-result.md).

## Alternatives considered

- Rechecking the surface arguments at replay time. Rejected: the
  provisional pass already ran each argument's own inference (a `42`
  defaulted, labels were recorded); re-checking would either
  duplicate diagnostics or need a quiet-mode checker.
- Deciding eagerly from the unresolved member's *name* (`AddDate`
  sounds like a method). Rejected: the CallInfo contract exists
  precisely so names never drive calling-convention decisions.
