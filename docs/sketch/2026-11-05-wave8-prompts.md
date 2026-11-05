# Wave 8 orchestration prompt drafts

State as of 2026-11-05: Wave 7 integrated — divergence (`cd89c60`),
interop-ops (`1d821bd`), gomtest (`65a5ee6`), slice-ops (`f0e97e0`),
lower-residuals (`ab946bc`), int32-eval (`8f3e018`), ledgers
(`3a8820c`, `21c84c4`, `7c8111d`, `1fc111c`, `cc51ff7`, `e2cf880`).
Round record:
[2026-11-05-wave7-round.md](2026-11-05-wave7-round.md). This file
drafts the prompts for the next wave, following
`2026-11-05-wave7-prompts.md`. Always append the "Common
instructions" block to each prompt.

**User direction for this wave: error-handling work is the
priority.**

## Orchestration notes (Wave 7 lessons)

- The 2-lane cap held; the rate-limit die-off recurred once
  (lower-residuals first worker) but **pre-incident WIP commits
  saved all work** — the resumed worker completed from worktree
  state. Keep "commit WIP early" mandatory.
- Reviewer `resume` for small nit batches works and is cheap —
  keep it for code-level fixes; orchestrator applies doc/nit-level
  fixes directly when the implementer has exited.
- Reviewers (`subagent_explore`) have no exec tool — they cannot
  run gates. The orchestrator must re-run `gofmt`/`vet`/`test` on
  the worktree before merging, even when the implementer reported
  them green.
- Merge-order conflicts on `docs/spec.md` remain the norm when two
  lanes touch different sections of the same file — resolve by
  keeping both.

## Dependencies cleared by Wave 7

- **`Ptr` ops surface shipped** — `errors.As` target slots are now
  expressible (`Ptr.new`/`get`/`set`/`is_nil`/`to_option`);
  `errors.Is` already works against error-typed package vars
  (`strconv.ErrSyntax`). Error inspection is complete; only
  **fabrication** is open.
- **Ctor-table member mechanism proven twice** (`Go.Map`,
  `Go.Slice`) — a third builtin surface (`Go`/`Error`-module
  bindings for constructors) has a known-good implementation path.
- **`Go.conv` slice copies adopted** — the copy rule is settled;
  the remaining pun on representation-equal-but-different element
  types is a recorded soundness bug, not a design question
  (reject-or-copy decision still needed).
- **Per-operation numeric semantics + width-aware eval** — the
  `Int32` divergence is closed; the adjacent "check accepts
  out-of-range int literals" gap is the recorded wall.
- **Diagnostic-display machinery exists** — `types.Display`/
  `metaNamer` (`?1, ?2` renumbering) and `nodeName`/`DiagName`
  (hidden-var names) are reusable parts for the remaining
  raw-ID/tmp-name leaks.

## Recommended Wave 8 composition — 2 parallel lanes (pool cap)

| # | Task | Main areas touched | Worktree |
|---|------|--------------------|----------|
| 1 | Error boundary: GoError fabrication + `errors` ops + spec example fix | check extern/binding path, goimport bindings, diag codes, spec fixtures, decision ×2 | gom-wt-error-boundary |
| 2 | Soundness bugs: `Go.conv` mismatched-element pun + int literal range + `checkInterfaces` Clean + W0501 inline conv | check conv/lit lint paths, cmd/gom iface, spec fixtures, decision ×2 | gom-wt-soundness-bugs |

Overlap notes:

- Lane 1 owns `internal/check` extern/binding + goimport loading +
  `Go`/`Error` builtin surface; lane 2 owns `internal/check` conv
  acceptance (`convOK`/`goConvKindOf`), `litType` range checks, and
  `cmd/gom`/`check/lint` — conv and lint files could conflict if
  lane 1 touches `check/expr.go` near `convOK`; order lane 2 first
  if both must edit the same function.
- `internal/diag/codes.go` is append-only; both lanes may add codes
  (W05xx/E05xx/E06xx) — append, never reuse; merge-order handles.
- spec.md: lane 1 edits §8.3/§8.4/§9.2 error wording, lane 2 edits
  §8.4 `Go.conv` + §3.3 literal ranges — different sections.

## Individual prompts

### 1. Error boundary — `wt/error-boundary` @ gom-wt-error-boundary

