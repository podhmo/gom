# Wave 10 orchestration prompt drafts

State as of 2026-11-05: Wave 9 integrated — error-tail (`edd65bc`),
small-bugs (`d0a4e18`), gomtest-multifile (`157ea01`),
boundary-contract (`ddafebc`), ledgers (`514ab04`, `ba04918`,
`03013bc`). Round record:
[2026-11-05-wave9-round.md](2026-11-05-wave9-round.md). This file
drafts the prompts for the next wave, following
`2026-11-05-wave9-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Orchestration notes (Wave 9 lessons)

- Fresh-eyes review caught a *recorded-mechanism* error for the second
  wave running: the `ArgToParam` guard was documented "unreachable
  dead defense" but is reachable and load-bearing (prevents a
  `CallInfo.Validate` panic). Conclusions can stay right while proofs
  are wrong — reviewers must verify named mechanisms, and sketch
  corrections are a normal fix-round deliverable.
- Rate-limit death recurred once. Salvage path is proven twice now:
  orchestrator commits uncommitted WIP (`1fbb4a5`), then **resume the
  same agent after the reset window** — resume preserved the
  transcript and finished cleanly; fresh respawn was unnecessary.
  Playbook updated: WIP commit → wait for reset → resume first,
  fresh-spawn only if resume fails.
- One worker edited the forbidden ledger files
  (open-questions/gaps/TODO) despite the rule — content was correct
  and landed via squash. Keep the rule; the orchestrator ledger pass
  still runs and reconciles.
- Merge conflicts stay trivial when prompts state ownership
  boundaries explicitly (only hit: two `Config.Sigs` forwarding
  methods at one spot in `gomimport/loader.go`).
- Docs-only lanes work well: the boundary-contract lane shipped a
  decision + spec wording with zero compiler changes — review still
  caught an under-complete enumeration.

## Dependencies cleared by Wave 9

- **Error-boundary result views are complete for `(T, error)` and
  `(error, error)`** — derived Option-Ok views, `NilGuard`,
  `ConvOptNil`, and spec §8.4's admission table all exist. Residual:
  named-interface/`any` first results (`nilablePresence` limited to
  `*T`/universe `error`).
- **CLI runs extern checking** (`gomimport.Loader.BindExtern` →
  `Config.Sigs` `ExternSource`) — arity/view drift now fails `gom
  check`/`build`/`test`, so new admitted shapes are enforced
  end-to-end.
- **Underlying-kind oracle exists** (`check.UnderlyingSource`,
  `goimport.Loader.GoUnderlyingKind`) — named-Go-type basic-kind
  queries are a one-method call for check-side judgments.
- **`for` iterates `Go.Slice`** — eval `hostElem` unboxing exists for
  host-typed iterables.
- **gomtest directory cases exist** — package-unit rules are spec
  fixtures now (`DiagnosticsFunc func([]File)`); new harness surface
  for any suite-side work.
- **Boundary mutation contract adopted** — spec §8.5 scope + §8.7
  contract settled; wording-level follow-ups only.
- **`ArgToParam` machinery understood** — wave9 sketch documents the
  exact reachability + guard contract (`check/reclassify.go`,
  `apply.go` `overDeclared`); the labeled-args bug below stands on it.

## Recommended Wave 10 composition — 4 lanes

The wave defines 4 lanes. **Execution under Devin: run at most 2
implementation lanes concurrently** (rate-limit history — waves 8–9
hit org-pool exhaustion twice); lanes 3–4 are queued and launch as
slots free, in table order. An external runner without the pool cap
may run all 4 in parallel — the overlap notes below keep even that
case mergeable.

| # | Task | Main areas touched | Worktree | Under-Devin order |
|---|------|--------------------|----------|-------------------|
| 1 | Call honesty: labeled-arg-into-returned-param bug + `(NamedIface, error)`/`(any, error)` Option view | check/apply.go, reclassify, goimport extern admission, spec §3.x + §8.4 | gom-wt-call-honesty | immediate |
| 2 | Hygiene batch: E0402/E0403/E0410/E0411 meta renumber + NilGuard/ConvOptNil lower tests + spec-root bare side-file orphan | check/expr.go diag sites, lower tests, gomtest | gom-wt-hygiene | immediate |
| 3 | gomtest residuals: golden directory cases + sibling test-name-tagging fixture surface | gomtest golden.go/GenerateFunc, suite options, testdata/golden | gom-wt-gomtest-followup | when a slot frees |
| 4 | Multi-file eval | internal/eval, build driver, eval fixtures | gom-wt-eval-multifile | when a slot frees |

Overlap notes:

- Lane 1 owns `internal/check/apply.go`/`reclassify.go` +
  `internal/goimport` extern admission + spec labeled-args wording +
  §8.4. Lane 2 owns `internal/check/expr.go` diagnostic report sites +
  `internal/lower` tests + `internal/gomtest` discovery/docs.
  `internal/diag/codes.go` is append-only all lanes (lane 1 may need a
  new E04xx for the rejection if that option is chosen — pick a fresh
  code, never reuse).
- Lanes 2 and 3 both touch `internal/gomtest`: lane 2 owns
  `discoverCases`/`dirCase` + `spec.go`/`doc.go` (root-orphan item);
  lane 3 owns `golden.go` + any `GenerateFunc` signature widening +
  suite-option plumbing. Directory cases already discover correctly —
  lane 3's work is output format + emission, so `gomtest.go` discovery
  changes shouldn't be needed; if they are, keep them additive and
  flag the overlap in the completion report.
- Lane 4 is disjoint from all others (`internal/eval` + build driver).
- spec.md: lane 1 labeled-args + §8.4; lane 2 probably none
  (diagnostic internals aren't spec-pinned — E0402/E0403 wording
  changes must not break existing expected.json fixtures unless
  updated); lane 3 none; lane 4 possibly eval-semantics wording if
  behavior surfaces.

## Individual prompts

### 1. Call honesty — `wt/call-honesty` @ gom-wt-call-honesty

```
Two soundness items in the calling-convention family; one commit each.

