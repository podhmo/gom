# Wave 9 orchestration prompt drafts

State as of 2026-11-05: Wave 8 integrated — error-boundary
(`4aac570`), soundness-bugs (`5018a46`), diag-fuzz (`47401ee`),
ledgers (`2dc1d23`, `534b540`), boundary-aliasing (pending — see
round record). Round record:
[2026-11-05-wave8-round.md](2026-11-05-wave8-round.md). This file
drafts the prompts for the next wave, following
`2026-11-05-wave8-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Orchestration notes (Wave 8 lessons)

- Salvage worked twice: a dead worker's *uncommitted* WIP is
  recoverable — orchestrator can commit it directly in the worktree
  (lane 1's draft decision + core node survived a rate-limit death
  this way). "Commit WIP early" stays mandatory; add "orchestrator
  commits stranded WIP" to the incident playbook.
- Resumed-agent fix rounds are the cheap path for review findings;
  two resume rounds per lane max before the orchestrator applies
  nits directly.
- Reviewers catch *recorded-mechanism* errors, not just code errors
  — the `.gom` group-split entry was a misdiagnosis a fresh reader
  caught by checking `filepath.Dir` semantics. When a bug record
  names a mechanism, the reviewer should verify it, not just the fix.
- Pool pressure recurred: two org-wide die-offs in one wave. Keep
  the 2-lane cap; a third sequential lane launched only after a merge
  freed a slot worked fine.

## Dependencies cleared by Wave 8

- **Error fabrication is complete**: lone `error` results admit
  `Result Unit GoError` / `GoError` (never-nil, boundary-checked) /
  `Option GoError` views; `internal/goimport/stdbind.go` ships
  `errors.New`/`fmt.Errorf`/`Join`/`Unwrap`; `rt.StepError` gives
  `|?>` a structured frame with working `Unwrap`. What remains is
  the `(T, error)`-position family — the `(error, error)` first-slot
  hole and the `errors.As` pointer contract.
- **Standard-bindings mechanism exists** — a third surface
  (`(T,bool)`-shaped declared views, `*T`-default mapping) now has a
  curated home + the extern-priority rule settled.
- **Literal-range machinery exists** — `pendingLit`/`revisitLitRanges`
  + `constant.ToInt` judgment; the named-target residual needs only
  an oracle underlying-kind query.
- **`CallInfo.ArgToParam` is consumed everywhere** — the
  reclassifyCalls arity-cap residual is now the only consumer that
  can still nil out.
- **`selectEscape`/`parenLed` machinery exists** for formatter
  selector-escape classes.

## Recommended Wave 9 composition — 2 parallel lanes (pool cap)

| # | Task | Main areas touched | Worktree |
|---|------|--------------------|----------|
| 1 | Error boundary tail: `(error,error)` Ok-side hole + errors.As contract + CLI-E0509 scope check | goimport mapping/stdbind, emit resultFromGo, gomimport Loader, decision ×2 | gom-wt-error-tail |
| 2 | Small-bugs batch: named-target conv lit range + E0701 + for-over-Go.Slice + reclassifyCalls repro | check litRange/lint/for, goimport oracle, reclassify | gom-wt-small-bugs |

Overlap notes:

- Lane 1 owns `internal/goimport` mapping/stdbind +
  `resultFromGo` emission; lane 2 owns `internal/check` litRange +
  `for`/`reclassify`. The goimport Loader gets touched by both (lane 1
  ExternSource for CLI; lane 2 underlying-kind query) — different
  methods, merge-order handles.
- `internal/diag/codes.go` append-only both lanes (E05xx/W05xx).
- spec.md: lane 1 §8.3/§8.4 result-views + errors.As wording, lane 2
  §3.3 literal tail + `for` iteration wording — different sections.

## Individual prompts

### 1. Error boundary tail — `wt/error-tail` @ gom-wt-error-tail

```
Three residuals from the wave8 error-boundary review — the user
prioritizes this area.
(a) [decision+impl] `(error, error)`-style results let a first-position
    nil error ride into the `Ok` payload unchecked (`Result GoError
    GoError` → `e.Error()` panics on nil interface). The lone-error
    decision explicitly scoped `(T, error)` out. Decide: extend the
    declared views to the Ok-side payload (`Option GoError` in slot 1),
    or boundary-nil-check the first slot like ErrorFromGo, or document
    the Go-side contract. TODO Bugs + gaps entry are authoritative.
(b) [decision] `errors.As`-family "any that must be a non-nil pointer"
    contract: `errors.As(e, p)` compiles with a non-pointer or nil
    target → Go panics. Decide: a typed standard-binding view
    (`errors.As : GoError -> Ptr T -> Bool` — needs generic-instantiated
    stdbind entries, a mechanism extension), a W-lint on the
    any-position pointer requirement, or status quo. Decision is the
    deliverable; implement only if the chosen form is small.