```
Error-handling boundary work — the user prioritized this area.
The fabrication half of
docs/problems/2026-11-04-goerror-construction.md is the wall;
inspection is already fixed (e.Error(), errors.Is sentinels,
errors.As via Ptr.new).

(a) [decision+impl] GoError fabrication: `errors.New`/`fmt.Errorf`
    currently return `Result Unit GoError` — the type-only default
    cannot separate "creating an error value" from "a fallible
    operation". The sketched direction (problems file): default by
    type, exceptions declared per-function — the same shape as the
    `(T, bool)` -> `Option` plan. Decide the exception form:
    candidates are a "never nil" extern return annotation (spec
    8.4 currently rejects `error` -> `GoError` with E0509 as lying
    about nil — design the syntax/decl that makes the claim
    checkable or explicitly trusted), a dedicated builtin
    (`Go.error`/`Error.new` ctor-table entry like Go.Map/Go.Slice),
    or the first entries of the standard-bindings list
    (TODO: "extern の「標準のバインディング」集の整備" — if the
    mechanism is the same, deliver errors.New / fmt.Errorf /
    errors.Join as its inaugural entries and record the home
    decision). `fmt.Errorf` is variadic `...any` — check whether
    `Go.Any` spellability blocks it (open-questions) and scope
    accordingly (errors.New alone is an acceptable v1 payload if
    variadic-any blocks the rest — record the wall).
(b) [impl] errors ops verification: prove `errors.Is`,
    `errors.As` (Ptr.new target), `errors.Unwrap`, `errors.Join`
    end-to-end from .gom — realworld-style fixtures or
    gocall/spec fixtures, both directions. Any missing surface
    (e.g. Unwrap returning error -> same fabrication class)
    recorded, not silently evaded.
(c) [doc] spec 9.2 `Result Int64 GoError` example: the type table
    says int64 -> Int but `parse_port` declares Int64 — reconcile
    (gaps ledger entry). If the example is intentionally about
    extern view matching, say so explicitly in spec wording.
(d) [decision only, stretch] `|?>` auto-wrap wording is future
    work (open-questions) but in the user's priority area: draft
    the decision (package-level format vs per-step wording;
    strconv-style double function-name; structured frame info).
    Prototype only if trivial — the deliverable is the adopted or
    recorded-as-rejected decision, not the feature.
Each decision: draft -> prototype -> sketch verification -> status
update. This lane may legitimately end with (a) decided-but-
partially-implemented if variadic-any or extern-form design is the
wall — record the wall, do not bypass it.
```

### 2. Soundness bugs — `wt/soundness-bugs` @ gom-wt-soundness-bugs

```
Four recorded bugs; (a) and (b) each need a draft decision first.
TODO.md Bugs section is authoritative.
(a) [decision+impl] `Go.conv` `Go.Slice T <-> List U` mismatched
    elements still pun: `Go.Slice Int64 -> List Int` etc. escape
    the types.Equal gate and cast — mutation aliases across the
    boundary (same class the copy rule just fixed). Decide:
    reject non-equal element pairs at convOK (sound, minimal) vs
    copy element-wise for representation-equal pairs (permissive).
    Implement the decision + spec fixtures + gaps close-out.
(b) [decision+impl] check performs no int literal range check:
    `let n : Int32 = 3000000000` compiles, emitted Go cannot
    build; `0 - 2147483648` hits the same hole via
    litFoldDiverges. Decide: reject out-of-range literals at check
    (Go's rule) vs wrap-materialize (spec 3.3 wrap semantics
    extended to literals). Note spec is silent — the decision
    adds the wording. problems:
    docs/problems/2026-11-05-int-literal-range-check.md.
(c) [impl] `checkInterfaces` groups by filepath.Dir without
    Clean: `gom test pkg/../pkg/a.gom pkg/b.gom` splits one
    physical dir into two groups (check/build/test all paths).
    Fix: Clean the grouping key; add a driver test.
(d) [impl] W0501 misses inline `Go.conv e : Go.Slice T` passed
    to a mutating Go call — the Ascribe has Conv:true Slice:false
    so the materialized check misses it. Extend the lint to this
    shape; keep verbatim-slice silence (the materialized flag
    rules still apply).
Small items; one commit per item is fine.
```

## Sequential tail (when a lane frees, or Wave 9 lead)

```
- Diagnostic-display batch: E0904 `through`/`path` leaks hidden
  tmpN VarDecl names (reuse E0907's nodeName/DiagName);
  E0407/E0409/E0801 raw meta IDs (reuse metaNamer per diagnostic).
  Both mechanisms already exist — small, safe.
- Registered fuzz bugs: labeled partial-application let-bind E0901
  (issue #11, knownCompileBugs); `gom fmt` selector non-idempotency
  (issue #12, knownFormatBugs).
- gomtest multi-file spec harness (fixture format is itself a
  small decision; enables spec-level regression for package-unit
  rules like cross-sibling E0907/E0904).
- `for` over `Go.Slice` — checker rejects what Core IR can
  express; decide then implement (small either way).
- Standard-bindings generalization: whatever lane 1 leaves —
  home decision + `(T, bool)` -> `Option` entry list.
- boundary-aliasing H1-H7 fixtures (TODO, sketch exists).
```

## Items needing decisions, not implementation

- `|?>` auto-wrap wording — lane 1 (d) drafts it; implementation
  follows in a later wave if adopted.
- `for` over `Go.Slice` (open-questions — sequential-tail item).
- `*T` default — reduced to automatic result/param mapping only;
  may settle inside a future standard-bindings lane.
- `Go.Any`/`any` spellable in signatures — may block lane 1 (a)
  for `fmt.Errorf`; if so it becomes its own decision.
- Structural Go constraints (`~[]E`/`~map`), Go struct
  construction/field writes, external-module stdlib imports,
  multi-file eval, 3+-result fns, `rune`->`Int32` vs `Char`,
  spec-9.2-vs-8.3 example reconciliation is lane-1 (c)'s scope.
- `Result.to_option`/`Option.to_list` — structurally blocked by
  Go import cycles; stays recorded.

## Wave 9 candidates

- Standard-bindings list implementation (post lane-1 decision).
- `|?>` wrap wording implementation (if adopted).
- Multi-file eval; `Go.Any` spellability.
- boundary-aliasing contract -> spec 8.7 wording or decision.
- P4-6 LSP, P4-7 incremental cache.
- Emacs gom-mode tree-sitter port; gist-cli byte I/O.
- D-3 README final adequacy, D-4 tutorial go.mod — still
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
