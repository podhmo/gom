# Wave 12 orchestration prompt drafts

State as of 2026-11-06: Wave 11 integrated — check-residuals
(`ca2e0c0`), wording-hygiene (`9178cc8`), lsp (`1ffd6f6`),
emacs-treesitter (`2f2fd35`), ledgers (`55835fd`, `dd3dad8`,
`cac8d8a`, `963d9fd`). Round record:
[2026-11-06-wave11-round.md](2026-11-06-wave11-round.md).
This file drafts the prompts for the next wave, following
`2026-11-06-wave11-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Orchestration notes (Wave 11 lessons)

- Fresh-eyes review caught *normative-doc overclaims* for the
  fourth wave running — this time in the **docs lane**: spec and
  decision text wrote guarantees stronger than the implementation
  twice (generated import alias emission; interface-only cycles
  as E0606). A lane whose job is spec-to-implementation honesty
  can itself write unchecked claims. Prompt wording lanes to
  verify every normative sentence against code before writing it;
  reviewers should diff doc claims vs implementation, not just
  check that docs changed.
- A reviewer produced a *wrong-in-mechanism* fix suggestion
  (lane 4's `treesit-defun-skipper` is a post-move hook, not a
  node filter) — the worker found the real gate mid-fix and
  reported the deviation. Keep routing fix-round instructions
  through the worker, but let the worker choose the mechanism
  when the reviewer's sketch is wrong and require deviations be
  reported.
- `.gitattributes` bit a line-ending-sensitive fixture
  mid-flight: a CRLF corpus entry lost its `\r` to
  `core.autocrlf=input` on first commit. Workers creating
  CRLF / raw-byte fixtures must mark them `-text` at creation.
- Rate-limit death recurred once (a reviewer) —
  resume-after-reset completed cleanly; playbook holds for the
  fifth wave.
- A worker edited the forbidden ledger file again
  (`implementation-gaps.md`, inside a docs commit) — content was
  accurate and landed via squash. Keep the rule; the orchestrator
  ledger pass reconciles.
- The LSP lane's *real-client* check (Eglot batch session)
  caught a markup-negotiation bug the scripted tests missed.
  When a lane adds protocol surface, require a real-client or
  real-peer verification where one exists.
- Merges stayed trivial with partitioned ownership (wholesale
  file + spec-section assignment); only the generated decisions
  index conflicted once — regenerated post-merge, as designed.

## Dependencies cleared by Wave 11

- **Go nullary fn values work end-to-end** — `-> T` applies with
  `()` as a call marker (`CallUnknown` `Applied:0`,
  `pendingApply.alwaysGom` replay on pendingSel, 0-arg `CallFunc`,
  eval bound builtin). spec §8.1.
- **`Go.conv` has a Gom-decl oracle** — `containsGomDecl` over
  `c.tyCons`, recursive at any depth; qualified-Con rejection is
  proven lossless (`boundaryType` never maps qualified `Con`).
- **Deferred any-hole pins are accepted narrowing** — hole metas
  bind `hole := pt`; `unifyDeferredResult` handles consumer-pinned
  result metas (documented monomorphic narrowing).
- **`checkBlock` infers under `bw`** — no more `Go.Any` stamped
  onto Block nodes in `...any` position.
- **The `.String()` audit is closed** — E0401/E0502/E0507 use
  fresh `metaNamer`+`Display`; E0502 proven unreachable; E0417
  concrete. The `?n`-leak class has no known remaining site.
- **The import surface is spec'd** — `import alias "path"` +
  `.gomi` imports defined §5/§7; residual gaps recorded (alias
  emit conditional; interface-only E0203). `gomstd.SelfPath`
  deleted — `gomimport.EntryPath` (go.mod-derived) makes
  self-import structurally impossible.
- **LSP foundation exists** — `internal/lsp` + `gom lsp` +
  `OffsetUTF16` + protocol gates + panic isolation. Substrate for
  multi-file/folding work; v1 limits recorded (single-file
  compile → sibling E0201).
- **tree-sitter-gom exists** — full-EBNF grammar + stateless NL
  scanner + treesit `gom-mode` (regex fallback). `.gitattributes`
  guards CRLF corpus.
- **gomtest silent-drop class closed** — root bare side files
  AND bare `.gomi` are load errors; non-case subdirs stay ignored.

## Recommended Wave 12 composition — 4 lanes

The wave defines 4 lanes. **Execution under Devin: run at most
2 implementation lanes concurrently** (pool cap — waves 8–11
each hit the limit); lanes 3–4 are queued and launch as slots
free, in table order.

| # | Task | Main areas touched | Worktree | Under-Devin order |
|---|------|--------------------|----------|-------------------|
| 1 | Codegen/import-boundary residuals: bound-name alias emit + iface-only cycle diag + `.gomi` no-source-registered + unknownCall silent-drop + §8.1 contrast | internal/codegen, internal/gomimport, internal/resolve, internal/lower/expr.go (unknownCall), spec §5/§7/§8.1 | gom-wt-codegen-boundary | immediate |
| 2 | LSP multi-file package units | internal/lsp, internal/build compileOptions, internal/diag srcmap (read-mostly) | gom-wt-lsp-multifile | immediate |
| 3 | eval forcing-order vardep port | internal/eval, vardep analysis (port from internal/lower CheckInitCycles) | gom-wt-eval-forcing | when a slot frees |
| 4 | Interop decisions: `*T` default + `Go.Any`/`any` spellability (decision+prototype, no production impl) | spec §8.3/§8.4, docs/decisions, goimport mapping (read + prototype only) | gom-wt-interop-decisions | when a slot frees |

Overlap notes:

- Lane 1 owns `internal/codegen`, `internal/gomimport`,
  `internal/resolve`, and the `unknownCall` site in
  `internal/lower/expr.go`, plus spec §5/§7/§8.1. Lanes 2–4 must
  not touch these. `internal/diag/codes.go` is append-only for
  all lanes.
- Lane 2 owns `internal/lsp` and may extend `internal/build`'s
  compileOptions *surface* (additive only — a package-unit entry
  point; do not change single-file semantics). If it needs
  `internal/diag` srcmap changes, keep them additive.
- Lane 3 owns `internal/eval` + its new vardep home. It READS
  `internal/lower`'s `CheckInitCycles` — extract, don't modify;
  if a shared lift makes sense, flag it rather than refactoring
  lower (lane 1 touches one lower site).
- Lane 4 is decision+prototype only — spec §8.3/§8.4 wording +
  decisions docs; it may patch goimport/check in a *prototype or
  test-only* path but must not land production behavior changes.
  §8.3/§8.4 is disjoint from lane 1's §5/§7/§8.1.
- spec.md: lane 1 → §5/§7/§8.1; lane 4 → §8.3/§8.4-adjacent.
  Call out touched sections in completion reports.

## Individual prompts

### 1. Codegen/import-boundary residuals — `wt/codegen-boundary` @ gom-wt-codegen-boundary

```
Five items around generated-code honesty and the import
boundary; one commit per item. All are TODO/gaps-recorded —
read the referenced entry first (docs/implementation-gaps.md,
TODO.md — read-only, do not edit).