(a) [decision+impl] labeled arg into a RETURNED function's param
    silently collapses to positional (TODO bug + gaps entry —
    authoritative, read both first): `fn f (~a: Int) -> (~b: Str) -> Int`
    with `f ~b: "x"` — `paramByLabel` returns flat index 1,
    `pi >= declArity` sets `overDeclared` silently, `ArgToParam` nils,
    lowering emits `f("x")` → unbuildable Go. Same shape `f ~b: 5` →
    `f(5)` silently misbinds ~a. The wave9 reclassify sketch
    (docs/sketch/2026-11-05-reclassify-arg-to-param-guard.md) documents
    the machinery this bug lives in — the ArgToParam guard DEPENDS on
    overDeclared nil-ing the mapping, so check that interaction before
    changing either side.
    Decide between: (i) reject at check — a label naming a param beyond
    the callee's own declared arity is not an argument to this call
    (likely a new E04xx or reuse an existing labeled-args code — check
    what E0408 covers); (ii) extend the meaning — `f ~b: "x"` = eta
    `fn a -> f a ~b: "x"`, i.e. cross-level reorder-fill in the wave8
    closureChain spirit. The gap records the bug; the decision records
    which reading is adopted and why the other is rejected. Verify
    eval-vs-compiled on the chosen semantics + spec fixtures both ways
    (rejection diag OR the eta meaning), plus a `CallUnknown` /
    forward-reference variant since that's where the guard fires.
(b) [decision+impl] `(NamedIface, error)`/`(any, error)` first result
    can't get the Option view (TODO task + gaps entry): `nilablePresence`
    admits only `*T` and universe `error`, so `(net.Error, error)`
    derives `Result <named> GoError` with an unchecked nil payload and
    `Result (Option T) GoError` declarations are rejected. Decide the
    nil-able class for first results — candidates: any interface type
    (nil-able in Go), named-interface + `any` only, or status quo with
    spec wording. `any` needs care: its nil is real but `Ok None` from a
    non-error `any` may be a worse default than verbatim — check what
    the derived mapping should produce vs what externs may declare.
    Decision → prototype → sketch → adopt, then impl the chosen form
    (admission + derived mapping + ConvOptNil/NilGuard coverage as
    needed + spec §8.4 admission table + fixtures).
```

### 2. Hygiene batch — `wt/hygiene` @ gom-wt-hygiene

```
Three small items; one commit per item.

(a) [impl] E0402/E0403 (and check E0410/E0411) leak raw `?n` — same
    class as the merged E0407/E0409/E0701/E0801 batch (TODO task):
    `check/expr.go` E0403 (if-without-else) uses `then.Type()/.String()`,
    E0402 (mid-block Unit warning) uses `ty.String()`; E0410/E0411's
    `b.ret.String()` may share it for unresolved lambda-ret metas.
    Reproduce each leak first (`if true { todo () % todo () }`,
    mid-block constrained meta), apply fresh `metaNamer()` +
    `types.Display` at every leaking report site, and add a spec
    fixture per observable leak. If E0410/E0411 prove unreachable in
    practice, record why rather than guessing.
(b) [impl] `payloadNilGuard`/`ConvOptNil` lower unit tests (TODO task):
    the core nodes' generation conditions — `GoResultsErr` + raw-`error`
    payload + declared `error` payload, the three-way distinction — are
    pinned only end-to-end (golden + extern tests). Add lower-side unit
    tests pinning when each node appears and when it doesn't (Option
    view → no NilGuard; `(int,error)` → no NilGuard; strict
    `Result GoError GoError` → NilGuard at every EscapeCall/
    ResultFromGo site). Look at existing lower tests for the shape to
    imitate (e.g. how goResultsOf is already pinned).
