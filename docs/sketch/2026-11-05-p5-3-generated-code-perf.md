# P5-3 generated-code performance: measurements and landed wins

- [decisions/2026-11-05-generated-code-benchmark-harness.md](../decisions/2026-11-05-generated-code-benchmark-harness.md)
  — where the benchmarks live and why
- Environment: go1.27 linux/amd64, AMD Ryzen 7 255, `go test -bench=. -benchmem`
  (default benchtime). 2026-11-05.

## Workloads and baseline

`stdlib/list/bench_test.go` measures the committed generated
`list.go` directly. Two axes per the harness decision: read-only
walks at n ∈ {1e3, 1e5}, cons-built results at n ∈ {256, 2048}
(the O(n²) axis on the slice representation — P0-3).

### Before (`list.gom` at 7e533ed)

| op | size | ns/op | B/op | allocs/op | note |
| --- | --- | --- | --- | --- | --- |
| Fold | 100,000 | 1,358,451 | 1,600,010 | 100,000 | `f(acc)(x)` — 1 closure/elem |
| Sum | 100,000 | 1,335,910 | 1,600,012 | 100,000 | delegates to Fold |
| Iter | 100,000 | 21,404 | 0 | 0 | `for range` — clean |
| Find | 100,000 | 131,515 | 8 | 1 | self-tail-recursive — already a `for` loop (`markTailCalls` runs unconditionally) |
| Length | 100,000 | 21,438 | 0 | 0 | `@tailrec` loop |
| Map | 2,048 | 5,851,552 | 17,743,800 | 4,096 | cons-copy quadratic |
| Filter | 2,048 | 1,507,483 | 4,478,748 | 2,047 | cons-on-keep quadratic |
| Append | 2,048 | 5,846,312 | 17,890,959 | 4,097 | O(m² + m·p) copies |
| Rev | 2,048 | 4,881,102 | 17,743,740 | 4,095 | rev_append conses |
| Range | 2,048 | 10,431,754 | 35,487,521 | 8,191 | cons + rev, double pass |
| Concat | 64 | 9,743,281 | 24,514,673 | 32,321 | `fold append` — see below |
| Concat | 512 | 4,639,715,692 | 12,125,811,688 | 2,094,732 | ~4.6 s, ~12 GB |

Concat is the catastrophic case: `fold append []` left-folds, so each
step `append acc xs` walks and copies the *whole accumulator*, and
`append` itself is O(m²/2 + m·p) — doubly quadratic.

### After (`sum`/`concat` rewrites, `@tailrec` on `get`/`find`)

| op | size | ns/op | B/op | allocs/op | vs before |
| --- | --- | --- | --- | --- | --- |
| Sum | 100,000 | 64,888 | 0 | 0 | **20.6× faster, allocs → 0** |
| Concat | 64 | 389,793 | 1,122,040 | 1,023 | **25× faster, 21.9× less mem** |
| Concat | 512 | 21,238,477 | 70,726,389 | 8,198 | **218× faster, 171× less mem** |
| Find | 100,000 | 130,589 | 8 | 1 | same wall time and same shape; `@tailrec` adds the compile-time guarantee |
| Fold | 100,000 | 1,379,686 | 1,600,011 | 100,000 | unchanged (repr-inherent) |

Landed changes, all in `stdlib/list/list.gom` — pure Gom-source
rewrites, no compiler or surface change:

- `sum` → dedicated `@tailrec sum_go` loop. The fold route paid one
  captured-closure allocation per element because the folder is a
  curried `b -> a -> b` value (`f(acc)` materializes `func(a) b`).
- `concat` → right-folded `concat_go` (`append xs (concat_go rest)`):
  each inner `append` shares the already-built suffix instead of
  re-copying a growing prefix. `append` copies only its head
  argument, so each step is O(mᵢ) and the whole pass is O(Σ mᵢ) =
  O(total elements) — linear instead of quadratic.
  Trade-off: O(k) stack vs fold's O(1) — recursion depth equals the
  number of sublists; acceptable at any depth where the old version
  was usable.
