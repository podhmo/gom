# Wave 4 orchestration prompt drafts

State as of 2026-11-05: Waves 1–3 integrated. Wave 3 landed P3-3
(Go slices/maps/generic fns, 4dd04e0), P4-5 (source-location
correspondence, db6326d), P4-8 (Gom-written stdlib list/option/result,
a14e47c), P2-4b (segmented try-pipe fusion, 1a6925f). This file drafts
the prompts to hand to child sessions for the next wave, following the
same pattern as `2026-11-03-wave3-prompts.md`. Always append the
"Common instructions" block to each prompt.

## Dependencies newly cleared by Wave 3

- P4-6 (LSP): P4-5 done — `build.Result.Output.SourceMap` is the
  internal API boundary; SourceMap-aware inverse mapping is explicit
  P4-6 design input (see gaps).
- P3-6 / P3-7: need P3-2, P3-3, P3-4, P3-5. P3-3 done; P3-2 still `[-]`
  (check what remains — named-type/interface edge cases), P3-4/P3-5 open.
- P2-4c (residual pipe fusion): P2-4b done — small optimization task.
- P2-2c: P2-4b merged, so lower/codegen init-ordering is free again.
- V-6: P4-1 done earlier; stdlib now exercises `.gomi` in production.
- P5-1: blocked on P2-2 `[-]` (P2-2c is the last open child).

## Recommended Wave 4 composition (4 parallel, disjoint areas)

| # | Task | Main areas touched | Worktree |
|---|------|--------------------|----------|
| 1 | P3-5 extern + extern arity bug | internal/goimport mapping, resolve, spec 8.4 | gom-wt-p3-5-extern |
| 2 | P3-4 method calls + `_` placeholder | check apply classification, codegen call | gom-wt-p3-4-methods |
| 3 | P2-2c recursive fn-value init cycles | lower init ordering, codegen decl emit | gom-wt-p2-2c-initcycle |
| 4 | V-6 multi-file + `.gomi` vertical check | build multi-file inputs, iface consumer | gom-wt-v6-multifile |

Overlap notes:

- P3-5 and P3-4 both reach `check` call classification, but through
  different mechanisms (extern signature mapping vs. method-select /
  placeholder). Acceptable; the second merge rebases.
- P3-5 and the extern arity diagnostic (TODO bugs) are the same code
  path — keep them in one task as planned in the Wave 3 doc.
- Do NOT run concurrently: P2-2c with P2-4c (same lower/codegen files);
  P3-5 with any new goimport conversion-table work.
- P4-6 is deliberately deferred: it is large and its design should
  consume the SourceMap limitations recorded in
  `docs/implementation-gaps.md` first.

## Individual prompts

### 1. P3-5 — `wt/p3-5-extern` @ gom-wt-p3-5-extern

```
Implement P3-5 of the Gom compiler. TODO.md P3-5 is authoritative:
`extern` declarations that override types and conversion rules for Go
bindings, including the comma-ok -> Option mapping (spec section 8.4).
Bundle the related bug from the TODO bugs section: `extern go "flag"
{ fn Lookup(name: String) -> Option (Ptr Flag) }` currently reports a
nonsense arity diagnostic ("`Ptr` takes 3 type argument(s)") — fix
both the diagnostic path and the message while implementing the real
conversion override. Decide how far extern reaches (function-level
signature replacement only, or member/field overrides too) as a draft
decision, prototype it, and record the result. Dependencies: P3-1 done.
Read spec chapter 8, docs/design.md, internal/goimport, and the
go-interop section of skills/gom-agent-guide first. Go-interop change:
add tests in both directions including nil, slices, named types,
interfaces.
```

### 2. P3-4 — `wt/p3-4-methods` @ gom-wt-p3-4-methods

```
Implement P3-4 of the Gom compiler. TODO.md P3-4 is authoritative:
Go method calls and the `_` placeholder (cute-style partial
application, decisions/2026-11-01-cute-placeholder.md). Non-`_`
arguments evaluate at creation time per spec 8.2. While in this area,
investigate the known delayed-binding bug (TODO bugs): a method call
on a lambda parameter whose receiver type is not yet resolved gets
misclassified as a Gom call convention — the second-opinion analysis
in TODO.md suggests deferring apply classification until constraint
resolution is complete; assess whether that design change fits inside
this task or needs its own decision/entry. Dependencies: P3-1, P2-2.
Read spec 8.2, the calleeOf/revisitSelects path in internal/check, and
the call-emission path in internal/codegen. Add spec fixtures and
eval-vs-compiled comparisons.
```