(a) [impl] Generated import alias is emitted only when the bound
    name differs from the path base (gaps entry + TODO; wave-11
    review F-1): `import "example.com/m/lib"` binding `lib` while
    `lib/` declares `package other` emits unqualified
    `import "example.com/m/lib"` — `lib.X` references fail
    `go build`. Options from the TODO: always emit `name "path"`,
    or pipe the real package-clause name through the loader to
    codegen and alias on mismatch. Pick one and implement —
    golden churn is expected either way; update testdata/golden
    and record the choice in a decision. Check interactions:
    `import go` aliases, `import alias "path"` (now spec'd §5),
    self-imports (EntryPath made them impossible — verify your
    change keeps that property), and `.gomi` member types that
    embed import paths.
(b) [decision+impl] Interface-only import cycles report E0203
    without a cycle path (gaps + TODO; wave-11 review F-2):
    `.gomi`-only cycles surface as a plain "cyclic Gom import"
    load failure nested under E0203 while `.gom`-edge cycles get
    E0606 + `data.cycle`. Decide: include `.gomi` edges in the
    impl scan for E0606 parity (the loader's in-flight guard
    needs to carry the cycle chain — design it), or keep E0203
    and word it honestly. Either way, fixture the outcome
    (spec fixture or gomtest dir case).
(c) [investigate → impl] `.gomi` package-clause mismatch E0601
    emits `cannot resolve span ... no source registered` (gaps
    entry, unconfirmed): reproducer — `mismatch/mismatch.gom`
    declares `package other` with `other.gomi` alongside. Trace
    the diag emission path: the span's file may be registered
    under a different spelling than the lookup, or the `.gomi`
    source never registered at all in that path. Fix the
    emission or record why it's structural; fixture either way.
(d) [impl] lower `unknownCall` silently drops 0-arg non-`-> T`
    calls (TODO nit): `len(args)==0 && !-> T` falls through to
    `callFuncChunks(fn, nil)` returning bare `fn`. Unreachable by
    checker contract today (Applied:0 exists only for `-> T`)
    but would silence a future checker bug — make it a defensive
    `l.err` instead. Also add the deferred `alwaysGom` unit test:
    `let m = x.Error; m ()` on a pendingSel (the replay branch
    has no pin — wave-11 review nit-1).
(e) [wording] spec §8.1 `-> T`/`f ()` marker vs `Unit -> T`
    `f ()` (TODO nit): in the same spec item, show the pair —
    `f ()` on `Unit -> T` passes a real Unit argument
    (`f(struct{}{})`), on `-> T` it's the call marker (`f()`).

Ownership: internal/codegen, internal/gomimport,
internal/resolve, internal/lower/expr.go (unknownCall site
only), spec §5/§7/§8.1. diag/codes.go append-only.
internal/check/expr.go is NOT yours this wave — if a fix needs
check-side changes, prefer resolve/loader-side and flag the
boundary in your report.
```

### 2. LSP multi-file package units — `wt/lsp-multifile` @ gom-wt-lsp-multifile

```
[impl] TODO task (wave-11 lsp residual): `gom lsp` compiles each
open file in isolation, so same-package sibling symbols report
E0201 that a package-level `gom check` wouldn't emit (gaps entry
+ doc.go recorded limitation).

