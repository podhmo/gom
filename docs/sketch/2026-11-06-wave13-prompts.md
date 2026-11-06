# Wave 13 orchestration prompt drafts

State as of 2026-11-06: Wave 12 integrated — codegen-boundary
(`f3147e6`), lsp-multifile (`e5f76aa`), eval-forcing (`9ce1d58`),
interop-decisions (`5cff2f7`), ledgers (`60b2b27`, `5a23064`,
`94bcd9b`). Round record:
[2026-11-06-wave12-round.md](2026-11-06-wave12-round.md).
This file drafts the prompts for the next wave, following
`2026-11-06-wave12-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Orchestration notes (Wave 12 lessons)

- Normative-doc overclaims caught for the **fifth wave running** —
  this time in a *decision* lane: spec §8.3 asserted an undecided
  behavior (declared-result `Go.Any` boxing) as normative and used
  `type Token = any` (a Go alias, collapses to `Go.Any`) as the
  example for a nominal distinct type. Reviewers now diff every
  normative sentence in spec/decision/doc.go/in-code comments
  against implementation regardless of lane type.
- **Probes beat confirmation** — reviewer-probed shapes found
  failures worse than the recorded class twice: `Option Go.Any`'s
  documented gap was a clean E0407, but `Some 42` passes
  `gom check`/`gom build` and fails `go build` (silent). And lane
  3's "pure aliases are observably identical" was falsified by a
  fn-path reference loop. Prompt reviewers to try adversarial
  variants of recorded gaps, not just verify the recorded one.
- **`go test` green ≠ gate green** — lane 3's partial type switch
  on a `//sumtype:decl` union passed tests but fails
  `scripts/check-sumtype.sh` (CI gate; repo convention: partial
  probes are if-assert chains). The before-finishing gate now
  includes `sh scripts/check-sumtype.sh`.
- **In-code comments count as normative text** — lane 2's CWD
  leak shipped behind two comments claiming the shape was
  impossible. Reviewer traced the non-`file:` URI path and found
  it live. Comments get the same honesty review as spec prose.
- **Workers verify reviewer claims before adopting** — lane 3's
  worker found the reviewer's counterexample program didn't
  typecheck, rebuilt a type-correct variant that confirmed the
  divergence, then rescoped the residual. Keep requiring
  deviation reports; don't auto-apply reviewer sketches.
- **Forbidden-ledger edit recurred** (lane 1, inside a docs
  commit) — content verified accurate, landed via squash;
  orchestrator ledger pass reconciled. Rule stays.
- **No rate-limit deaths this wave.** Merge conflicts only the
  generated decisions index (once — regenerated post-merge).

## Dependencies cleared by Wave 12

- **Generated imports always carry the bound name** — `lib "x/lib"`
  unconditionally; package-clause mismatches can no longer produce
  unbuildable output; self-imports still structurally impossible
  (`EntryPath` checked before `imp()`).
- **`.gomi`-only cycles → E0606 with `data.cycle`** — the loader's
  in-flight guard carries the chain (`map[string]int` + `stack`);
  impl scan still sees only `*.gom` edges (back-edges to
  non-mid-load packages stay non-cycles).
- **E0603 clause check is live** — impl `iface.Package` gets
  `Name` wired; stray `.gomi` sources register before E0604 (the
  "no source registered" emission bug is closed; gaps entry
  corrected E0601→E0604).
- **`unknownCall` silent-drop closed** — 0-arg non-`-> T` is a
  defensive E0901; deferred `alwaysGom` replay pinned.
- **LSP compiles package units** — overlay gather (buffer shadows
  disk, absolute `.gom` only), per-member publish/retract,
  ~48–50 ms/recompile with `import go` measured. Real Eglot
  verified.
- **eval init order = Go frontier** — `check.Decl`-level vardep
  (`internal/eval/initorder.go`), earliest-declared all-ready
  pick, `test` decls after init. ValueAlias residual recorded.
- **Two interop decisions landed** — `*T` default = `Ptr T`
  (ResNilable deliberately not extended); `Go.Any` admitted at
  all type-expression positions (impl pending — spec §8.3 is
  intentionally spec-ahead). 5 impl TODOs + 5 gaps filed.

## Recommended Wave 13 composition — 4 lanes

The wave defines 4 lanes. **Execution under Devin: run at most
2 implementation lanes concurrently** (pool cap — waves 8–12
each hit the limit); lanes 3–4 are queued and launch as slots
free, in table order.

