# Generated-code benchmarks live beside the artifacts they measure

- 状態: 採用（実装・計測は [sketch: P5-3 generated-code perf](../sketch/2026-11-05-p5-3-generated-code-perf.md)）
- 日付: 2026-11-05

## Problem

P5-3 needs a repeatable way to measure the generated Go code —
`List.map`-style combinators, the closure/alias wrappers P2-2/P2-2c
introduced, and `|?>` pipeline shapes — before and while optimizing.
Two questions had to be settled up front: where benchmark code lives,
and what workloads it runs.

Constraints found while surveying the tree:

- Committed generated artifacts exist only under `stdlib/<pkg>/`
  (`list.go` generated from `list.gom`, kept fresh by
  `internal/stdlib/fresh_test.go`) and `testdata/golden/*.golden.go`
  (asserted text, not a buildable package).
- `experiments/p0/` already established the probe convention: a
  separate Go module per question holding hand-written mirrors of
  candidate representations, benchmarked with `go test -bench`.
- `internal/build` e2e tests compile a `.gom` string and run the
  output in a scratch module (`runCompiled`) — good for one-shot
  verification, too heavy and too variable for numbers.

## Decision

Benchmarks live beside the artifact they measure, in three slots:

1. **`stdlib/<pkg>/bench_test.go`** — hand-written Go benchmark files
   next to the committed generated code (same precedent as the
   hand-written `boundary_test.go`). They benchmark the real
   generated bodies (`list.Map`, `list.Fold`, ...) directly — no
   compile step, so the numbers reflect exactly what users' compiled
   programs link against. `go test ./stdlib/... -bench=.` runs them;
   plain `go test` does not, so CI is unaffected.

2. **`experiments/p5/`** — a scratch Go module in the p0 mold for
   generated-code *shapes* that have no committed artifact:
   curried-alias wrappers (`let plus = add`), `|?>` segment
   closures, pipe-`try` fusion paths. Each file carries a provenance
   comment naming the `.gom`/golden it mirrors; when the generated
   shape changes the mirror is updated by hand. Keeps throwaway
   measurement out of the shipping packages.

3. **Escape analysis / `-race`** are captured as command output in a
   `docs/sketch/` report rather than as tests — `go build
   -gcflags="-m"` on `stdlib/` and the `experiments` mirrors, and
   `go test -race` over the concurrency paths (`rt`, the
   `internal/build` `go`/`select`/`Chan` end-to-end tests). Escape
   output is noisy across toolchain versions; pinning it in a test
   would gate on wording, not performance.

Workloads:

- **Traversal-dominated** (`Map`/`Filter`/`Fold`/`Sum`/`Find`/`Iter`/
  `Length`): sizes 1e3 and 1e5 — the linear axis, where per-element
  closure allocation shows up in `benchmem`.
- **Construction (`[x, ..rest]` cons)**: `Append`/`Rev`/`Range`/
  `Concat` at sizes 256/2048 — the known O(n²) axis on the slice
  representation (P0-3); larger sizes would spend benchtime on the
  asymptote, not the signal.
- **Curried value application**: `plus(x)(y)` vs `add(x, y)` per the
  P0-2/function-value-representation shapes — isolates the arity-one
  adapter cost.

Results go to `docs/sketch/` with environment notes; no CI
regression gate yet — machine variance makes absolute thresholds
meaningless, and relative-before/after comparison is the honest
signal for now.