- `get`, `find` → `@tailrec`: identical wall time and identical
  generated shape — `markTailCalls` lowers self-tail calls to loops
  whether or not the attribute is present. What the attribute buys is
  the proof obligation: a future edit that moves the recursive call
  out of tail position now fails to compile instead of silently
  regrowing O(n) stack. Zero-cost structural fix.

Not landed (needs design, recorded as residuals below):

- `Map`/`Filter`/`Append`/`Rev`/`Range` cons-built results —
  `append(List{h}, rest...)` copies per step; inherent to cons on the
  slice representation. The honest fix is one of: mutable `Array`
  builder + `to_list`, an intrinsic/extern-impl route letting stdlib
  fns build into a local `[]A` buffer (P0-3 already anticipated
  this), or compiler specialization of cons-recursion patterns.
- `Fold`'s per-element closure — the arity-one value representation
  (decisions/function-value-representation) materializes one
  `func(A) B` per `f(acc)`. A FoldFlat-style dual-surface stdlib or
  call-site uncurrying is a specialization-policy question.

## Escape analysis: alias/segment wrappers (P2-2/P2-2c/P2-4b)

Mirrored shapes in `experiments/p5/gen-perf` (provenance comments
name the goldens they mirror). `go build -gcflags=-m` plus
`go test -bench=. -benchmem`:

| shape | ns/op | allocs/op | escape finding |
| --- | --- | --- | --- |
| `add(20,22)` direct | 0.215 | 0 | — |
| `plus(20)(22)` opaque curried alias | 12.96 | 1 (16 B) | inner `func(tmp8)` literal escapes — the partial application's closure |
| `plus(20)(22)` same-scope | 0.213 | 0 | Go eliminates both literals entirely when the callee is provable |
| `count(8)` direct | 5.75 | 0 | — |
| `again(8)` opaque arity-1 alias | 6.56 | 0 | named wrapper costs one indirect call, no allocation |
| `parse("12345")` \|?> IIFE | 5.64 | 0 | `s does not escape` — segment closure stays on stack |

Conclusions:

- **The curried alias is only expensive across a boundary.** Opaque
  `plus(x)(y)` pays ~60× and one alloc; a provably-local partial
  application is free. Hot-loop HOF use is where it bites (Fold).
- **Arity-1 aliases and pipe IIFEs are allocation-free.** The P2-2
  named wrapper costs ~14% on a trivial body (one extra call);
  the P2-4b segment closure adds nothing measurable.
- No action needed on either — the curried-value cost is a
  representation consequence, recorded as residual.

## `-race` on the P5-1 concurrency paths

- `go test -race ./rt/` — clean (chan ops, direction views, negative
  make, spawn/await, await-rethrows-panic).
- Generated code under race: the P5-1 e2e surface (`concGom` —
  buffered/unbuffered chan, `go` rendezvous, `for` drain, `select`
  recv/send/default arms, direction views, `chan`/`RecvChan` Go
  boundary, ctx.Done shape, nil-chan select, notify-done, Task
  spawn/await) was compiled by `gom test` into
  `/tmp/gom-race-check/` and run as `go test -race -count=1 .` —
  **ok, 12/12 tests, no reports**. Note `gom test` itself shells out
  to plain `go test` — generated code gets no `-race` today; the
  manual run above is how this was verified.
- `go test -race ./internal/build/ -run <concurrency-adjacent>`
  (golden/sourcemap/pipe suites) — clean.
- Full `go test -race ./...` — see the run recorded in the report;
  repo suite is race-clean.

## Reproduce

```sh
go test ./stdlib/list/ -run='^$' -bench=. -benchmem
(cd experiments/p5/gen-perf && go test -bench=. -benchmem . && go build -gcflags=-m .)
go test -race ./rt/
# generated-code race check: gom test a .gom exercising the surface,
# then `go test -race` the emitted _test.go in its module dir
```