| # | Task | Main areas touched | Worktree | Under-Devin order |
|---|------|--------------------|----------|-------------------|
| 1 | `Go.Any` admission + boundary honesty (5 TODOs: admission patch, declared-result boxing, exported-let fidelity, `Option Go.Any` None, container-payload mismatch) | internal/resolve/scope.go, internal/check (typenv/unify/expr sites), internal/codegen/decl.go, internal/lower (result-conv), spec §8.3-adjacent + testdata/spec | gom-wt-go-any-boundary | immediate |
| 2 | ValueAlias shared-lift — close the eval init-order residual | internal/lower/alias.go + new shared home (new package preferred), internal/eval (initorder consumer) | gom-wt-eval-alias-lift | immediate |
| 3 | SourceMap-aware inverse + go-tool diagnostic folding (the hard substrate) | internal/diag/srcmap.go (additive), internal/build gopos, internal/lsp diagnostics | gom-wt-lsp-gotool | when a slot frees |
| 4 | Tooling hygiene: `.gomi` type-member table + flat `tests:true` golden + spec `Go.Int` wording + gomtest Fatalf infra | internal/gomtest, internal/stdlib, testdata/golden conventions, spec.md wording bits | gom-wt-tooling-hygiene | when a slot frees |

Overlap notes:

- Lane 1 owns `internal/check` broadly this wave (`typenv.go`,
  `unify.go`, `expr.go` sites as needed), `internal/resolve/
  scope.go`, `internal/codegen/decl.go` + varDecl-adjacent, and
  `internal/lower`'s result-position conv insertion site. Lanes
  2–4 must not touch these.
- Lane 2 owns `internal/lower/alias.go` + the shared home it lifts
  into + `internal/eval`. **Prefer a new leaf-adjacent package**
  (e.g. `internal/declclass` or similar — decide + record) over
  landing inside `internal/check`, which is lane 1's surface; if
  check is truly the right home, make it a new file and flag the
  overlap in the report. `internal/lower/expr.go` is NOT lane 2's.
- Lane 3 owns `internal/diag/srcmap.go` (additive),
  `internal/build` (gopos/`GoToolDiagnostics` consumption —
  additive), `internal/lsp`. If the honest inverse needs codegen
  anchor-emission changes, flag in the report — codegen is lane
  1's this wave; prefer a build/diag-side additive solution.
- Lane 4 owns `internal/gomtest` + fixtures, `internal/stdlib`,
  `testdata/golden` conventions, spec.md wording bits (§3-adjacent
  only — lane 1 owns §8.3-adjacent). No internal/check, lower,
  codegen, eval, lsp, diag edits.
- `internal/diag/codes.go` is append-only for all lanes.
- spec.md: lane 1 → §8.3-adjacent; lane 4 → §3.1-adjacent wording
  only. Call out touched sections in completion reports.

## Individual prompts

### 1. `Go.Any` admission + boundary honesty — `wt/go-any-boundary` @ gom-wt-go-any-boundary

