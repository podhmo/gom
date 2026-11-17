# Wave 15 orchestration prompt drafts

State as of 2026-11-17: Wave 14 lanes out as PRs —
go-any residual (#16), CLI go-tool (#17), hygiene (#18),
gist-cli byte I/O (#19), round record (#20). All unmerged;
podhmo merges. Lane 1 below is the direct continuation of
#16's pinned Go.Any residuals — rebase onto it if still open,
or note the assumption.
This file drafts the prompts for the next wave, following
`2026-11-17-wave14-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Execution model (unchanged from wave 14)

Quota constraints: **no child sessions this wave** — one Devin
session executes every lane sequentially. Review is delegated to
Devin Review on the PR instead of fresh read-only reviewer
sessions. Consequences:

- **One lane = one branch = one PR.** Lanes stay disjoint by
  surface so each PR reviews cleanly; the user merges.
- **Ledger edits move into the lane PR.** TODO.md /
  gaps / open-questions check-offs for a lane's own work ride in
  its PR. Mark only what the lane itself closed.
- **Sequential order below.** Ownership notes remain as surface
  documentation for review, not conflict avoidance.

## Orchestration notes (Wave 14 lessons)

- **Rebase discipline**: a lane's commits once landed on the
  previous lane's branch — check `git log origin/main..HEAD`
  matches the lane before pushing; keep every lane PR's diff vs
  main disjoint from sibling lanes.
- **Decision lanes need podhmo-visible drafts early**: lanes 2–4
  below are decision-heavy — open the `状態: draft` decision file
  before implementing so the PR shows the choice, not just code.
- **`gom fmt -check` on edited `.gom`** — wave 14 found the
  example file unformatted at lane end.
- **Backtick in Go string fixtures**: raw-string fixture sources
  can't contain `` ` `` — quote types with quotes, not backticks.
- **Review-driven spec-rule fixes still need golden + reverse
  interop coverage** — the AGENTS.md dual-direction rule was the
  top wave-14 review finding.

## Recommended Wave 15 composition — 4 sequential lanes

| # | Task | Main areas touched | Branch | Order |
|---|------|--------------------|--------|-------|
| 1 | `Go.Any` residual tail — widen or close F1–F3/P3–P5 | internal/check (apply/expr/unify), internal/lower (pat/equal), internal/codegen, testdata/spec | `devin/wave15-go-any-tail` | first — continues #16's pins |
| 2 | Structural-constraint generics (`~[]E`, `~map[K]V`, union constraints) — decision+impl | internal/goimport (E0506 classification, instantiation), internal/check call sites, spec §8.3/8.5 | `devin/wave15-structural-generics` | second |
| 3 | Go struct construction — composite literals / field writes (decision+impl) | spec §8.3/8.5, internal/goimport, internal/lower+codegen or check depending on surface | `devin/wave15-struct-construction` | third |
| 4 | LSP go-tool diagnostics — invocation point (decision+impl) | internal/lsp, cmd/gom, internal/build (additive reuse of wave-14 `gom build -go` machinery) | `devin/wave15-lsp-gotool` | fourth |

Overlap notes:

- Lanes 1 and 2 both touch `internal/check` — lane 1 owns
  conform/lambda/pipe sites, lane 2 owns call-site
  instantiation + E0506 classification; sequence 1 → 2 and
  keep lane-2 changes additive.
- Lane 3 may touch syntax (composite-literal spelling) — if it
  does, it's a bigger surface; a "declared-constructor-only"
  outcome (no syntax change) is a legitimate lane result.
- `internal/diag/codes.go` is append-only for all lanes.
- spec.md: lane 1 → §8.3-adjacent only if a residual resolution
  changes promises; lane 2 → §8.3/§8.5 generic-fn wording;
  lane 3 → §8.3/§8.5 construction/field wording. Call out
  touched sections in the PR body.

## Individual prompts

### 1. `Go.Any` residual tail — `devin/wave15-go-any-tail`

```
[impl] Wave-14 lane 1 (PR #16) fixed P1/P2 and pinned
F1–F3/P3–P5 as honest E0401s (docs/implementation-gaps.md
entries 1326–1391 — verify against the merged #16 state).
This lane resolves each pin: widen where the mechanism
supports it, or record the restriction as intentional.

(a) F1 — lambda result `want` threading: `let f : Int -> Go.Any
    = fn x -> 1` rejects because `checkLambda` mints fresh `?r`
    and binds it to `Int` before `unifySub(Go.Any, ?r)` runs.
    Pass `want` into `checkLambda` so `Int -> Go.Any` reaches
    the body conform (same channel as `fn f (x:Int) -> Go.Any`
    declared-sig path). Probe: nested lambda `fn x -> fn y -> 1`,
    lambda in pipe `1 |> (fn x -> x)`, lambda under `match` arm.
(b) F2 — generalize ctor pin beyond Con-vs-Con: `id (Some 1)`
    (`id : a -> a`, want `Option Go.Any`) skips pin because ret
    is shared `?a`; `pkg.NewBox v` (`*types.Go` ret) same shape.
    Extend the pin to meta/`types.Go` returns where the want
    carries `Go.Any`, or record why Con-vs-Con is the honest
    boundary.
(c) F3 — pipe-arg ctor pin: `Some 1 |> f` (`f : Option Go.Any
    -> R`) settles `x` before `f` is known so `conformCallee`
    sees `Option num`. Decide: defer pipe-arg settling until
    callee known, or two-pass. Note the same eager-lhs class
    as P5's `xs |> f` (`f : Go.Slice T -> R` — type unify
    precedes `conformFirstArg`'s List→Slice conv while `f xs`
    direct-apply works) — pick one coherent pipe story for
    both.
(d) P3 — `match o { Some 42 -> }` on `Option Go.Any`: lit test
    emits `case 42` vs boxed `any(int64(42))` — unreachable.
    Materialize the lit against the boxed rep or emit honest
    deadness diagnostic. Probe: `Some "s"`, negative lit,
    guard-arm variant `Some x if x == 42`.
(e) P4 — any/concrete mixed if-arms under bare `Go.Any` want
    are order-dependent (`1` first → num meta bind → E0401;
    `v : any` first → conform passes). Decide: order-independent
    join (two-pass or Go.Any-aware join), or pin the recorded
    order-dependence as spec-visible behavior.

Per-item calls go in the PR body + gaps entries. Items that
stay rejected keep their E0401 pins with a decision note.
Ownership: internal/check (apply.go conformCallee/checkApply,
expr.go checkLambda/pipeCall/if-join, unify.go), internal/lower
(pat.go lit-test emit), internal/codegen (lit rep for (d)),
testdata/spec fixtures + the listed gaps entries. diag/codes.go
append-only — prefer reusing E0401-class codes. Do NOT touch
internal/lsp, internal/gomtest, internal/stdlib.
```

### 2. Structural-constraint generics — `devin/wave15-structural-generics`

```
[decision+impl] Open question "Go のジェネリック関数のうち
`any`/`comparable` 以外の制約を持つものをどう扱うか": P0-5
showed the strict `any`/`comparable` boundary hides nearly all
of `slices`/`maps`/`cmp` (`slices.Contains` is `[S ~[]E,
E comparable]`, `maps.Keys` is `[M ~map[K]V, K comparable]`,
`cmp.Compare` is `[T cmp.Ordered]` — a union constraint).
Wave-14 lane 3 aligned spec §8.3/§8.5 wording with the hidden
state; this lane decides whether the boundary widens.

(1) Survey first: enumerate which stdlib symbols sit behind
    each constraint class (`~[]E`, `~map[K]V`, union/Ordered,
    `~int`-style basic unions, named constraints like
    `cmp.Ordered`, `slices`.Order's own aliases) via
    `go/types`' type-set oracle — the callable fraction is the
    decision's payoff number. Write it to docs/sketch.
(2) Decision draft: the honest options are (a) keep E0506
    hidden (status quo), (b) admit structural constraints by
    instantiating the type parameter at the call's concrete
    arg (`S := []E` for `~[]E` — Go allows a named or unnamed
    satisfying type), (c) admit only the `~` classes whose
    Gom-side rep is fixed (`~[]E`→`List E`/`Go.Slice E`,
    `~map[K]V`→`Go.Map`, union constraints → the Gom member
    types of the union). Sketch the emission for each —
    instantiation shape `slices.Contains[[]byte, byte](...)` is
    fine; `S` can also bind a named type, which widens what
    the Gom side may need to name.
(3) Implement per decision: E0506 classification gains the
    admitted classes (record excluded classes with their own
    reasons — don't widen the hidden list silently), call-site
    instantiation reuses the declared-order machinery from
    TestModuleInteropGenerics, boundary conversions follow the
    existing param/result channels (a `List Byte` argument for
    an `S ~[]byte` parameter must not need a new channel).
(4) Tests: `slices.Contains`/`maps.Keys`/`cmp`-class callables
    in TestModuleInteropBothDirections shape (both directions
    where the generated Gom is called back); spec fixtures for
    admitted + still-hidden classes; golden for instantiation
    emission. Update spec §8.3/§8.5 + problems doc
    2026-11-04-generic-go-fns-hidden.md and open-questions.
Ownership: internal/goimport (constraint classification,
instantiation, mapping), internal/check call sites (E0506
adjacent, additive — lane 1 owns the conform sites this wave;
flag overlap), spec.md §8.3/§8.5, docs/open-questions.md,
docs/problems/2026-11-04-generic-go-fns-hidden.md.
diag/codes.go append-only. Do NOT touch internal/lsp,
internal/gomtest, internal/lower, internal/codegen unless the
emission shape requires it (flag).
```

### 3. Go struct construction — `devin/wave15-struct-construction`

```
[decision+impl] Open question "Go の struct 型の構築・書き込み":
exported-field read is decided; construction (`http.Server{Addr:
...}` composite literals) and field writes stay unbounded. Today
the honest escapes are factory externs — gist-cli keeps TWO
adapters for exactly this (`NewClient` = `http.Client{Timeout:}`
+ callback; `StringField` = decode destination). This lane picks
the construction surface and implements it.

(1) Re-survey the need: gist-cli's `NewClient`/`StringField`
    against wave-14's `Byte` boundary — check whether
    `Ptr.make`/`Ptr.set` + `json.Unmarshal`-via-`any`-slot
    already closes `StringField` (decode destination is a
    `*string`; `json.Unmarshal(data, p)` may already typecheck
    through the `any` param). Only the genuinely missing piece
    is this lane's deliverable.
(2) Decision draft — options: (a) composite-literal syntax
    (`http.Server{Addr: "x"}` or a Go-scoped record form —
    new syntax, biggest surface: parse + check + emit), (b)
    field-write only (`v.Field = x` on `Ptr`/addressable —
    narrower), (c) constructor extern convention (status quo —
    record it as the decided answer). Sketch each against
    `http.Client{Timeout: 5*time.Second}` — duration, nested
    struct, and the `omitempty`-class partial-initialization
    cases.
(3) Implement per decision, additive. Syntax changes need
    parse+resolve+check+lower+codegen end-to-end (the full
    pipeline — sequence this lane AFTER lanes 1–2 so check
    churn settles); a field-write surface needs addressability
    rules (Go `x.f = v` needs addressable x — `Ptr.get`-style
    reads of non-addressable values can't spell it).
(4) Then shrink gist-cli's remaining adapters as far as the
    chosen surface reaches; the example stays end-to-end green.
(5) Spec §8.3/§8.5 + open-questions + problems
    (boundary-aliasing hypotheses sketch is relevant —
    construction shares the aliasing contract).
Ownership: spec.md §8.3/§8.5-adjacent, docs/open-questions.md,
internal/syntax+resolve+check+lower+codegen ONLY under option
(a) — under (b)/(c) the surface is check/goimport-narrow.
examples/gist-cli for the adapter shrink. diag/codes.go
append-only. Do NOT touch internal/lsp, internal/gomtest.
```

### 4. LSP go-tool diagnostics — `devin/wave15-lsp-gotool`

```
[decision+impl] Wave-14 lane 2 landed `gom build -go` — opt-in
`go build` per output dir with SourceMap-aware fold. The TODO
"LSP: go-tool 診断の取り込み" residual is the `gom lsp` side:
the server compiles CheckOnly with no output, so go-tool
diagnostics have nowhere to run.

(1) Decision draft FIRST: the invocation point options are
    (a) per-change: temp-pkg dir + `go build` after each
    publish (adds ~55–90ms/pkg on top of the ~48–50ms CheckOnly
    recompile — measure the real number on a fixture package),
    (b) debounced/idle: run go build only when the buffer goes
    quiet, (c) opt-in server flag (`gom lsp -go`/`gomi lsp
    --with-go`). The loader session cache question (~48–50ms
    dominated by `go list -export`) is the same lane's
    secondary decision — cache or record the deferral.
(2) Implement: reuse — don't fork — the wave-14 machinery
    (`runGoTests` directory grouping + GoPosIndex +
    GoToolDiagnostics folding). The temp output dir gets the
    generated files + a synthetic go.mod (the runGoTests recipe
    already builds a throwaway module context). Publish folded
    diagnostics through the same publishDiagnostics channel
    with `source: "go"` distinct from `source: "gom"` — clients
    can tell them apart; retract on next compile like gom
    diagnostics.
(3) Async shape: per-change compile is already synchronous on
    the read loop — decide whether go-tool runs block the loop
    (simplest) or fork (needs a generation counter to retract
    stale publishes; `$/cancelRequest` isn't implemented — note
    it).
(4) Verify in Emacs+Eglot like wave-11/12: inject a compile
    that passes gom check but trips `go build` (unresolvable
    reference, arity drift — same fixtures class as wave-14
    lane 2); the folded diag lands on the .gom span in the
    editor, drifts/ambiguities follow ResolveMatch honesty.
    Record measured latency in the decision + sketch.
Ownership: internal/lsp, cmd/gom (flag surface), internal/build
(additive reuse only). Do NOT touch internal/check,
internal/lower, internal/codegen, internal/eval. Ledger: the
TODO task's invocation-point half closes with 対応 note; the
cache half updates with its own outcome.
```

## Sequential tail (Wave 16 lead)

```
- P4-7 incremental build cache (design.md 5 章 cache keys).
- LSP completion/definition/references + incremental sync +
  `$/cancelRequest` + didSave/workspace.
- External-module stdlib Gom imports (E0203 — `import "list"`
  outside the main module; multi-package Gom's premise).
- `.gomi` `let` member polymorphism (thunk representation).
- `SpanAt` column/range granularity + codegen per-line re-pin.
- Exported-`let` façade / API hardening (boundary-mutation
  contract deferred alternative).
- D-3 README final adequacy, D-4 tutorial go.mod layout —
  end of project per user direction.
```

## Items needing decisions, not implementation

- Go ABI stability policy (generated API surface across Gom
  versions).
- Gom types implementing Go interfaces (v1 can't).
- User-declared mutable structures beyond `Ref`/`Array`
  (W0701/W0702 lint closed builtin list).
- Type classes vs explicit dictionary passing (v2).
- 3+-result fn *implementation* (v1 wording pinned wave 11).
- `rune`-as-`Char` semantic widening (byte/alias structural
  concern noted in problems/2026-11-06-rune-char-correspondence.md).
- `Result.to_option`/`Option.to_list` — structurally blocked by
  Go import cycles; stays recorded.
- Perf: stdlib cons-built combinators O(n²), `concat` O(k)
  stack — deferred until a workload demands it.

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
