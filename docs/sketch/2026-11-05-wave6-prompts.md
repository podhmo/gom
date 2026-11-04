# Wave 6 orchestration prompt drafts

State as of 2026-11-05: Wave 5 integrated — hardening batch
(`c2d0d62`), P3-2 residual + P3-6 (`2c080f6`, golden `a8ff720`),
V-6 residuals (`1a47892`), P5-1 concurrency (`ecfbb16`), ledger
(`cf85c3a`). Round record:
[2026-11-05-wave5-round.md](2026-11-05-wave5-round.md). This file
drafts the prompts for the next wave, following
`2026-11-05-wave5-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Dependencies newly cleared by Wave 5

- **P3-7**: P3-2 `[x]`, P3-3 `[x]`, P3-4 `[x]`, P3-5 `[x]`, P3-6
  `[x]` — all dependencies closed. This is the P3 completion
  audit: real programs using `os`/`io`/`strconv`/`encoding/json`,
  and its findings feed the standard-bindings and Go.Any
  open-questions.
- **P5-2**: P5-1 `[x]` — the lint surface (`Chan`/`SendChan`/
  `RecvChan`/`Task`, `go`, `select`) now exists to warn over.
- **P5-3**: P4-8 `[x]`, P5-1 `[x]` — generated-code perf work can
  start (benchmarks first, then specialization).
- **P2-4c**: P5-1 merged — lower/codegen is free again. Same lane
  as P5-3, so bundle them in one session (P2-4c first; it is small
  and thematically identical: remove an intermediate Result from
  generated code).
- **E0415 item ③**: P5-1 merged — the select check code is stable.
  Bundle with the other `checkFor`/diagnostic leftovers.
- **Package-init-order**: the blocking spec-decision is now the
  task itself (draft spec section as a decision, then implement
  package-wide `checkInitCycles` — the TODO bug is filed).

## Recommended Wave 6 composition (4 parallel, disjoint areas)

| # | Task | Main areas touched | Worktree |
|---|------|--------------------|----------|
| 1 | P3-7 real-program interop sweep | testdata/e2e fixtures, goimport edge fixes, interop decisions | gom-wt-p3-7-realworld |
| 2 | P5-2 mutable send/capture warnings + diagnostic leftovers | check analysis, diag codes, spec fixtures | gom-wt-p5-2-lint |
| 3 | P2-4c + P5-3 codegen/perf lane | lower pipe fusion, codegen specialization, benchmarks | gom-wt-codegen-perf |
| 4 | Package init order + small fixes | spec §init-order decision, lower/initcycle, eval lit, test polish | gom-wt-initorder-small |

Overlap notes:

- Slot 2 owns `internal/check` diagnostics and `checkFor`/select
  check paths; slot 4's `checkInitCycles` lives in
  `internal/lower` — disjoint.
- Slot 3 owns lower/codegen broadly; slot 4 touches only
  `internal/lower/initcycle.go` and `internal/eval/lit.go` —
  acceptable minor overlap, merge-order handles.
- Slot 1 may want small goimport/check interop fixes discovered
  during real programs — keep them in slot 1 (its e2e depends on
  them); report anything bigger as a residual.
- P4-6 LSP stays deferred (consume SourceMap gaps first — same
  reasoning as Waves 4–5). P4-7 incremental cache is a candidate
  for the slot-4 area but defer unless slots finish early —
  `iface.Hash` consumer is its own design.

## Individual prompts

### 1. P3-7 — `wt/p3-7-realworld` @ gom-wt-p3-7-realworld

```
Implement P3-7 of the Gom compiler — the P3 completion audit.
TODO.md P3-7 is authoritative: real programs using os, io,
strconv, encoding/json (and neighbors you find natural: flag,
time, strings, errors) must compile and run. This is a sweep, not
a feature: write several small-but-real .gom programs (file IO,
flag parsing + strconv, JSON round-trip, http is optional) as
e2e fixtures in the testdata/multifile|gocall style, exercise them
through gom check → gom build → go build → go run, and fix what
breaks. Everything you cannot fix lands as a recorded residual
(TODO bug / gaps ledger / open-questions) — the audit's job is a
map of the interop boundary, and "this program cannot work today
because X" is a first-class result. Specifically probe the known
boundary questions: does anything need extern overrides (input for
the standard-bindings task); Go.Any in signatures; composite
literal construction; struct field writes; error wrapping with
|?>. Record a sketch report of what worked/broke per package.
Go-interop change: tests in both directions where relevant.
Dependencies: P3-2..P3-6 done.
```

### 2. P5-2 + diagnostic leftovers — `wt/p5-2-lint` @ gom-wt-p5-2-lint

```
Two-phase task in one session. Phase 1 — P5-2: linter warnings for
sending/capturing mutable values (TODO.md P5-2). Spec 10 /
concurrency-model decision govern what counts as mutable (Ref,
Array, and whatever the lint surface decides for ADTs containing
them). Warnings vs errors is itself a spec question — check the
concurrency decisions and open-questions first; if undecided,
draft the decision, prototype, verify, update status. Warnings
need a diag mechanism for non-fatal diagnostics if none exists —
check internal/diag first.
Phase 2 — diagnostic leftovers in the same check/diag area:
(i) E0415 item ③ — select channel operand reports E0401 instead of
E0415 when builtin `Chan` is shadowed (the `for` shadowedBuiltin
branch has no recv/send-arm equivalent); (ii) `checkFor`'s
shadowed-builtin list lacks `SendChan` (TODO bug — a file-local
`type SendChan` under `for` gets a bare mismatch); (iii) the
`Chan.close (Chan.recvOnly ch)` E0407+E0401 noise pair flagged in
P5-1 review — improve only if trivially safe, else record.
Spec fixtures for each diagnostic rule. Dependencies: P5-1 done.
```

### 3. P2-4c + P5-3 — `wt/codegen-perf` @ gom-wt-codegen-perf

```
Ordered task in the lower/codegen lane.
Phase 1 — P2-4c (TODO.md): unguarded-step EscapeCall fusion. When
an unguarded pipe step's argument is a Go (T,error) call, the
argument's ResultFromGo materialization followed by the next
segment's EscapeResult check leaves one intermediate Result —
separate argument evaluation (user-side, may contain try) from the
call proper (in-segment EscapeCall) to fuse. Also fix the pipeRest
span inconsistency (binding uses lv.Y.Span(), enter-escape uses
lv.Sp — P2-4b review F5/F6). Golden updates + execution
regressions.
Phase 2 — P5-3 (TODO.md): generated-code optimization and perf
verification. Start with a benchmark harness decision (where do
benchmarks live, what is the workload — record as draft decision),
then measure the obvious suspects: List.map-style specialization
per the TODO entry, escape analysis of the closure/alias wrappers
P2-2/P2-2c introduced, `go test -race` clean on the concurrency
paths P5-1 added. Land wins that are safe; record anything needing
bigger design (List representation, specialization policy) as
residual. Dependencies: P2-4b done, P4-8 done, P5-1 done.
```

### 4. Package init order + small fixes — `wt/initorder-small` @ gom-wt-initorder-small

```
Ordered batch.
(a) [spec decision] Package initialization order: spec has no
section defining init order for a multi-file package — E0907's
Spec: reference is ungrounded and checkInitCycles can't go
package-wide until the semantics exist (gaps ledger). Draft the
spec section as a decision (Go-consistent order? declaration
order? dependency order? — Go's own file-order-by-filename +
dependency analysis is the natural baseline; the package-unit
decision has context), record proposed spec wording in the
decision doc WITHOUT editing spec.md, prototype-verify.
(b) [implement] With (a)'s rule, make checkInitCycles package-wide:
the sibling-file init cycle `a.gom: let x = y + 1` /
`b.gom: let y = x + 1` currently emits two vars and dies at
`go build` "initialization cycle" (TODO bug). It must become a Gom
diagnostic (E0907-family) on the package unit.
(c) [small] eval litValue parses int literals via ParseInt before
the Float check — `let m: Float = 99999999999999999999` eval-fails
CodeInternal while codegen emits float64(...) fine (TODO bug).
ParseFloat-first under isFloatCon + regression.
(d) [small] internal/stdlib/fresh_test.go polish from review:
header note that the byte-compare becomes Go-version/platform
sensitive once a stdlib package gains `import go`; make the
os.Stat(testWant) error direction explicit (treat non-ENOENT stat
errors as failures, not "missing").
Dependencies: V-6 merged. If (a)'s decision cannot settle
reasonably, record the failed approach in docs/problems and skip
(b) — do not guess the semantics silently.
```

## Sequential / smaller tasks (Wave 6 tail or Wave 7 lead)

```
- P4-6 LSP (SourceMap-aware inverse mapping per P4-5 gaps) —
  deferred since Wave 4; consider after the gaps thin out.
