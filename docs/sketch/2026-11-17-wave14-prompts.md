# Wave 14 orchestration prompt drafts

State as of 2026-11-17: Wave 13 integrated — go-any-boundary
(`a5506cb`), lsp-gotool (`d181a80`), eval-alias-lift (`047cf50`),
tooling-hygiene (`8a0e136`); ledgers `a37fa7b` `62fcdc1` `e4fb95e`
`1c59cdf`. Round record:
[2026-11-06-wave13-round.md](2026-11-06-wave13-round.md).
This file drafts the prompts for the next wave, following
`2026-11-06-wave13-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Execution model change (user direction, 2026-11-17)

Quota constraints: **no child sessions this wave** — one Devin
session executes every lane sequentially. Review is delegated to
Devin Review on the PR instead of fresh read-only reviewer
sessions. Consequences:

- **One lane = one branch = one PR.** Lanes stay disjoint by
  surface so each PR reviews cleanly; the user merges.
- **Ledger edits move into the lane PR.** The
  orchestrator-on-main reconciliation is gone, so TODO.md /
  gaps / open-questions check-offs for a lane's own work ride in
  its PR (the wave-9..13 "forbidden-ledger-edit" rule is lifted
  for this wave — there is no orchestrator branch to conflict
  with). Mark only what the lane itself closed.
- **Sequential order below replaces the parallel-lane table.**
  Ownership notes remain as surface documentation for review,
  not conflict avoidance.

## Orchestration notes (Wave 13 lessons)

- **Normative-doc overclaims caught for the sixth wave
  running** — reviewers now diff every normative sentence in
  spec/decision/doc.go/in-code comments against implementation.
  Keep prompting that check.
- **Probes beat confirmation** — reviewer-probed shapes found
  worse failure classes than the recorded gaps twice. Probe
  adversarial variants, not just the recorded one.
- **`go test` green ≠ gate green** — run `make check` (gofmt /
  vet / `go test -race` / check-sumtype / check-staticcheck /
  check-experiments); `go vet`/`go test` alone miss dead helpers
  and partial type switches.
- **In-code comments count as normative text** — verify them
  against the mechanism they describe.
- **Forbidden-surface touch can be necessary** — lane 1 had to
  no-op its own new ascribe in eval to keep eval's suite green.
  Flag + justify rather than assuming surfaces are avoidable.
- **Rate-limit deaths: 3 reviewer deaths** — moot this wave
  (single session, PR-based review).
- **Worker zero-commit violation** — commit WIP early and often;
  commits are the only recovery point.

## Dependencies cleared by Wave 13

- **`Go.Any` is a first-class type spelling** — `builtinGoMembers`
  + `Any`, `goTypeArity["Any"]=0`; one materialize channel
  (`conformExpr` → `Ascribe{Conv:true}` → `ConvBoxAny`) covers
  declared results, `.gomi` exported lets, extern `any` slots.
  `unify`/`unifySub` split: value-slot accepts meta-free;
  structural positions bind got-metas to `Go.Any`. `unifyFunc`
  params invariant. Residuals (P1/P2 silent-failure bugs,
  F1–F3 coverage gaps, P3–P5 pre-existing) are ledgered — lane 1
  below.
- **eval/lower share static-alias classification** —
  `internal/staticalias.Classify(decls, universe)`; both recorded
  init-order residuals converge.
- **SourceMap-aware inverse landed** — `SourceMap.ReportedLine`/
  `Candidates` + `GoPosIndex.AddSourceMap`/`ResolveMatch`
  (exact/drift/ambiguous); `GoToolDiagnostics`/`ResolveStack`/
  `RenderStack`/`gom test` fold are match-aware. Honest wall:
  `gom lsp` is CheckOnly — the go-tool invocation point is the
  remaining design decision (lane 2 below).
- **stdlib `.gomi` type members on the module table**;
  flat `{"tests":true}` golden convention; gomtest `t.Fatalf`
  helper-process infra.

## Recommended Wave 14 composition — 4 sequential lanes

| # | Task | Main areas touched | Branch | Order |
|---|------|--------------------|--------|-------|
| 1 | `Go.Any` residual boundary — P1/P2 silent-failure bugs, then F1–F3 pins + P3–P5 | internal/check (apply/expr/unify sites), internal/lower/equal.go, internal/codegen lit emit, testdata/spec | `devin/wave14-go-any-residuals` | first — silent-failure class |
| 2 | CLI go-tool invocation — `gom build -go` (decision+impl) | cmd/gom, internal/build (additive) | `devin/wave14-cli-gotool` | second |
| 3 | Hygiene: `metasIn` suppression, stdlib inert divergences, spec §8.5 generics wording | internal/check (narrow), internal/stdlib, internal/iface, spec.md | `devin/wave14-hygiene` | third |
| 4 | gist-cli byte I/O + `Go.Slice String` boundary | experiments/gist (or wherever the example lives), internal/goimport or rt as needed | `devin/wave14-gist-cli` | fourth |

Overlap notes:

- Lane 1 owns `internal/check` broadly (`apply.go`, `expr.go`,
  `unify.go` sites as needed), `internal/lower/equal.go` +
  pat/emit sites for P3, `internal/codegen` lit emit for P2.
  Lane 3 may touch `internal/check` only for the `metasIn`
  suppression item (narrow, additive); sequence lanes 1 → 3 to
  avoid intra-check conflicts.
- Lane 2 owns `cmd/gom` and `internal/build` (invocation-site
  consumption — additive). Rewritten after podhmo's B-route
  pick: the invocation surface is the CLI, not `gom lsp` — the
  per-change latency and async-publish questions leave this
  wave. `internal/lsp` is untouched.
- `internal/diag/codes.go` is append-only for all lanes.
- spec.md: lane 3 → §8.5-adjacent only; lane 1 may touch
  §8.3-adjacent wording if a residual resolution changes what
  the spec promises — call out touched sections in the PR body.

## Individual prompts

### 1. `Go.Any` residual boundary — `devin/wave14-go-any-residuals`

```
[impl] Close the recorded Go.Any boundary residuals
(docs/implementation-gaps.md, wave-13 go-any re-review
P1–P5 + F1–F3; TODO.md Bugs section). Order: silent-failure
bugs first, then honesty pins, then loosen-vs-document.

