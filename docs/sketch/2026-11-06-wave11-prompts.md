# Wave 11 orchestration prompt drafts

State as of 2026-11-06: Wave 10 integrated — call-honesty
(`36707d8` + merge fix `7ce5a89`), hygiene (`f1f3774`),
gomtest-followup (`a9648a1`), eval-multifile (`e31ff02`),
ledgers (`a81844b`, `a9deb2d`, `c0b8cb9`, `de50975`). Round
record: [2026-11-06-wave10-round.md](2026-11-06-wave10-round.md).
This file drafts the prompts for the next wave, following
`2026-11-05-wave10-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Orchestration notes (Wave 10 lessons)

- Fresh-eyes review caught a blocker in the worker's own domain
  for the **third wave running**: lane 1's `appendArg` kept the
  pre-change `Applied >= Arity` promotion while the checker side
  moved to the flat-prefix rule — `x |> g ~c:7` diverged
  eval/compiled in exactly the class the lane existed to close.
  When a shared convention changes, have the reviewer enumerate
  EVERY consumer, not just the ones the diff touched.
- Semantic merge conflicts beat textual ones — twice: a
  signature change (`bindGoStubs`) vs a new call site written
  against the old one (auto-merged, `go vet` caught it), and a
  generalized guard vs another lane's new `GoSig` literals
  (predicted by the reviewer, resolved at merge). Prompt
  reviewers for post-merge *semantic* breakage, not just
  textual conflicts — it worked both times.
- `git add -A` during merge prep swept a user-side regenerated
  artifact (`stdlib/list/list.go` self-import from an in-dir
  `gom build`) into the merge — `go vet` caught the import
  cycle. Orchestrator lesson: stage conflict paths explicitly;
  treat vet import-cycle errors as suspect-artifact signals.
- Rate-limit death once (a reviewer). Resume-after-reset
  completed cleanly — playbook holds (no WIP salvage needed
  for read-only agents).
- Reviewers again verified *mechanisms*, not just diffs: lane 2's
  "E0410/E0411 are reachable" claim (contradicting the TODO) was
  traced to concrete fixtures and confirmed.

## Dependencies cleared by Wave 10

- **Cross-level labeled args are complete** — `ArgToParam` is a
  flat-domain mapping; `ClassMappedCall` (check/apply.go) is the
  shared declared-prefix classifier consumed by `checkGomApply`,
  `reclassifyCalls`, and lower's `appendArg`. Labels into
  returned-function params eta-expand; eval/compiled pinned by
  `deep_label_pipe`.
- **Nil-able interface first results are admitted** —
  `methodIface` + `ResNilable` (`GoSig`/`CallGo`) + generalized
  `payloadNilGuard`; `(I,error)` derives `Result (Option T)
  GoError`; `nilablePresence` admits `*T` + all interfaces;
  `any` deliberately stays verbatim.
- **Golden harness handles directory cases + test-mode** —
  `GenerateFunc func([]File, CaseOptions) ([]GeneratedFile,
  error)`; per-output `<path>.golden.go` including
  `<member>_test.golden.go` via `case.json {"tests": true}`.
  Stray/duplicate goldens fail deterministically.
- **Package-unit eval exists** — `eval.NewFiles`, basename sort,
  file-blind lazy forcing, per-file `|?>` labels; comparison
  harnesses (`evalDiff`, `evalAnswerFiles`) verify multi-file
  programs.
- **The `?n`-leak fix pattern is fully established** — fresh
  `metaNamer()` + `types.Display` at the report site, one spec
  fixture per observable leak; E0401/E0402/E0403/E0407/E0409/
  E0410/E0411/E0701/E0801 all done. Remaining sweep candidates
  recorded (E0502/E0401-data/E0507).

## Recommended Wave 11 composition — 4 lanes

The wave defines 4 lanes. **Execution under Devin: run at most
2 implementation lanes concurrently** (pool cap — waves 8–10
each hit the limit); lanes 3–4 are queued and launch as slots
free, in table order.

| # | Task | Main areas touched | Worktree | Under-Devin order |
|---|------|--------------------|----------|-------------------|
| 1 | Check residuals: Go nullary fn value + `Go.conv` `gomOnly` Qual blindness + any-hole premature bind + `...any` block-want asymmetry + remaining `.String()` audit | internal/check/expr.go (+ apply-adjacent), internal/goimport/boundary.go | gom-wt-check-residuals | immediate |
| 2 | Spec-wording + tooling hygiene: rune/Char, 3+-result arity, spec-12 panic wording, import-alias/`.gomi`-import wording, root bare `.gomi`, SelfImportPath in-dir footgun, `make check` sumtype sites | spec.md, gomtest, internal/stdlib, cmd/gom, lower/format/codegen/core test walkers | gom-wt-wording-hygiene | immediate |
| 3 | P4-6 LSP foundation | new `internal/lsp` (or similar), build driver, `internal/diag` srcmap, GoPosIndex | gom-wt-lsp | when a slot frees |
| 4 | Emacs gom-mode tree-sitter port | `editor/emacs`, new `tree-sitter-gom/` grammar | gom-wt-emacs-treesitter | when a slot frees |

Overlap notes:

- Lane 1 owns `internal/check/expr.go` wholesale this wave —
  every item lands there or adjacent (`checkGoApply`,
  `convOK`, `checkBlock`, report sites). Lane 2 must not touch
  it; its `.String()` audit item moved INTO lane 1 for exactly
  that reason. `internal/diag/codes.go` is append-only both
  lanes.
- Lane 2 owns `spec.md` + `docs/grammar.ebnf` wording +
  `internal/gomtest` + `internal/stdlib` + `cmd/gom` +
  `internal/format`/`codegen`/`core` **test-side** walkers
  (make-check sumtype sites). If a make-check fix needs a
  non-test production change, flag it — lane boundaries may
  need a micro-negotiation; keep it additive.
- spec.md: lane 1 probably §8.1/§8.5-adjacent (nullary-fn
  wording, conv soundness, any-hole verdict); lane 2 owns all
  other wording edits. Same-file different-section edits merge
  fine, but call out touched sections in completion reports.
- Lanes 3 (LSP) and 4 (emacs) are fully disjoint from 1–2 and
  each other.

## Individual prompts

### 1. Check residuals — `wt/check-residuals` @ gom-wt-check-residuals

```
Five items in internal/check; one commit per item. All are
TODO/gaps-recorded — read the referenced entry first.

