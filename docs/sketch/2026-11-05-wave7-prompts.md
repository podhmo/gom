# Wave 7 orchestration prompt drafts

State as of 2026-11-05: Wave 6 integrated — initorder-small
(`5a2b0c7`, ledger+spec `9965701`), P5-2 lint (`c919cfb`), P3-7
realworld (`c083fba`), P2-4c + P5-3 (`778ddca`), ledgers
(`afea1d0`, `a3569ea`), TODO GC (`d0fdf4c`). Round record:
[2026-11-05-wave6-round.md](2026-11-05-wave6-round.md). This file
drafts the prompts for the next wave, following
`2026-11-05-wave6-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Orchestration notes (Wave 6 lessons)

- The free-plan rate pool is org/plan-wide, not per-model. Four
  parallel `subagent_general` workers exhausted it; three died
  **without notification** and their transcripts are unrecoverable
  (`read_subagent`/`resume` → "No subagent found"). Run **at most
  two implementation lanes concurrently**; queue the rest.
- `subagent_explore` reviewers hit the same pool but stay preferred
  for reviews (read-only; keeps write-capable slots free).
- "Commit WIP early and often" is now in the common block —
  worktree commits are the only recovery point.
- `.devin/agents/nanny.md` (timer-only watchdog) exists but was
  never exercised; custom profiles created mid-session are not
  registered, and a `model:` pin is rejected by the router anyway.

## Dependencies cleared by Wave 6

- **P3-7 audit delivered the boundary map**
  ([sketch](2026-11-05-p3-7-realworld-interop-audit.md)): `Ptr`
  read-through only — no deref/nil-compare/construct, `errors.As`
  compiles then panics, `Option (Ptr T)` extern is the only
  nil-safe read; `Go.Map` has no ops (`Go.Map.make` → E0201);
  `sort.Strings` silently no-ops; `Go.Any` unspellable hurt
  `json.Decoder.Token`; 3+-result fns hidden. Feeds lanes 1 and
  the standard-bindings task.
- **Package-init semantics adopted** (spec §5.1, package-wide
  E0907 in `lower.CheckInitCycles`) — the `@tailrec` R4
  sibling-file gap now has a known-good fix pattern.
- **W0701/W0702 lint mechanism exists** — new advisory warnings
  (e.g. the `[]T` in-place mutation question) have a home in the
  check lint sweep.
- **Benchmark harness adopted** — perf numbers reproducible via
  `stdlib/<pkg>/bench_test.go`; specialization-policy residuals
  are recorded in open-questions 最適化.

## Recommended Wave 7 composition — 2 parallel lanes (pool cap)

| # | Task | Main areas touched | Worktree |
|---|------|--------------------|----------|
| 1 | Interop ops surface: `Ptr` ops + `Go.Map` ops + `[]T` in-place mutation | check/resolve/goimport member tables, diag codes, spec fixtures, draft decisions | gom-wt-interop-ops |
| 2 | Divergence semantics: Float const-fold + wrong-variant `.f` | spec wording + decisions ×2, eval, codegen, spec fixtures | gom-wt-divergence |

Overlap notes:

- Lane 1 owns check/resolve/goimport + W07xx-style warnings;
  lane 2 owns eval/codegen semantics + spec wording — mostly
  disjoint; both may add testdata/spec fixtures under different
  filenames (merge-order handles).
- spec.md: lane 1 adds ops-surface/boundary wording, lane 2 edits
  Float arithmetic and `.f` semantics — different sections.
- `internal/diag/codes.go` is append-only; only lane 1 is likely
  to add codes (W07xx).

## Individual prompts

### 1. Interop ops surface — `wt/interop-ops` @ gom-wt-interop-ops

```
Three related boundary-ops tasks from the P3-7 audit
(docs/sketch/2026-11-05-p3-7-realworld-interop-audit.md), each
starting as a draft decision. TODO.md's P3 section is
authoritative.
(a) [decision+impl] Ptr ops surface: `Ptr T` supports
    read-through (method/field) but nothing else — no deref, no
    nil compare, no construction; `errors.As` compiles then
    panics; `Option (Ptr T)` extern is the only nil-safe read.
    Decide the ops surface: a `Go.deref`-style member table?
    `Ptr.is_nil`? A deref returning `Option T`? How does it
    interact with the `*T`-default open-question — settle that
    too if the design forces it, else keep it open.
    problems: docs/problems/2026-11-05-ptr-t-no-deref.md.