(a) [bug] Direct call on a `Go.Any` value emits invalid Go:
    `a : Go.Any` + `a "s"` slips through `conformCallee`'s
    fallback `unify(any, Fn(res, P(argT)))` via the `isGoAny`
    early-accept and emits `a("s")` → `cannot call non-function`
    at `go build` (check/apply.go). Reject honestly at check —
    a `Go.Any` callee cannot be classified as callable; the
    user spells the call through `Go.conv` or a type switch.
    Probe adjacent shapes: `a ()`, `a 1 2`, `a |> f`,
    over-apply on `(x : Go.Any) x`, `a "s" |> g`.
(b) [bug] `a == 42` (`a : Go.Any`) is always-false: `g.lit`
    emits raw `int`, `rt.Equal` sees `reflect.DeepEqual(int64,
    int)` → false (check/expr.go checkBinary, lower/equal.go,
    codegen/expr.go g.lit). Emit operand conform —
    `rt.Equal(a, any(int64(1)))` — or the honest alternative the
    mechanism supports; check whether the same raw-lit shape
    hits other compare ops (`!=`, and pattern-position lit tests
    are item (d)).
(c) [pin] F1–F3 coverage gaps are all honest E0401s today —
    better than the silent bad-emits they replaced. Fixture-pin
    the current rejections FIRST (E0401 fixtures: `Int -> Go.Any`
    lambda via checkLambda's fresh `?r`; `id (Some 1)` meta-ret
    pin skip; `Some 1 |> f` pipe-arg pin). Then decide per item:
    loosen (want through checkLambda, generalized ret pin, pipe
    pin) or document the restriction. Record the per-item call.
(d) [pin] P3: `match o { Some 42 -> … }` on `Option Go.Any` —
    the literal arm is unreachable (`case 42` vs `any(int64(42))`
    — same rep-mismatch class as (b)). Either materialize the
    lit test against the boxed rep or record honest deadness.
    P4: any/concrete mixed if-arms under bare `Go.Any` want are
    order-dependent (`1` first → join meta num-binds → E0401;
    `v` first → conform passes). Pin current behavior; decide
    whether ordering-independence is reachable without a
    two-pass join. P5: `xs |> f` (`f : Go.Slice T -> R`) unifies
    before `conformFirstArg`'s List→Slice conv — same
    eager-lhs class as F3's pipe pin; the direct-apply `f xs`
    works. Document or fix; note the overlap with the pipe
    items and pick one coherent pipe story.

