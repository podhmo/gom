# Wave 5 orchestration prompt drafts

State as of 2026-11-05: Wave 4 integrated — P2-2c (aggregate init
cycles, `96e6ec9`), P3-5 (extern overrides, `0bbcfce`), P3-4 (Go
method calls + `_` placeholder, `3733255`), V-6 (multi-file packages +
`.gomi` imports, `32650f7`). Round record:
[2026-11-05-wave4-round.md](2026-11-05-wave4-round.md). This file
drafts the prompts for the next wave, following
`2026-11-05-wave4-prompts.md`. Always append the "Common instructions"
block to each prompt.

## Dependencies newly cleared by Wave 4

- **P5-1**: P2-2 is `[x]` (P2-2c was the last open child) — unblocked.
  Note: `go`/`select` are already scaffolded — `syntax.GoExpr`/
  `SelectExpr`/`SendArm`, check direction checks, lower's
  `selectExpr`, eval hard-fails with CodeUnsupported. Missing is the
  `Chan.*` operation surface, `Task`, Go `chan` type mapping, and the
  eval boundary decision.
- **P3-6**: P3-3 done, P3-2 still `[-]` (named-type/interface edge
  cases). Bundle P3-2 residual + P3-6 in one session (same conv/check
  paths); P3-7 follows next wave.
- **P2-4c**: P2-2c merged, so lower/codegen is free again — but keep
  it sequential while P5-1 touches lower/codegen.
- **V-6 residual task**: V-6 merged — new.
- **Method-chain parse bug** (`x.M(1).N(2)`): P3-4 merged; `syntax` is
  otherwise untouched by the Wave 5 slots below, so it can run in
  parallel now.

## Recommended Wave 5 composition (4 parallel, disjoint areas)

| # | Task | Main areas touched | Worktree |
|---|------|--------------------|----------|
| 1 | P5-1 concurrency completion (`go`/`Chan`/`select`/`Task`) | check chan ops, lower/codegen emission, eval boundary, spec 10 | gom-wt-p5-1-concurrency |
| 2 | P3-2 residual + P3-6 + conv bugs | check conv/type rules, codegen calls, interop tests | gom-wt-p3-2-p3-6-interop |
| 3 | V-6 residuals (pkg.Type.Ctor, import cycles, dump, name collisions) | resolve member tables, gomimport, cmd/gom, build output | gom-wt-v6-residual |
| 4 | Small fixes + test hardening | syntax (method chain), codegen literals, testdata, diagnostics | gom-wt-hardening |

Overlap notes:

- Slot 4 owns `internal/syntax` (method-chain parse fix). P5-1 should
  not need parser changes — `go`/`select`/`send` already parse. If it
  does, that is a merge-order flag for the orchestrator.
- Slot 4 owns eval `..e` spread ONLY if P5-1 leaves eval's
  `CodeUnsupported` boundary as-is; if P5-1 implements eval
  concurrency, move the spread fix to the sequential tail (same
  `eval.go` area).
- Slot 2 owns the conv paths (`ConvBoxAny`, `{Go.conv}` validation) —
  keep the Float-inferred-int literal fix in slot 4 (codegen literal
  emission, not conv).
- E0415 follow-up item ③ (select channel operand → E0401 instead of
  E0415) belongs to P5-1's select check code — slot 4 handles items
  ①②④⑤ only; ③ goes to the sequential tail.
- P4-6 LSP stays deferred (consume SourceMap gaps first — same
  reasoning as Wave 4).

## Individual prompts

### 1. P5-1 — `wt/p5-1-concurrency` @ gom-wt-p5-1-concurrency