```
[impl] Five recorded TODOs closing the `Go.Any` decision —
read docs/decisions/2026-11-06-go-any-spellable.md and the
prototype sketch docs/sketch/2026-11-06-go-any-spellable-prototype.md
FIRST (the 2-line admission patch is recorded there; re-apply
it as your starting point). Items interlock: land admission
first, then each gap fix can assume `Go.Any` resolves.

(a) [impl] Admission: `builtinGoMembers` += `Any` (KindType/Go),
    `goTypeArity` += `"Any": 0`. Spec fixtures BOTH directions:
    signature/annotation/extern Option+Result declaration/
    container accepts; expression-position `Go.Any` → E0201,
    `Go.Any Int` arity reject, `Option Go.Any` param → E0509,
    `Go.conv` from `Go.Any` → E0507 (verify each error code
    against diag/codes.go before writing fixtures).
(b) [decision+impl] Declared-result boxing (residual gap 1):
    `fn -> Go.Any` / `-> Result Go.Any GoError` bodies of
    named-iface (json.Token) or typevar hit E0901 in
    core/validate. Decide: emit `any(...)` materialize at
    residual positions (check how ConvBoxAny reaches Go.conv
    sites in lower today and reuse the channel — call facts
    travel structured, per AGENTS) OR narrow check admission to
    reject. Fixture either way.
(c) [impl] Exported-let declared-type fidelity (residual gap 2):
    `.gomi` `let x : Go.Any` must produce `var X any`, not the
    initializer's concrete type — codegen's varDecl currently
    emits `d.Body.Tail.Type()`. Carry the declared type through
    for exported lets. Check what `.gomi` conformance requires
    of the emitted API surface first.
(d) [decision+impl] `Option Go.Any` `None` → E0407 (residual
    gap 3): `isGoAny(w)` accepts without binding the meta —
    `None`'s element meta dangles. Refine the want-side meta
    bind or document the restriction honestly. Check whether
    the same dangling-meta class hits other shapes while you're
    in unify.go.
(e) [impl] Container-payload type/rep mismatch (residual gap 4 —
    review-found, the WORST shape): `let o : Option Go.Any =
    Some 42` (`Ok 42`, `[1]` same) passes `gom check`/`gom
    build` then fails `go build` (bind emits `Option[int64]`,
    slot is `Option[any]` — type-invariant generic arg). Either
    materialize the payload into the `Go.Any`-typed slot or
    reject the concrete bind at check. Silent-failure class —
    closing it (either direction) matters more than (d)'s clean
    diagnostic. Scalar `let x : Go.Any = 42` (`x := int64(42)`)
    is the same shape but harmless — pin it as accepted with a
    comment why.

Ownership: internal/check (typenv, unify, expr sites as needed),
internal/resolve/scope.go, internal/codegen/decl.go (+ varDecl-
adjacent), internal/lower result-position conv site, spec
§8.3-adjacent + testdata/spec fixtures. diag/codes.go
append-only. Do NOT touch internal/lower/alias.go, internal/eval,
internal/lsp, internal/gomtest, internal/stdlib, internal/diag —
other lanes own those. gaps entries to close: the 4 residual-gap
items + the spec-ahead-of-impl item — report proposed check-offs
(the spec-ahead entry's §8.3 claim becomes TRUE under admission;
verify the wording still matches once (a) lands).
```

### 2. ValueAlias shared-lift — `wt/eval-alias-lift` @ gom-wt-eval-alias-lift

```
[decision+impl] Close the recorded ValueAlias init-order residual
(gaps entry "残差: ValueAlias FuncDecl に下る let の初期化順ずれ"
+ decision 2026-11-06-eval-init-ready-order §5 + open-questions
future work): eval's frontier graph works on check.Decl nodes,
but lower rewrites some LetDecls into ValueAlias FuncDecls
(hidden temp var + wrapper fn) that eval can't see — two
divergent shapes recorded:
(a) `let f = { …; count }` effectful init → Go's caller doesn't
    depend on the temp but eval treats f-refs as var deps
    (measured: compiled `w,a,f-tmp` / eval `w,f,a`)
(b) `let f = g` pure alias on a fn-path loop
    (`fn g = { ignore (f); … }`) → the g-body's f-ref resolves
    to varNode[f], a self-dependency → never-ready → leftover
    forced in decl order (measured: compiled `w,z` / eval `z,w`;
    pinned as `known_residual:` test)

Fix path recorded in the gaps entry: lift
`internal/lower/alias.go`'s classification (`bindElems`,
transitive `static()`, `initNeedsEval`, arity/thunks index) to a
shared layer both lower and eval consume — review + fix round
verified it already runs on check.Decl/check.Expr with no core
dependency (arity/thunks are recomputable from check.FnDecl),
so this is nearly a file move, not a port.

Steps:
(1) Choose the shared home. Prefer a NEW leaf-adjacent package
    (dependency direction stays one-way per AGENTS; eval is a
    leaf, lower is mid-pipeline) over landing inside
    internal/check — check is another lane's surface this wave.
    If check is truly the right home, use a new file and flag
    the overlap. Record the choice + reasoning in a decision
    (draft → prototype → verify → status).
(2) Lift the classification; lower consumes the shared version
    (no behavior change — same classification), eval's
    buildInitGraph consults it so alias-lets get the node shape
    lower will actually produce.
(3) Update the `known_residual:_pure_alias_on_a_function-path_loop`
    pin to the corrected expectation; add a pin for shape (a);
    extend diff_initorder_test.go so the previously-divergent
    programs now compare equal eval-vs-compiled.
(4) Update the decision + verification sketch; proposed gaps
    check-off + open-questions removal in your report.

If the lift reveals the classification depends on lower-side
state after all (arity/thunks indexes built mid-lower, initNeedsEval
needing core-level info), record the real boundary and pick the
honest narrower fix — don't fake the lift.

Ownership: internal/lower/alias.go + its new shared home +
internal/eval (initorder.go consumer, tests). internal/check
ONLY if the shared home lands there (new file, additive). Do
NOT touch internal/lower/expr.go, internal/check existing files,
internal/codegen, internal/lsp, internal/gomtest — other lanes
own those. diag/codes.go append-only.
```