(c) [impl] spec-suite root bare side files silently ignored (TODO task,
    nit): a bare `expected.json`/`case.json`/`golden.go` directly under
    the spec suite root is neither marker nor orphan — `dirCase` never
    runs on suiteDir and the name fails the `.expected.json` suffix
    check. Decide: report them as load errors (consistency with
    malformed-group posture) or document the ignore. Reporting is
    probably right — a stray `expected.json` at root almost always
    means a misplaced fixture file. Small impl + a gomtest testdata
    case either way.
```

### 3. gomtest residuals — `wt/gomtest-followup` @ gom-wt-gomtest-followup

```
Two harness residuals from the wave9 directory-cases lane; each needs
a small format decision (draft → prototype → sketch → adopted) then
impl. One commit each. Ownership: golden.go + GenerateFunc side +
suite options — leave discoverCases/dirCase alone (a parallel lane
owns it).

(a) [decision+impl] golden directory cases (TODO task): spec suite
    supports dir cases but RunGoldenTests rejects them with t.Fatalf —
    a dir case's output is per-member generated .go, so the golden
    format itself needs deciding: per-member golden files inside the
    case dir (a.golden.go next to a.gom?), one concatenated output, or
    a manifest. Check how RunGoldenTests/GenerateFunc currently map
    case → output (internal/gomtest/golden.go, callers in
    internal/codegen or build tests), pick the least special-cased
    shape, and migrate an existing flat golden case or author a small
    dir case to prove it. Note: GenerateFunc may need the same
    func([]File) widening DiagnosticsFunc got — that signature is in
    scope here.
(b) [decision+impl] sibling test-name tagging can't be pinned by spec
    fixtures (TODO task): emitted `TestX_<tag>` func names aren't
    diagnostics, so expected.json can't express them; today only
    internal/build's TestCompileFilesTestNameCollision covers it
    (decision 2026-11-05-test-name-file-tag). Options: a suite option
    that compiles with lower.Env{Tests:true} and an expectation
    surface for emitted func names (new side-file kind? an
    expected-funcs.json?), OR extend the existing dir-case mechanism
    with a func-name assertion section in expected.json, OR decide the
    driver test is the right home and document that. If a fixture
    surface is chosen, implement + migrate the collision case as a
    spec fixture.
```

### 4. Multi-file eval — `wt/eval-multifile` @ gom-wt-eval-multifile

```
[decision+impl] eval.New takes a single *check.File, so it can't see
sibling package lets — since package-unit compilation (V-6) the
eval-vs-compiled comparison harnesses (internal/build/diff_test.go,
compare_test.go, eager_test.go — all do eval.New(r.Typed) on
single-file compiles) can't verify programs whose behavior depends on
package-scope lets across files. open-questions entry records this.
Task: extend eval to accept the unit's typed files
(check.Files returns []*check.File; build.CompileFiles already hands
them out — likely an eval.NewFiles([]*check.File) or New taking a
unit) so sibling decls share one package scope, in the same order the
init-order machinery (package-initialization-order decision) computes.
Key questions to resolve in the decision: declaration order across
files (dependency order vs file order — follow what codegen/runtime
does), how test harnesses switch to multi-file eval (keep New for
single-file compat or migrate), and whether eval semantics change for
forward refs. Then migrate at least one comparison harness path to
multi-file eval and add a two-file eval-vs-compiled case (sibling
package let + init order). If the scope proves bigger than a lane
(e.g. eval needs real package initialisation semantics), implement
the narrow version that unblocks the harness and record the rest.
```

## Sequential tail (when a lane frees, or Wave 11 lead)

```
- gist-cli byte I/O residual ([-] TODO) — example-local adapters.
- `*T` default (auto result/param mapping) — open-questions decision,
  may fold into a stdbind/extern-admission lane.
- `Go.Any`/`any` spellability in signatures — open-questions decision.
- Emacs gom-mode tree-sitter port (self-contained tooling lane).
- P4-6 LSP, P4-7 incremental cache — larger slots.
```

## Items needing decisions, not implementation

- `*T` default; `Go.Any`/`any` spellability (both open-questions).
- Structural Go constraints (`~[]E`/`~map`), Go struct construction /
  field writes (boundary-contract decision feeds wording here),
  external-module stdlib imports, 3+-result fns, `rune`->`Int32` vs
  `Char`.
- `|?>` wrap-wording configurability — future work.
- Exported-`let` façade / API hardening — deferred alternative recorded
  in boundary-mutation-contract; revisit only if a hardening mode is
  wanted.

## Wave 11 candidates

- P4-6 LSP (SourceMap-aware inverse mapping), P4-7 incremental cache.
- `Go.Any` spellability impl; `*T` default impl (post-decision).
- Emacs tree-sitter; gist-cli byte I/O residual.
- D-3 README final adequacy, D-4 tutorial go.mod layout — end of
  project per user direction.

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