```
Complete P5-1 of the Gom compiler. TODO.md P5-1 is authoritative;
spec chapter 10 and two adopted decisions define the semantics
(concurrency-model: `go`/`Chan`/`select` only, recv-on-closed gives
None, match-style exhaustiveness for receive arms; select-receive-arms
for the arm syntax). Much is scaffolded: syntax.GoExpr/SelectExpr/
SendArm exist, check validates arm direction and channel operands,
lower emits select via selectExpr, `for x in ch` over channels works,
Chan/RecvChan/SendChan/Task constructors exist in types/builtin.go.
Missing pieces to implement or bound: (a) the `Chan.*` operation
surface (Chan.make/send/recv/recvOnly/sendOnly/close — decide builtin
vs. stdlib .gom as a draft decision); (b) `Task a` with
Task.spawn/Task.await including panic re-throw on await; (c) Go
`chan`/`<-chan`/`chan<-` mapping to Chan/RecvChan/SendChan per spec
8.5's table, including `<-chan struct{}` (ctx.Done()) — handle or
record the boundary in implementation-gaps.md; (d) the eval boundary:
eval currently fails go/select with CodeUnsupported — draft a
decision on whether eval runs real goroutines (the Go runtime is
available) or documents concurrency as compiled-only, then verify.
Go-interop + language-rule changes: spec fixtures for each rule in
spec 10, golden updates, eval-vs-compiled comparisons only for
deterministic programs (buffered channels, fixed order — no timing
races). Dependency: P2-2 done.
```

### 2. P3-2 residual + P3-6 — `wt/p3-2-p3-6-interop` @ gom-wt-p3-2-p3-6-interop

```
Two-phase task in one session. Phase 1 — finish P3-2 (`[-]` in
TODO.md): audit what remains of named-type/interface handling (read
the P3-2 entry, docs/implementation-gaps.md named-type items, and
prior sketch reports; produce the concrete residual list before
coding). `Go.conv` explicit conversion and go/types assignability are
the spec'd semantics. Phase 2 — P3-6: Go calls Gom. Generated APIs
must be exercised from Go-side tests (a scratch go.mod calling the
generated package, as testdata/multifile does), including ADT
invalid-value (`nil`) panics per spec 8.6/8.7. Bundle the two
conv-path bugs from TODO.md — they live in the same code: (i)
`{Go.conv}` member-style spelling reports E0901 "member builtin is
not an expression" — the cause is the conv validation path, not
method classification (P3-4's decision has context); (ii) numeric
literals to Go.Any lose the concrete Gom type (`%T` gives `int`, not
`int64`) — materialize the Gom numeric type before boxing in
ConvBoxAny, with execution regressions (literal, binding, arithmetic,
Go-side type switch). Go-interop change: both directions, nil,
slices, named types, interfaces. Dependencies: P3-1 done, P3-3 done.
```

### 3. V-6 residuals — `wt/v6-residual` @ gom-wt-v6-residual

```
Implement the "V-6 残件" TODO entry (four items): ① `pkg.Type.Ctor`
member spelling on imported Gom types — needs a per-type member
table in resolve (record the design as a draft decision); ② mutual
`.gom`↔`.gom` import cycles currently surface only as `go build`
import cycles — detect them in internal/gomimport/the loader with a
proper diagnostic, and document the cycle rule for spec §7 (record
the spec wording in a decision; do not silently edit spec); ③
`gom dump` on a multi-file package currently emits misleading E0201
for sibling members — make it package-aware or give a clean
diagnostic; ④ same-named `test` blocks in sibling files generate
duplicate Go test functions — apply deterministic collision-free
naming (the file-tag convention from lower.pkgTmp is the precedent).
Also pick up the two same-area gap entries: `gom build` writing
output when E0601 `.gomi` conformance fails after codegen (make the
write atomic or record why not), and exported `let foo` vs `type
Foo` colliding in generated Go even single-file (naming decision).
Finally audit the .gomi member-surface gaps (let members surfacing
without matching external form; type members absent from the module
member table; SelfImportPath convention) — fix or record each.
Dependencies: V-6 merged.
```

### 4. Small fixes + hardening — `wt/hardening` @ gom-wt-hardening

