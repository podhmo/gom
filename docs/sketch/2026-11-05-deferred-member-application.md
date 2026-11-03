# Deferred member application: verification record

- 日付: 2026-11-05
- 状態: verified; decision adopted

Verification for
[the deferred member application decision](../decisions/2026-11-05-deferred-member-application.md)
(P3-4): Go method calls `x.M(args)`, the `_` cute placeholder on a
receiver whose type binds late, and the convention diagnostics the
provisional pass withholds.

## Mechanism under test

`checkApply` now defers when the callee is a `MemberExpr` whose
receiver type is a meta and the selector resolved to no symbol:
`checkDeferredApply` checks each argument provisionally
(`provisionalArg` — `_`/`..e` kept as typed markers, the surface
argument tuple's elements checked the same way, everything else via
`checkExpr`), records a `pendingApply`, and `revisitSelects` replays
it (`replayGoApply`/`replayGomApply`) the moment the member resolves,
or `finalize` replays under Gom when it never does.
`flagStrayMarkers` then reports the recorded markers the settled
convention did not consume. `generalize` pins the metas an open
pending apply still needs, so `let later = fn x -> x.AddDate(_, 2,
3)` stays monomorphic until `later t0` binds `x = time.Time`.

## Evidence

Checker unit tests (`internal/check/check_deferred_test.go`, a stub
MethodSource answering for one Go named type):

- `fn x -> x.M(1)` applied to the stubbed receiver replays as
  `CallGo` with the method symbol as callee (TestDeferredMethodCall).
- `fn x -> x.M(_, 3)` keeps the `_` provisionally and replays with
  `HoleIndex 0`; the node's type is `String -> Unit`
  (TestDeferredMethodHole).
- `x.M(("s", 3))` — the single tuple argument — unpacks to two Go
  arguments at replay, like the eager path (TestDeferredMethodTupleArgs).
- `fn r -> r.n(_)` where `r` binds to a record: field resolution,
  Gom replay, `E0504` + the curried-over-application diagnostics —
  same emission order as the eager `os.Args _` case
  (TestDeferredHoleOnGomField).
- A receiver that never binds still reports the withheld `E0504`
  plus the unresolved-parameter `E0407` (TestDeferredUnresolvedReceiver).
- `x.M((_, 1))` — a hole nested inside the argument tuple — reports
  `E0504` after the method resolves: the provisional TupleLit no
  longer smuggles markers past the convention check
  (TestDeferredNestedHole, fixed in review: `flagStrayMarkers`).
- `x.NoSuch` on the stubbed receiver reports the ordinary
  E0401 (`TestDeferredNoSuchMethod`).
- `let s : fmt.Stringer = x.M()` inside the unannotated lambda
  compiles clean: the deferred apply's result meta defers interface
  conformance (`deferInterfaceSource`), so the provisional want
  routes through pendingIfaces and `revisitInterfaces` accepts once
  the replay binds the real result — review fix
  (TestDeferredApplyResultInterface; end-to-end `gom check` on
  `time.Time`/`fmt.Stringer` verified).
- `let g = fn r -> r.N()` applied as `g (x.M(1))`: the member
  entered *before* the apply that binds its receiver resolves on the
  revisitSelects fixpoint — review fix
  (TestDeferredMemberFedByLaterApply; end-to-end `time.Time.Year`
  verified).
- Convention details the provisional pass now provably keeps: a
  second `_` reports E0504 (TestDeferredDoubleHole), `..xs` replays
  as a legal variadic spread (TestDeferredSpread), and labeled
  arguments map back to declaration order through ArgToParam
  (TestDeferredLabeledArgs).

Spec fixtures (`testdata/spec/`):

- `ok_go_method_cute.gom` — the accepted delayed-placeholder case,
  `{"diagnostics": []}`.
- `E0504_hole_in_go_arg_tuple.gom` — arity E0401, element E0401, and
  the nested-hole E0504 in the deferred replay.
- `E0504_delayed_hole_on_field.gom` — E0401/E0407/E0504 for the
  field-resolved member.

Golden (`testdata/golden/go_method_cute.gom` → `.golden.go`):
`later` lowers to `func(x time.Time) func(int64) time.Time` with
`tmp4 := int(2); tmp5 := int(3)` evaluated *outside* the returned
closure and `x.AddDate(tmp7, tmp4, tmp5)` inside — the cute capture
exactly as spec 8.2 describes; the direct `t0.AddDate(5, 2, 3)`
emits the same temporary conversion shape.

Evaluator (`internal/eval/eval_test.go` `TestGoMethods`): bound
methods prepend the receiver to the host call, `x.M(_, 7)` produces
a one-argument `fnGoPartial`, the missing-stub case reports
`CodeUnsupported`, and the evaluate-once contract is pinned — a
counted `tap` argument is evaluated once at partial creation while
the method is invoked per application (cute arguments evaluate
once).

Eval-vs-compiled (`internal/build/compare_test.go` case
`go_method_cute`): `let later = fn x -> x.AddDate(_, 2, 3)` plus
`(t0.AddDate (1, 0, 0)).Format "2006-01-02"` prints
`1975-03-04/1971-01-01` under both the interpreter (VHost receiver +
host stubs keyed `time.Time.AddDate`/`time.Time.Format`) and the
generated binary.

## Findings and fixes folded in

- Provisional TupleLit elements could smuggle markers: only
  TupleLit arguments kept their markers provisional, every other
  surface form reported E0504/E0505 eagerly — fixed by recording
  markers on the `pendingApply` and `flagStrayMarkers` in both
  replays (spec fixture above).
- The `r.n(_)` replay used to leave the placeholder's meta unsolved
  (extra E0407 noise): `markQuiet` on replaced/stray holes, matching
  the eager path's quiet recovery meta.
- `x.M(1).N(2)` chained method calls parse `.N` against the
  *argument tuple*, not the call result — a pre-existing parser
  precedence issue, unrelated to the deferred mechanism; explicit
  parentheses work
  ([problem](../problems/2026-11-05-method-chain-select-binds-args.md)).

## Remaining boundary

- A receiver that stays unresolved through finalize replays under
  Gom and reports E0407/E0401 — the mechanism decides convention,
  not polymorphic method sets (open-questions typeclass territory).
- A consumer can still bind the standing result meta through the
  callee unification before the replay runs: `f = x.Print(_,
  ..["b"])` then `f "a"` diverges from the eager path (`expected
  String, got Go.Any`) — recorded as a spec question in
  [problems/2026-11-05-any-hole-deferred-result](../problems/2026-11-05-any-hole-deferred-result.md).
- The eval side now honors `CallInfo.ArgToParam` for CallGo
  (`declOrderVals`, mirroring lowering's `declOrderArgs`) — labeled
  Go-call arguments reach the host stub in declaration order, and
  the cute path keeps the hole's declaration slot. Eval still cannot
  evaluate a `..e` spread *inside* a Go call (`*check.Spread` is
  only handled in list literals) — pre-existing limitation, noted
  for the eval-support ledger.
- Eval requires host stubs for every method symbol the program
  references; unstubbed methods report `CodeUnsupported` by design —
  the differential harness only covers audited, deterministic
  members (`time.Unix`, `time.Time.AddDate`, `time.Time.Format`,
  `strings.Builder.WriteString`, `builtin.Error`).