(a) [impl] Go nullary function values can't be called: spec
    says Go functions are exposed as values, but
    `let f = e.Error; f ()` is E0401 while `e.Error ()` works
    (problems/2026-11-02-go-nullary-fn-value.md + gaps entry).
    Decide the fix shape: admit nullary Go-fn values through
    the value path (check what CallGo/CallUnknown needs —
    a Go sig with 0 params and the method-value receiver
    capture), or restrict spec wording if the value path is
    structurally wrong. Reproduce first; spec fixture either
    way.
(b) [impl] `Go.conv`'s `gomOnly` judgment doesn't look at
    `Qual` (gaps entry — residual unsoundness):
    `Con{Qual:"geom",Name:"Int"}` (a foreign ADT sharing a
    builtin name) fails the name check so `Go.conv <handle>
    : geom.Int` can be admitted via the `*types.Go` fallback.
    `t.Qual == ""` alone is wrong (qualified `list.List[T]`
    is genuinely convertible). Needs a real "is this a
    Gom-side type" oracle — check what check.TypeDeclSource /
    declaredT / shadowedBuiltin already provide before
    designing one. Reproduce + reject fixture.
(c) [decision+impl] any-hole premature bind: `(_ : String)`-
    style holes get want-side-bound by callee unify in the
    deferred path but not eager — same program, different
    diagnostics (`expected String, got Go.Any` vs clean)
    (problems/2026-11-05-any-hole-deferred-result.md + gaps).
    Decide which reading is correct (want-side acceptance
    bound vs unbound), implement, fixture both paths.
(d) [investigate → maybe impl] `...any`-position block-want
    asymmetry (gaps entry, unconfirmed): `checkBlock` alone
    stamps raw `want` onto the Block node while if/match/
    select/recordLit use `bw` (Go.Any → nil). `fmt.Println
    ({563.33})` produces a `Go.Any`-typed Block whose last
    expr stays Float; build/lower currently pass. Determine
    intended vs accidental; if accidental, align and watch
    for the E0901 invariant the `bw` comment worries about.