Extend the server to compile at package granularity: when a
document opens, load the package's sibling `.gom` files (the
loader already resolves package imports — check what
`compileOptions(input{path,src}, true)` does with an in-memory
src vs on-disk siblings; eval.NewFiles / the build driver's
package-unit path is the existing model). didOpen/didChange of
any member must invalidate and republish diagnostics for ALL
open documents in the package — a change in b.gom can flip a
diagnostic in a.gom.

Decide and record the consistency model: in-memory doc versions
shadow disk; unopened siblings read from disk (the standard
LSP overlay shape). Keep the single-file path when there's one
member.

Watch (reviewer-flagged for the v1 code):
- `convertDiagnostic`'s `Span.File != d.path` drop was written
  for single-file — diagnostics whose spans point at sibling
  files must now publish per-file, keyed to each document's URI.
- Hover across sibling symbols should just work if the package
  compiles — verify with a test (hover on a use whose decl is in
  a sibling file).
- A sibling with syntax errors: the package compile degrades —
  make sure diagnostics still publish for the healthy files and
  the error lands on the right file.
- Per-change compileOptions/`go list` session cost is a recorded
  unknown — measure roughly (e.g. timing in a test log) and
  record.

Tests: didOpen(a,b)→diagnostics-in-both, didChange(b) flips a's
diagnostics, didClose keeps the other's set, hover cross-file.
If a real-editor re-check is cheap (the v1 lane used Emacs+Eglot
batch), re-run it; otherwise record scripted-only.

Stretch (only if the package unit lands early): sketch — don't
implement — the SourceMap-aware inverse the go-tool-diagnostics
task needs (`//line` drift, SpanAt granularity; gaps entries).
```

### 3. eval forcing-order vardep port — `wt/eval-forcing` @ gom-wt-eval-forcing

```
[decision+impl] eval's initialization order can diverge from
Go's ready-in-declaration-order pick (gaps entry + open-questions
future work; eval-package-unit decision §6 documents it as a
constraint): eval drives lazily by forcing order while Go picks
the earliest-declared ready var. Observable through initializer
side-effect order — host-call results can diverge if
state-dependent; `test` decls also run at declaration position
in eval vs after init completes in compiled output.

The recorded fix path: port `lower.CheckInitCycles`' vardep
graph onto `check.Decl`s so eval can compute the same
ready-set frontier Go uses. That port is the lane — mid-size,
and the only way this divergence class closes.

Steps:
(1) Extract/replicate the vardep graph over `check.Decl` — look
    at `internal/lower`'s `CheckInitCycles`; reuse or lift the
    analysis if it's shareable rather than copying (but don't
    refactor lower — another lane touches a lower site; keep
    your side additive).
(2) eval's top-level forcing picks earliest-ready per the graph
    instead of pure lazy forcing — preserve laziness for the
    dependency-driven part; only the ready-set ordering changes.
    `test` decls run after init per the same model.
(3) Comparison tests in the evalDiff/evalAnswerFiles style:
    programs with independent ready vars interleaved between
    dependency chains (side-effect order via a host counter or
    printed log), plus a `test`-decl ordering case.
(4) Decision + verification sketch.

If the port reveals the divergence is deeper than ordering
(e.g. cycle semantics differ structurally), stop and record the
finding — don't force a wrong fix.
```

### 4. Interop decisions — `wt/interop-decisions` @ gom-wt-interop-decisions

```
[decision+prototype, NO production impl] Two open-questions
items that gate next-wave implementation lanes. Deliverables:
two decisions (draft → prototype → verified → status update),
spec wording for whatever is adopted, open-questions entries
resolved, and implementation TODOs filed for next wave. A
"don't do it" outcome is real — record why.