### 3. P2-2c — `wt/p2-2c-initcycle` @ gom-wt-p2-2c-initcycle

```
Implement P2-2c of the Gom compiler (the last open child of P2-2):
initialization cycles through aggregate values for recursive function
bindings. TODO.md P2-2c is authoritative. The lowering/init-order
machinery landed in P2-4b is merged — build on it, do not re-litigate
it. Determine which cycles are legal (function values inside
records/lists that are only called later) vs. must error, per
design.md's initialization ordering section; record the boundary as a
draft decision and verify. Dependencies: P2-2b done, P2-4b done.
Generated-code change: update testdata/golden and add execution
regressions (both allowed-cycle and rejected-cycle cases).
```

### 4. V-6 — `wt/v6-multifile` @ gom-wt-v6-multifile

```
Implement V-6 of the Gom compiler. TODO.md V-6 is authoritative: a
small library of two `.gom` files plus one `.gomi`, called from both
another Gom package and Go. Include one public function, one ADT, and
one function value. Same-package multi-file scope sharing is an open
question in docs/open-questions.md that affects name resolution, type
identity, initialization order, and generated-name collisions — resolve
it (or record the boundary found) as part of this task. stdlib now
uses `.gomi` in production, so treat its `.gomi` handling as the
reference implementation. Dependencies: P4-1 done.
```

## Sequential / smaller tasks (Wave 4 tail or Wave 5 lead)

```
Bug batch (localized, each gets a spec/codegen regression test):
- Numeric literals passed to Go.Any lose the Gom concrete type
  (boxing conversion path).
- Integer literal binding inferred as Float is emitted as Go `int`,
  rejected by Go (codegen literal emission must keep the lowered type).
Do NOT bundle: the delayed-receiver method misclassification belongs
to P3-4's design space; the two divergences (Float constant folding,
variant `.f` on wrong variant) need spec decisions first.

Test-hardening batch (P4-5 / P4-8 review leftovers, all in TODO.md):
- SourceMap/GoPos coverage: multi-.gom inputs, same-name relative
  paths, multi-line chunks, `file:line:` parse edge cases.
- stdlib artifact freshness gate: recompile `.gom` and compare to
  committed `*.go`/`*_test.go`.
- stdlib boundary helper unit tests (result.FromGo/AssertOk/Unwrap,
  option.FromGo, list FromSlice/ToSlice independence), nil Option pin,
  E0505_list_spread_mid and stdlib_partial fixtures, embed list vs
  internal/stdlib.packages sync guard.

P2-4c unguarded-step EscapeCall fusion: residual Result
materialization at segment boundaries (P2-4b review F5/F6). Small
optimization; pair with the pipeRest span inconsistency fix.
```

## Items needing decisions, not implementation

- Structural Go constraints (`~[]E`, `~map[K]V`, unions): keep E0506
  boundary, or extend / fix spec 8.1.6 examples. See
  docs/problems/2026-11-04-generic-go-fns-hidden.md and
  docs/open-questions.md.
- Float constant-folding divergence (eval per-op rounding vs. Go
  arbitrary-precision constant folding): choose eval-semantics or
  Go-semantics — spec question.
- Named-variant field access `.f` on the wrong variant: eval reads the
  slot, compiled code panics via type assertion — spec needs the
  contract (assertion semantics proposal in the gaps entry).
- `.gomi` is currently checked only by `build`/`check`; whether other
  subcommands enforce it is open (docs/open-questions.md).
- checkInterfaces asymmetry flagged during P4-8 review — design
  judgment needed, recorded in open-questions.
- `*T` default (Ptr T vs Option (Ptr T)) still open in open-questions.

## Wave 5 candidates (after Wave 4)

- P3-6 / P3-7 (Go-interop completion check) once P3-2/P3-4/P3-5 close
- P4-6 LSP (SourceMap-aware inverse mapping per P4-5 gaps), P4-7
  incremental cache (iface.Hash consumer)
- P5-1 concurrency once P2-2 closes; P5-2/P5-3 after
- D-3 README final adequacy judgment, D-4 tutorial/examples go.mod
  layout — both end-of-project tasks per user direction

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