(c) [scope check → maybe impl] CLI-E0509 gap: `gom check`/`gom build`
    pass `gomimport.Loader` which doesn't implement
    `check.ExternSource` — extern decls are never verified and, post
    wave8, declared views shape codegen so arity drift emits broken
    Go. First measure the plumbing cost of implementing
    ExternSource on gomimport.Loader (it already loads types for
    import resolution — check what the interface needs); if it's a
    small adapter, implement + driver tests; if it's a loader-fork
    risk, record the design question in open-questions and stop.
    gaps entry updated in wave8 — read it first.
Each decision: draft -> prototype -> sketch verification -> status.
```

### 2. Small-bugs batch — `wt/small-bugs` @ gom-wt-small-bugs

```
Four small items; one commit per item.
(a) [impl] `Go.conv` literal operand at named Go target skips range
    judgment (TODO bug + gaps): `Go.conv 5000000000 : os.FileMode`,
    `Go.conv 1.5 : time.Duration` accepted → emitted Go can't build.
    The gaps entry prescribes: goimport.Loader underlying-kind query
    (litRange consults the oracle for the named type's underlying
    kind, then reuses the builtin judgment) OR lowering-side typed
    escape. Prefer the oracle query — it keeps the judgment in check.
    Fixtures for FileMode + Duration.
(b) [impl] E0701 Meta arm leaks raw `?n` (TODO task): same fix shape
    as the wave8 E0407/9/E0801 batch — fresh metaNamer +
    types.Display at the E0701 report site (check/expr.go ~1880).
    Fixture if observable.
(c) [decision+impl] `for` over `Go.Slice` (open-questions): checker
    rejects (E0401 — List/channel only) but Core ForList already
    expresses it and `range` over []T works in emitted Go. Decide:
    admit (then it's a checker relaxation + spec wording + fixtures)
    or keep rejecting (then it's a wording/decision record). Either
    is a real outcome — do not leave undecided.
(d) [repro → maybe impl] `reclassifyCalls` nils `ArgToParam` past
    declared arity (TODO [bug?], unconfirmed): let-bound lambda
    promoted to KindFunc, labeled fill beyond flattened arity →
    mapping nils → positional fallthrough while eval uses
    f.Remaining() mapping → possible divergence. FIRST write a
    reproducing .gom and observe eval-vs-compiled; if confirmed
    divergence, decide reject-at-check vs correct reclassification;
    if the case is unreachable, record why and close the entry.
```

## Sequential tail (when a lane frees, or Wave 10 lead)

```
- gomtest multi-file spec harness (fixture format is itself a small
  decision; enables package-unit spec regression).
- boundary-aliasing contract -> spec 8.7 wording or decision
  (depends on the wave8 verification lane's findings).
- `*T` default (auto result/param mapping) — may settle inside a
  stdbind-extension lane.
- Emacs gom-mode tree-sitter port; gist-cli byte I/O residual.
- P4-6 LSP, P4-7 incremental cache — larger slots.
```

## Items needing decisions, not implementation

- `Go.Any`/`any` spellable in signatures (wave8 showed variadic-any
  works via derived params; the spelling question stands alone now).
- Multi-file eval (eval.New single-File input).
- Structural Go constraints (`~[]E`/`~map`), Go struct
  construction/field writes (boundary-aliasing findings feed this),
  external-module stdlib imports, 3+-result fns, `rune`->`Int32` vs
  `Char`.
- `|?>` wrap-wording configurability — future work, decision
  adopted for the frame; custom format syntax remains.
- `Result.to_option`/`Option.to_list` — structurally blocked by Go
  import cycles; stays recorded.

## Wave 10 candidates

- P4-6 LSP (SourceMap-aware inverse mapping), P4-7 incremental
  cache.
- Multi-file eval implementation; `Go.Any` spellability impl.
- boundary-aliasing contract wording (post-decision).
- `errors.As` typed view impl (if the wave9 decision adopts one).
- D-3 README final adequacy, D-4 tutorial go.mod layout — still
  end-of-project per user direction.

## Common instructions (append to every prompt)

```
Before finishing run: gofmt -l . (no output), go vet ./...,
go test ./... Commit WIP early and often — worktree commits are
the only recovery point if the session dies. Decisions made
during implementation go to docs/decisions as 状態: draft; verify
with a prototype, write results to docs/sketch, then update
status. Language-rule changes add testdata/spec fixtures,
generated-code changes update testdata/golden, Go-interop changes
add tests in both directions. Concerns go to docs/problems;
spec/implementation/doc discrepancies are recorded in
docs/implementation-gaps.md, never resolved by silently rewriting
the spec. Do not edit TODO.md, docs/open-questions.md,
docs/implementation-gaps.md, or TODO-archive.md — report proposed
ledger changes in your completion report and the orchestrator
will apply them. Read skills/gom-agent-guide before writing
.gom/.gomi files.
```