- P4-7 incremental cache (iface.Hash consumer).
- Standard-bindings task (default extern override list) — decision
  input arrives from P3-7; manage location per open-questions.
- Multi-file eval — open-questions decision needed first
  (eval.New package-unit inputs vs recorded boundary).
- Any-hole cute premature-bind diagnostic divergence — small
  semantics question from P3-4 review.
- select arm-priority syntax — open-questions.
- Carried: Float constant-folding divergence; named-variant `.f`
  on the wrong variant; structural Go constraints (~[]E/~map);
  *T default; .gomi enforcement beyond build/check;
  checkInterfaces asymmetry; spec §7 .gomi-import wording;
  spec parse_port Int64 example drift.
```

## Items needing decisions, not implementation

- Standard bindings home + list content (P3-7 output is the input).
- `.gomi` polymorphic let members (thunk across imports) —
  open-questions; the E0602 bound holds meanwhile.
- `Go.Any`/`any` spellable in signatures — open-questions.
- `Go.conv` Slice↔List raw pun — spec wording or align to copy.
- stdlib Gom modules importable from external modules (E0203).
- Multi-file eval (eval.New takes single check.File).
- Channel send-value type restrictions (Rust Send analog) —
  future work already listed.
- Carried from Wave 5: Float constant folding; wrong-variant `.f`
  semantics; structural constraints; `*T` default; `.gomi`
  enforcement scope; checkInterfaces asymmetry.

## Wave 7 candidates (after Wave 6)

- P4-6 LSP, P4-7 incremental cache.
- Standard-bindings implementation (after P3-7 + home decision).
- Multi-file eval implementation (after its decision).
- D-3 README final adequacy, D-4 tutorial go.mod layout — still
  end-of-project per user direction.

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