(e) [impl] remaining raw `.String()` renderings audit (TODO
    + gaps): E0502 `data["type"]` (expr.go ~363), E0401
    `data["type"]` (~728), E0507 `data["from"/"to"]` (~773 —
    `convOK` only defers top-level metas, so nested metas can
    leak e.g. `Go.conv ([todo ()]) : Go.Slice Int` →
    `List ?17`). E0417 `judgeT` verified concrete — confirm,
    don't touch. Reproduce each leak; fix the reachable ones
    with the established fresh-`metaNamer()` + `types.Display`
    pattern + spec fixture; record why unreachable ones can't
    fire.
```

### 2. Spec-wording + tooling hygiene — `wt/wording-hygiene` @ gom-wt-wording-hygiene

```
Small items across spec/docs/tooling; one commit per item.
None touch internal/check/expr.go (another lane owns it).

(a) [wording batch — one commit] spec-vs-impl wording closes,
    three items from gaps:
    - `rune` → `Char` vs implementation `Int32` (spec §8.3
      correspondence says `rune`/`Char`; goimport basicType
      maps `types.Int32` → `Int32`, only untyped rune
      literals become `Char`). Pick the honest reading —
      probably spec wording (`rune` is `Int32`; `Char` is the
      literal/conv path) — small spec fix + problems note.
    - 3+-result Go fns are hidden by E0501 but spec is silent
      on the arity cap — add the limit to spec §8.3/§8.4
      (admission wording) rather than implementing N-ary.
    - spec §12 says panic output is "2 部構成" unconditionally,
      but `renderTrace` returns raw-only when zero frames
      resolve — wording fix matching design.md §4.8.
(b) [decision+wording] `import alias "path"` and `.gomi`-side
    imports are grammar-accepted but spec-undefined (gaps
    entry; PROVISIONAL gom-import-syntax tag;
    problems/2026-11-01-gom-import-syntax.md +
    2026-11-05-gom-package-imports.md). `.gomi` imports DO
    load mechanically (nested resolution + cycle detection,
    TestGomiSideImport) — the gap is spec definition. Decide:
    define in spec §5/§7 (alias semantics, .gomi import
    surface) or restrict at parse/resolve. Either is a real
    outcome; decision + wording (or rejection) required.
(c) [impl] spec-suite root bare `.gomi` silently ignored (TODO
    nit): same silent-drop class the root side-file lane just
    fixed — extend the load-error reporting to bare `.gomi`
    at the suite root, or document the ignore. Follow the
    2026-11-06-spec-suite-root-bare-side-files decision's
    shape.
(d) [impl] SelfImportPath in-dir build footgun (gaps entry,
    observed biting): `gom build` inside `stdlib/list/` with
    basename input produces a self-importing `package list`
    (SelfPath needs `stdlib/<pkg>/` in the input path; `""`
    fallback treats builtin List refs as module-qualified).
    Options: detect "source declares package P and would
    import P" and reject early (E05xx — fresh code, check
    codes.go first), OR extend SelfPath detection
    (package-decl/go.mod based), OR document. The reject is
    probably right — a self-import is never intentional.
    Careful: the fix must not false-positive on user modules
    that legitimately re-declare the stdlib layout (the
    problems file records that convention).
(e) [impl] `make check` sumtype failures (gaps entry): the
    complete-check gate fails on 5 sites — lower Atom
    snapshot, format parenLed, codegen's two fixture walkers,
    core's set check. Fix the missing enumerations in TEST/
    tool-side code (production semantics unchanged); if a
    site needs a production `default:` arm change, flag the
    overlap in your report — internal/check/expr.go is off
    limits.