### 3. SourceMap inverse + go-tool diagnostics — `wt/lsp-gotool` @ gom-wt-lsp-gotool

```
[decision+impl] The recorded hard substrate (TODO task "LSP:
go-tool 診断の取り込み"): folding `go build`/`go vet` diagnostics
back to `.gom` positions needs the SourceMap-aware inverse that
doesn't exist. Two gaps entries gate it — `//line` drift (one
.gom line expanding to k generated lines drifts subsequent
//line numbers, so GoPosIndex.Resolve can alias a report line to
a real-but-wrong .gom line) and `SpanAt` returning inner-syntax
spans instead of generated-line granularity. design.md 2.2 wants
a correspondence-table inverse + ambiguity fallback; the LSP
package-unit work (wave-12) is the consumption surface.

This lane is the substrate, not the whole feature — build the
inverse honestly, then fold go-tool diagnostics through it. If
the honest inverse needs codegen to emit finer anchors (per-
statement //line or an anchor-table artifact), that's the
difficult wall: either implement additive anchor emission (flag
it — codegen is another lane's this wave) or record precisely
why the build/diag-side inverse is insufficient and what codegen
change would unblock it. A "this approach doesn't work and
here's why" record is a real outcome.

Steps:
(1) Decision draft FIRST: what do GoPosIndex/SourceMap record
    today (read internal/build/gopos.go + internal/diag/srcmap.go),
    what's the cheapest emission change making the inverse
    unambiguous, what's the ambiguity fallback. Record
    candidates + choice.
(2) Implement the inverse + fallback. Tests construct real drift
    shapes: a multi-line-expanding .gom line followed by a
    diagnostic on a later line (the aliasing case itself), plus
    the honest fallback when the span is ambiguous.
(3) Fold go-tool output through it: decide where this lives —
    `gom lsp` post-package-compile (wave-12 landed the package
    unit; go build on unit output?) vs a `gom build`-time path
    (GoToolDiagnostics exists in internal/build but has no live
    caller — the "実起動がない" gaps entry). Record the choice;
    LSP publish is the natural consumer but a CLI path may be
    the honest first surface.
(4) Verification: induce a real `go vet`/`go build` error in
    generated code and show it lands on the right .gom line —
    not the drifted wrong-but-real line. Ambiguity falls back
    honestly. Per-change cost of the extra go-tool invocation
    is a new session cost — measure roughly and record.

Partial honest outcomes ship: anchor model + fallback + recorded
wall is a fine lane result.

Ownership: internal/diag/srcmap.go (additive), internal/build
(gopos + GoToolDiagnostics consumption — additive), internal/lsp
diagnostics path. Do NOT touch internal/check, internal/lower,
internal/codegen emission beyond additive anchors (flag if
needed), internal/gomtest, internal/stdlib — other lanes own
those. diag/codes.go append-only.
```

### 4. Tooling hygiene — `wt/tooling-hygiene` @ gom-wt-tooling-hygiene

```
Mixed tooling + wording items; one commit per item. None touch
internal/check, internal/lower, internal/codegen, internal/eval,
internal/lsp, internal/diag — other lanes own those. spec.md:
§3.1-adjacent wording only (§8.3 is another lane's).

(a) [impl] `.gomi` type members not on the module member table
    (gaps entry): internal/stdlib skips MemberType building the
    Modules table, so a `.gomi` type member can't be referenced
    as `pkg.Type`/`pkg.Type.Ctor` through the builtin-module
    path (gomimport resolves it via p.Members — the skew is
    stdlib-side only; inert today since no .gomi declares
    types). Add MemberType registration + a test that a .gomi
    type member resolves identically through both paths — or
    record why it's structurally deferred.
(b) [impl] Flat `tests:true` `x_test.golden.go` convention
    (recorded future extension in the gomtest-tests-option
    decision): directory cases emit `<member>_test.golden.go`;
    a flat (non-directory) golden case with `{"tests": true}`
    currently fails the 1-output constraint. Decide the flat
    convention (`x_test.golden.go` alongside `x.golden.go`?)
    and implement in the golden harness + a flat fixture.
(c) [wording] spec.md:196 `Go.Int` example — `Go.Int` is not
    spellable (internal Con only; `goTypeArity` admits only
    `Go.Slice`/`Go.Map`; same class the wave-12 fix round
    corrected in the go-any-spellable decision). The sentence's
    point (qualified names never shadow) is right but the
    example can't be written. Replace with a spellable name
    (`Go.Slice`?) and sweep the file's other `Go.*` examples
    against builtinGoMembers/goTypeArity for the same class.
(d) [impl] gomtest `t.Fatalf`-path test infra (TODO task):
    stray/duplicate golden and 1-output-constraint failures
    report via `t.Fatalf`, which no in-process test can
    exercise. Build the failure-injection or subprocess harness
    that pins the fatal paths — or convert the right call sites
    to Errorf if that's the honest fix. Check how the suite
    currently splits Fatalf vs Errorf first (stray/duplicate/
    1-output are the recorded targets).

Ownership: internal/gomtest + fixtures, internal/stdlib,
testdata/golden conventions, spec.md §3-adjacent wording.
diag/codes.go append-only.
```

## Sequential tail (when a lane frees, or Wave 14 lead)

```
- P4-7 incremental build cache (design.md 5 章 cache keys;
  P4-1 done — no remaining blocker).
- gist-cli byte I/O residual ([-] TODO — example-local adapters;
  prototype doc lists needed ops).
- LSP completion/definition/references + incremental sync +
  `$/cancelRequest` + didSave/workspace + loader session cache
  (~48–50 ms/recompile measured in wave 12 — a cache is the
  recorded cut).
- `metasIn(want)` quiet-ification over-suppression (gaps entry —
  shared want across conform positions can swallow a legit
  2nd-arm E0401; narrow reachability kept it accepted).
- `.gomi` `let` member polymorphism (thunk representation across
  imports — open question, needs the member-table work first).
- D-3 README final adequacy, D-4 tutorial go.mod layout — end
  of project per user direction.
```

## Items needing decisions, not implementation

- Structural Go constraints (`~[]E`/`~map`), Go struct
  construction / field writes, external-module stdlib imports,
  3+-result fn *implementation* (v1 wording pinned in wave 11),
  `rune`-as-`Char` semantic widening (problems note exists).
- `|?>` wrap-wording configurability — future work.
- Exported-`let` façade / API hardening — deferred alternative
  recorded in boundary-mutation-contract (related to the
  exported-let declared-type item inside lane 1 — flag if the
  two interact).
- `Result.to_option`/`Option.to_list` — structurally blocked by
  Go import cycles; stays recorded.
- W0701/W0702 lint coverage beyond the scope-out — recorded
  scope, widening is an open question not yet a lane.
- Mutability beyond `Ref`/`Array` — user-declared mutable
  structures still open (lint list is closed builtin set).

## Wave 14 candidates

- LSP completion/definition/references + incremental sync +
  session cache (after lane 3's substrate lands).
- P4-7 incremental cache.
- gist-cli byte I/O; `metasIn` suppression; `.gomi` poly-let.
- D-3 README final adequacy, D-4 tutorial go.mod — end of
  project per user direction.

## Common instructions (append to every prompt)

```
Before finishing run: gofmt -l . (no output), go vet ./...,
go test ./..., AND sh scripts/check-sumtype.sh (CI gate —
`make check` additionally covers staticcheck+experiments; run
if cheap). Partial probes on `//sumtype:decl` unions are
if-assert chains, never partial type switches. Commit WIP early
and often — worktree commits are the only recovery point if the
session dies. Decisions made during implementation go to
docs/decisions as 状態: draft; verify with a prototype, write
results to docs/sketch, then update status. Language-rule
changes add testdata/spec fixtures, generated-code changes
update testdata/golden, Go-interop changes add tests in both
directions. Concerns go to docs/problems; spec/implementation/
doc discrepancies are recorded in docs/implementation-gaps.md,
never resolved by silently rewriting the spec. In-code comments
are normative text too — verify them against the mechanism they
describe (wave-12 CWD leak shipped behind two wrong comments).
Do not edit TODO.md, docs/open-questions.md,
docs/implementation-gaps.md, or TODO-archive.md — report
proposed ledger changes in your completion report and the
orchestrator will apply them. Read skills/gom-agent-guide
before writing .gom/.gomi files. Verify every normative doc
claim against the code it describes — doc overclaims have been
caught five waves running, including in decision lanes. Fixtures
carrying raw bytes (CRLF, non-UTF-8) need a .gitattributes
-text mark at creation time. Probe adversarial variants of the
gaps you're fixing, not just the recorded shape — wave-12
review found worse failure classes twice that way.
```