(b) [decision+impl] Go.Map ops: `Go.Map.make` is E0201 —
    builtinGoMembers names only the conv/Slice/Map type
    constructors. Pick the minimal consistent op set
    (make/get/set/del/len; iteration via a List of pairs is
    acceptable for v1) and the mechanism (compiler member table
    like Chan/Task vs a Gom-side module). Spec fixtures in both
    directions where semantics are involved.
(c) [decision+impl maybe] `[]T` in-place mutation:
    `sort.Strings` compiles into a silent no-op because
    List→[]T converts by copy. Decide: a lint warning (what
    heuristic is defensible — the signature alone cannot see
    intent; strings.Join is the false positive to beat), a
    known-mutating denylist, or spec wording making the
    `Go.Slice` raw-pun escape hatch explicit. A spec-wording-only
    resolution is acceptable if a lint cannot be made sound —
    record the failed approach in docs/problems.
    problems: docs/problems/2026-11-05-go-param-mutation-noop.md.
Each decision: draft → prototype → sketch verification → status
update. Tests in both directions where semantics change.
```

### 2. Divergence semantics — `wt/divergence` @ gom-wt-divergence

```
Two knownDivergences need spec semantics decisions then
implementation — these are the hard walls; decide, do not evade.
(a) Float constant folding (TODO bug, problems
    docs/problems/2026-11-02-float-const-fold-arithmetic.md):
    eval computes `869.32 * 38.29` = 33286.262800000004 (float64
    per literal then multiply); generated Go emits the untyped
    constant expr → arbitrary-precision fold + single round =
    33286.2628. Decide Float arithmetic semantics: IEEE754-based
    (codegen materializes operands as non-constants) vs Go
    constant semantics (eval folds in codegen order) vs a third
    option. Prototype, verify eval and compiled paths agree,
    update spec wording.
(b) wrong-variant `.f` (TODO bug, problems
    docs/problems/2026-11-02-variant-field-wrong-variant.md):
    `s.r` on a Rect returns slot-0's w in eval but panics on the
    type assertion in compiled code. check accepts `.f` only when
    exactly one variant owns the field, so the static owner is
    uncertain. Natural candidate: assertion semantics (mismatch =
    panic, consistent with match's DPanic family) — spec wording
    + eval ctor check to match. Decide, implement, fixtures.
Both items live in knownDivergences in the eval-vs-compiled
comparison harness — remove entries as they resolve.
```

## Sequential tail (when a lane frees, or Wave 8 lead)

```
- Diagnostics/lowering residuals batch: `@tailrec` R4
  package-wide (same shape as CheckInitCycles — the gaps ledger
  entry describes the fix); nested `go` duplicate W0702 (attach
  boundary-identifying data or dedupe); unguarded/guarded
  EscapeCall span asymmetry (//line only); E0401 `?n` meta
  spelling (display rendering of unresolved metas).
- Test/tooling infra: gomtest multi-file spec harness (the
  fixture format is itself a small decision); `gom test` flag
  passthrough (`-race` verification is manual today).
- Standard bindings: the P3-7 audit is the input — draft the
  "where managed + which entries" decision then implement the
  extern override list.
- Gist CLI `[-]` continuation: byte I/O + `Go.Slice String` →
  `List String` — may depend on lane 1's ops surface.
```

## Items needing decisions, not implementation

- Standard-bindings home + list content (audit input now exists).
- Multi-file eval (`eval.New` takes a single `check.File`).
- `Go.Any`/`any` spellable in signatures.
- `Go.conv` `Slice`↔`List` raw pun — spec wording or align to
  copy.
- 3+-result Go functions hidden while spec states no arity cap —
  spec wording or N-ary support.
- `rune`→`Int32` vs `Char` table drift — fix spec or the
  `basicType` mapping.
- Structural Go constraints (`~[]E`/`~map`), `*T` default (may be
  settled inside lane 1), `.gomi` enforcement beyond build/check,
  `checkInterfaces` asymmetry, select arm priority,
  specialization policy (open-questions 最適化 section).
- Channel send-value restrictions — future work, already listed.

## Wave 8 candidates

- P4-6 LSP (SourceMap-aware inverse mapping), P4-7 incremental
  cache.
- Multi-file eval implementation (after its decision);
  specialization implementation (needs the buffer-path decision);
  standard-bindings implementation (if only decided in 7).
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