```

### 3. P4-6 LSP foundation — `wt/lsp` @ gom-wt-lsp

```
[decision+impl] TODO P4-6 (dep P4-5 done — GoPosIndex +
SourceMap exist). Build the LSP foundation, not the whole
server. Scope decision first: the v1 slice should be —
type/hover display + diagnostics push + UTF-16 position
conversion over a single open-file model, or narrower.
Design inputs from gaps: `//line` drift aliases report lines
to real-but-wrong .gom lines (design 2.2 wants
correspondence-table inverse mapping + ambiguity fallback),
`SpanAt` returns inner-syntax spans not generated lines —
decide whether LSP needs column/range precision or the
existing granularity suffices for v1, and record the
deferred hardening. Protocol: a minimal stdio JSON-RPC loop
(textDocument/didOpen + didChange + hover +
publishDiagnostics) using the compiler's diag internal
repr — output format must stay invisible to the compiler
(AGENTS diag rule); put protocol types in the new package,
no new deps beyond stdlib (no external LSP library unless
it earns the dependency). Verify: a scripted didOpen→hover→
diagnostics round-trip test + a real editor check if
feasible (record which). If the whole loop is too big for a
lane, land the position-mapping layer (UTF-16 + span
inverse) with tests and record the rest as TODO.
```

### 4. Emacs gom-mode tree-sitter — `wt/emacs-treesitter` @ gom-wt-emacs-treesitter

```
[impl] TODO task: `editor/emacs/gom-mode.el` is regex-based
for lack of a grammar (sketch
docs/sketch/2026-11-05-emacs-gom-mode.md). Write
tree-sitter-gom from `docs/grammar.ebnf` — `grammar.js` plus
the external scanner for newline-as-NL insertion (the
sketch analyses why a plain grammar can't express it) —
then port the mode to `treesit` font-lock / imenu / indent
following podhmo/moonbit-mode's structure. Reuse
`editor/emacs/test/`'s ert-font-lock annotations and indent
tests as the regression suite — run them if emacs is on
PATH (`emacs --version`); if not, verify the grammar with
`tree-sitter generate` + `tree-sitter parse` on repo `.gom`
files (testdata/spec, stdlib) and record that coverage
substituted for the ert suite. Grammar coverage priorities:
the full expression/declaration surface, `~label:` args,
`|>`/`|?>` pipes, `..spread`, test blocks, `extern`/`import
go`. Keep the regex mode working as a fallback (feature-detect
treesit availability) — don't delete it.
```

## Sequential tail (when a lane frees, or Wave 12 lead)

```
- P4-7 incremental cache (design.md 5 章 cache keys; P4-1 done).
- gist-cli byte I/O residual ([-] TODO — example-local adapters).
- eval ready-in-declaration-order tie-break (gaps + future
  work): port lower.CheckInitCycles' vardep graph to
  check.Decl — the only way eval can match Go's "earliest
  ready var" pick; mid-size, unblocks the divergence class.
- gomtest `t.Fatalf`-path test infra (TODO): failure-injection
  or subprocess harness for stray/duplicate/1-output fatals.
- Flat `tests:true` `x_test.golden.go` convention (recorded
  future extension in gomtest-tests-option decision).
- `*T` default (auto result/param mapping) + `Go.Any`/`any`
  spellability — an open-questions decision lane; note the
  spellability entry now also gates `(any, error)` declared
  views (named-iface alias is the workaround).
- spec root bare `.gomi` — done if lane 2 lands it.
```

## Items needing decisions, not implementation

- `*T` default; `Go.Any`/`any` spellability (both
  open-questions; a decision lane could take both).
- Structural Go constraints (`~[]E`/`~map`), Go struct
  construction / field writes, external-module stdlib imports,
  3+-result fn *implementation* (lane 2 pins the cap wording
  for v1), `rune`-as-`Char` semantic widening.
- `|?>` wrap-wording configurability — future work.
- Exported-`let` façade / API hardening — deferred alternative
  recorded in boundary-mutation-contract.
- `Result.to_option`/`Option.to_list` — structurally blocked by
  Go import cycles; stays recorded.

## Wave 12 candidates

- P4-7 incremental cache; eval forcing-order vardep port.
- Interop decision lane (`*T`, `Go.Any` spellability) → impl
  lanes the wave after.
- gist-cli byte I/O; gomtest Fatalf infra; flat tests:true.
- D-3 README final adequacy, D-4 tutorial go.mod layout —
  end of project per user direction.

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