(a) `*T` default mapping (open-questions; the Ptr-ops decision
    2026-11-05-ptr-ops-surface adopted the `Ptr` module, so
    `Option (Ptr T)` extern is no longer the only nil-safe
    path): the remaining question is the automatic `*T`
    result/param mapping — `Ptr T` (Go-like: nil panics on
    get/set, ergonomic) vs `Option (Ptr T)` (safe, verbose).
    Decide the default for derived views AND for extern
    declarations. Check interaction with wave-10's nilable-iface
    work: `ResNilable` already plumbs interface results —
    `*T` is the other nilable class; does the same mechanism
    apply or does `*T` want its own? Prototype: derive both
    views over a real Go API returning `*T` in a non-Result
    position (a lone `*T` or `(*T, non-error)` shape —
    `(*Regexp, error)` cases already take the Result path) and
    compare call-site ergonomics + nil handling in a scratch
    program.
(b) `Go.Any`/`any` spellability (open-questions): `any` can't be
    written in `.gom`/`.gomi` signatures — `Go.Any` exists but
    `goTypeArity` only admits `Go.Slice`/`Go.Map` constructors;
    P3-7 observed `json.Decoder.Token` returning only `Go.Any`
    as the sole stdlib decode path. Wave-10 recorded that this
    also gates `(any, error)` declared views (named-iface alias
    is the workaround). Decide: admit `any`/`Go.Any` as a
    first-class type spelling (where — signatures only or
    everywhere?), keep `Go.Any`-only spellable in declaration
    positions, or neither. Trace the parse/type-name-resolution
    path first — what does `Go.Any` vs `any` resolve to and
    where would each be admitted. Prototype: the smallest patch
    that lets the chosen spelling resolve in an extern or
    `.gomi` signature — enough to exercise the downstream
    admission/conversion paths (boxing, Option views, conv).

Report proposed ledger changes (open-questions items resolved,
TODO entries for the impl lanes, gaps updates) — don't edit the
protected files.
```

## Sequential tail (when a lane frees, or Wave 13 lead)

```
- P4-7 incremental build cache (design.md 5 章 cache keys;
  P4-1 done — no remaining blocker).
- gist-cli byte I/O residual ([-] TODO — example-local adapters;
  prototype doc lists needed ops).
- gomtest `t.Fatalf`-path test infra (TODO): failure-injection
  or subprocess harness for stray/duplicate/1-output fatals.
- flat `tests:true` `x_test.golden.go` convention (recorded
  future extension in gomtest-tests-option decision).
- `.gomi` type members not on the module member table (gaps —
  `internal/stdlib` skips MemberType; gomimport side works).
- `metasIn(want)` quiet-ification over-suppression (gaps —
  shared want object across conform positions can swallow a
  legitimate 2nd-arm E0401; narrow reachability kept it
  accepted, but the suppression class is recorded).
- LSP go-tool diagnostic folding (needs the SourceMap-aware
  inverse — see lane 2 stretch) + completion/definition/
  references + incremental sync + `$/cancelRequest`.
- D-3 README final adequacy, D-4 tutorial go.mod layout —
  end of project per user direction.
```

## Items needing decisions, not implementation

- Structural Go constraints (`~[]E`/`~map`), Go struct
  construction / field writes, external-module stdlib imports,
  3+-result fn *implementation* (v1 wording pinned in wave 11),
  `rune`-as-`Char` semantic widening (problems note exists).
- `|?>` wrap-wording configurability — future work.
- Exported-`let` façade / API hardening — deferred alternative
  recorded in boundary-mutation-contract.
- `Result.to_option`/`Option.to_list` — structurally blocked by
  Go import cycles; stays recorded.
- W0701/W0702 lint coverage beyond the scope-out (alias-bound
  callee, thunk/closure capture) — recorded scope, widening is
  an open question not yet a lane.

## Wave 13 candidates

- `*T` / `Go.Any` implementation lanes (following the wave-12
  decisions, if adopted).
- P4-7 incremental cache.
- gist-cli byte I/O; gomtest Fatalf infra; flat `tests:true`;
  `.gomi` type-member table.
- LSP go-tool diagnostics + completion/definition/references.
- D-3 README final adequacy, D-4 tutorial go.mod — end of
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
.gom/.gomi files. Verify every normative doc claim against the
code it describes — wave-11 review caught two spec/decision
overclaims in a docs lane. Fixtures carrying raw bytes (CRLF,
non-UTF-8) need a .gitattributes -text mark at creation time.
```