```
A batch of small, mostly-additive fixes — land them as one branch,
each with its own regression test. Ordered checklist:
(a) [parser] Method chain `x.M(1).N(2)` mis-parses as a select on an
arg tuple (problems/2026-11-05-method-chain-select-binds-args.md) —
fix binding so chained calls parse left-associatively; keep the
single-call path identical (golden risk: check testdata for shape
changes).
(b) [codegen] Integer literal binding inferred as Float is emitted as
Go `int` (`fn f () -> Float = { let n = 1; n + 0.5 }` fails `go
build`) — carry the lowered binding type into codegen literal
emission; add an execution regression.
(c) [eval] eval cannot evaluate `..e` spread inside Go-call arguments
(check.Spread unsupported there) — evaluate it like the Gom-call
spread path or record the boundary.
(d) [tests] SourceMap/GoPos coverage per TODO: multi-.gom inputs
(real since V-6), same-name relative paths, multi-line chunks,
`file:line:` parse edges.
(e) [tests] stdlib freshness gate: recompile the `.gom` sources and
compare to committed `*.go`/`*_test.go`.
(f) [tests] stdlib boundary helpers (result.FromGo/AssertOk/Unwrap/
ToGo, option.FromGo, list FromSlice/ToSlice independence), nil Option
pin, E0505_list_spread_mid and stdlib_partial fixtures, embed list vs
internal/stdlib.packages sync guard.
(g) [diagnostics] E0415 quality follow-up items ①②④⑤ only —
item ③ (select operand) is owned by the P5-1 slot, skip it.
(h) [infer] V-4 leftovers: deterministic witness tie-break, minimum
decl count for the infer sweep, parseWant `->` rejection.
Dependencies: none (merge order handles any overlap).
```

## Sequential / smaller tasks (Wave 5 tail or Wave 6 lead)

```
- P2-4c unguarded-step EscapeCall fusion + pipeRest span fix (lower
  pipe fusion — deliberately sequential while P5-1 touches
  lower/codegen; P2-4b review F5/F6).
- E0415 item ③: select channel operand gives E0401 not E0415 when
  builtin Chan is shadowed — after P5-1 merges.
- Package-wide checkInitCycles: analysis is file-local but Go
  initialization is package-wide — needs the init-order spec
  decision below first, then implementation pairs with the V-6
  residual follow-up lane.
- {Go.conv} remaining subcases beyond the slot-2 fix (if any
  surface).
```

## Items needing decisions, not implementation

- **Package initialization order** (new, now load-bearing): spec has
  no section defining init order for a multi-file package — E0907's
  Spec: reference is ungrounded and checkInitCycles can't go
  package-wide until the semantics exist. Draft the spec section
  (Go-consistent order? declaration order? dependency order?), then
  implement.
- **Multi-file eval** (new): eval.New takes a single check.File;
  post-V-6, eval can't see sibling-package `let`s. Decide whether
  eval gets package-unit inputs or records the boundary.
- Standard bindings home: the extern mechanism is implemented but
  the default override list (`(T,bool)` → Option etc.) has no
  decided home — open-questions holds it; P3-7's real-world audit is
  the decision input.
- Any-hole cute premature-bind diagnostic divergence (from P3-4
  review) — small semantics question.
- Carried from Wave 4: Float constant-folding divergence; named-
  variant `.f` on the wrong variant; structural Go constraints
  (`~[]E`/`~map`); `*T` default; `.gomi` enforcement beyond
  build/check; checkInterfaces asymmetry.

## Wave 6 candidates (after Wave 5)

- P3-7 (os/io/strconv/encoding/json real programs) once P3-2/P3-6
  close; standard-bindings task likely lands here informed by P3-7.
- P5-2 (mutable send/capture linter warnings) once P5-1 lands; P5-3
  (generated-code perf) after P4-8/P5-1.
- P4-6 LSP (SourceMap-aware inverse mapping per P4-5 gaps),
  P4-7 incremental cache (iface.Hash consumer).
- Package-init-order implementation + eval `..e` spread leftovers.
- D-3 README final adequacy, D-4 tutorial go.mod layout — still
  end-of-project.

## Common instructions (append to every prompt)

```
Before finishing run: gofmt -l . (no output), go vet ./...,
go test ./... Decisions made during implementation go to
docs/decisions as 状態: draft; verify with a prototype, write results
to docs/sketch, then update status. Language-rule changes add
testdata/spec fixtures, generated-code changes update
testdata/golden, Go-interop changes add tests in both directions.
Concerns go to docs/problems; spec/implementation/doc discrepancies
are recorded in docs/implementation-gaps.md, never resolved by
silently rewriting the spec. Do not edit TODO.md,
docs/open-questions.md, docs/implementation-gaps.md, or
TODO-archive.md — report proposed ledger changes in your completion
report and the orchestrator will apply them. Read
skills/gom-agent-guide before writing .gom/.gomi files.
```