Ownership: internal/check (apply.go conformCallee/checkApply,
expr.go checkLambda/checkBinary/pipeCall, unify.go as needed),
internal/lower (equal.go, pat.go lit-test emit), internal/codegen
(lit rep), testdata/spec fixtures + the gaps entries listed.
diag/codes.go append-only — prefer reusing E0401/E0407-class
codes over minting. Do NOT touch internal/lsp, internal/gomtest,
internal/stdlib, internal/eval (flag if a forced eval touch is
semantically required like wave 13).
```

### 2. CLI go-tool invocation — `gom build -go` — `devin/wave14-cli-gotool`

```
[Rewritten 2026-11-17 (podhmo's B-route pick): the
invocation-point question is pinned to the CLI surface. The
LSP per-change path — temp-pkg compile latency on top of the
CheckOnly recompile, plus async publish/retract — leaves this
wave as a recorded deferral, not a lane deliverable.]

[decision+impl] `gom build` writes generated .go but never runs
the go tool, and `gom lsp` compiles CheckOnly with no output —
so the SourceMap-aware inverse (wave-13 ResolveMatch
exact/drift/ambiguous + GoToolDiagnostics/`gom test` fold) has
no invocation point on either surface. This lane lands the
invocation where output already materializes: an opt-in
`gom build -go` runs `go build` per output package directory
(the runGoTests directory grouping + GoPosIndex construction
is the template) and folds the tool's diagnostics back onto
.gom spans through GoToolDiagnostics (gaps entry
"`go build`/`go vet` のエラーを `.gom` 位置へ戻す経路" + TODO
"LSP: go-tool 診断の取り込み" — the CLI half this lane closes;
the LSP half splits off as its own open entry).

Steps:
(1) Decision draft FIRST: invocation surface = `gom build -go`
    (opt-in — default `gom build` stays transpile-only; a
    mandatory go run would break the no-go.mod usage and slow
    every build). `go vet` stays unfired (vet:-prefixed output
    remains raw text — record that residual). draft → verify →
    verified per docs/decisions/README.md.
(2) Implement: after runBuild's write loop, group written
    inputs per package dir (the runGoTests recipe), build each
    dir's GoPosIndex from inputs + Result.Output source maps,
    run `go build` there, and fold stderr lines via
    GoToolDiagnostics into the report (SourceGo, tool="go
    build"). Invocation failure itself is an honest E0001
    go-tool diag. An -o destination outside the module is not
    special-cased — the go build failure is the report.
(3) Measure `gom build -go` vs plain `gom build` wall time on
    a fixture package; record the number in the decision file.
(4) E2E verify: a compile that passes gom check but trips
    `go build` (e.g. a generated file referencing something the
    module can't resolve, or a post-write corruption in a test
    harness — the fold is what matters) lands on the right
    .gom span; drifted lines and ambiguous positions follow
    ResolveMatch honesty.

Partial honest outcomes ship: flag + fold + measured cost is a
full lane; a recorded "still no honest invocation surface"
with evidence is the fallback.

Ownership: cmd/gom, internal/build (additive helpers only —
reuse, don't fork, the runGoTests machinery). Do NOT touch
internal/check, internal/lower, internal/codegen,
internal/lsp. Ledger edits stay lane-local: the gap row's CLI
half gets the 対応 note; its LSP half moves to a new open
entry (or TODO task) so nothing is falsely checked off.
```

### 3. Hygiene — `devin/wave14-hygiene`

```
Mixed items; one commit per item. Lane 1 owns internal/check
broadly this wave — item (a) is the only check-touching item;
keep it narrow and additive.

(a) [impl] `metasIn(want)` quiet-ification may suppress a legit
    2nd-mismatch: a shared `want` across conform positions can
    swallow a real E0401 on a later arm (gaps entry). Reproduce
    the suppression, narrow the quiet-ification to the positions
    it's meant to cover, fixture both the suppressed-shape fix
    and the still-quiet intended case.
(b) [impl] stdlib fn/let member syms are re-minted without
    `d.Sym` (Sp lost) vs gomimport's reuse — inert today.
    Align the member registration (or record why the inert
    divergence stays); sibling-audit `MemberLet` shape while
    there.
(c) [impl] iface.Surface emits thunk `fn` as `MemberLet`
    (`Type: d.Sig.Ret`) while gomimport registers the full
    `() -> T` sig — inert (no .gomi can declare a thunk body).
    Align or record; check whether the recorded inertness
    assumption still holds under wave-13's type-member table.
(d) [wording] spec §8.5 generic-fn wording vs §8.3: structural
    constraints (`~[]E`, `~map[K]V`) stay E0506-hidden while a
    spec section suggests broader coverage (gaps entry "Go
    ジェネリック関数の扱いが文書内でも異なる"). Fix the wording
    to match reality or record the decision to widen — the
    widening itself is an open-question decision, not a lane.

Ownership: internal/stdlib, internal/iface, spec.md §8.5-
adjacent, internal/check ONLY for item (a) (narrow + additive).
diag/codes.go append-only. Do NOT touch internal/lsp,
internal/gomtest, internal/lower, internal/codegen, internal/eval.
```

### 4. gist-cli byte I/O + `Go.Slice String` boundary — `devin/wave14-gist-cli`

```
[impl] TODO item "Gist CLIのGo境界" (parent [-]): implement
byte I/O and `Go.Slice String`→`List String` to shrink the
remaining example-local adapters. The minimal repro and needed
ops are in docs/sketch/2026-11-03-gist-cli-prototype.md —
read it first; related: P3-2/P3-3 boundary machinery.

(1) Re-survey the gist example's local adapters against the
    current boundary surface (Ptr ops, Go.Map ops, Conv
    materialize, W0501 lint) — several landed after the
    prototype sketch; only implement what's still missing.
(2) Byte I/O: the prototype lists the ops it needs; prefer the
    smallest honest addition (Go-side extern bindings? a narrow
    rt helper?) and record the choice.
(3) `Go.Slice String`→`List String`: check whether conv
    materialize already covers element-wise conversion at the
    boundary; if not, add the conversion at the recorded
    channel (check/apply conform or lower conv site — lane 1's
    surface this wave; keep the change additive and flag the
    overlap).
(4) Shrink the example adapters to what the boundary now
    provides; the example compiles + runs end-to-end.

Ownership: the gist example dir, internal/goimport (bindings),
rt (helpers), internal/check/apply ONLY if the conversion needs
a conform-site change (additive, flag the lane-1 overlap).
testdata fixtures for any new boundary conversion.
diag/codes.go append-only.
```

## Sequential tail (Wave 15 lead)

```
- P4-7 incremental build cache (design.md 5 章 cache keys;
  P4-1 done — no remaining blocker).
- LSP completion/definition/references + incremental sync +
  `$/cancelRequest` + didSave/workspace + loader session cache
  (~48–50 ms/recompile measured wave 12 — a cache is the
  recorded cut).
- `.gomi` `let` member polymorphism (thunk representation
  across imports — wave-13 member-table work may unblock).
- `SpanAt` column/range granularity + codegen per-line re-pin
  (ambiguity erasure) if the LSP surface wants it.
- D-3 README final adequacy, D-4 tutorial go.mod layout — end
  of project per user direction.
```

## Items needing decisions, not implementation

- Structural Go constraints (`~[]E`/`~map`), Go struct
  construction / field writes, external-module stdlib imports,
  3+-result fn *implementation* (v1 wording pinned wave 11),
  `rune`-as-`Char` semantic widening.
- `|?>` wrap-wording configurability — future work.
- Exported-`let` façade / API hardening — deferred alternative
  recorded in boundary-mutation-contract.
- `Result.to_option`/`Option.to_list` — structurally blocked by
  Go import cycles; stays recorded.
- W0701/W0702 lint coverage beyond the scope-out — recorded
  scope, widening is an open question not yet a lane.
- Mutability beyond `Ref`/`Array` — user-declared mutable
  structures still open (lint list is closed builtin set).

## Common instructions (append to every prompt)

```
Before finishing run: gofmt -l . (no output), go vet ./...,
go test ./..., AND sh scripts/check-sumtype.sh (CI gate —
`make check` additionally covers staticcheck+experiments; run
if cheap). Partial probes on `//sumtype:decl` unions are
if-assert chains, never partial type switches. Commit WIP early
and often — commits are the only recovery point. Decisions made
during implementation go to docs/decisions as 状態: draft;
verify with a prototype, write results to docs/sketch, then
update status. Language-rule changes add testdata/spec
fixtures, generated-code changes update testdata/golden,
Go-interop changes add tests in both directions. Concerns go
to docs/problems; spec/implementation/doc discrepancies are
recorded in docs/implementation-gaps.md, never resolved by
silently rewriting the spec. In-code comments are normative
text too — verify them against the mechanism they describe.
Ledger edits (TODO.md / open-questions / implementation-gaps
check-offs) ride in the lane's own PR this wave — mark only
what the lane closed; the round record reconciles at wave end.
Read skills/gom-agent-guide before writing .gom/.gomi files.
Verify every normative doc claim against the code it describes —
doc overclaims have been caught six waves running. Fixtures
carrying raw bytes (CRLF, non-UTF-8) need a .gitattributes
-text mark at creation time. Probe adversarial variants of the
gaps you're fixing, not just the recorded shape — review found
worse failure classes by probing three waves running.
```
