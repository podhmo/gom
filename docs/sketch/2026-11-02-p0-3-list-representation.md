# P0-3: `List` representation — slice vs persistent cons list

- 目的: choose the Go representation of Gom's builtin `List a` by hand-writing candidate implementations and comparing them, per TODO.md P0-3.
- 関連: [decisions/2026-11-01-value-representation](../decisions/2026-11-01-value-representation.md) (`List` row of "P0 で決めること"), [open-questions.md](../open-questions.md) ("`List` の表現: スライスベースか、永続 cons リストか"), [design.md](../design.md) 4.1 (ADT encoding), spec.md 3.1 / 4.1 / 8.5.
- 実験コード: `experiments/p0/list-repr/` (standalone module `github.com/podhmo/gom/experiments/p0/list-repr`, go 1.27).
- 測定環境: go1.27.1 linux/amd64, AMD Ryzen 7 255, `go test -bench=. -benchmem` (default benchtime). 2026-11-02.

## Candidates

Three representations were implemented with parallel APIs
(`Cons`/`Uncons`/`Head`/`Tail`/`Len`/`At`/`Map`/`Fold`/`Append`/`All`/
`Of`/`FromSlice`/`ToSlice`):

| package | representation | notes |
| --- | --- | --- |
| `slicelist` | `type List[A] []A` | empty = nil. Direct `len()`, indexing, `range`. |
| `conslist` | `type List[A] struct{ head *cell[A] }`, `cell = {value A; next *cell}` | opaque persistent cons list; the realistic engineering choice if `List` is a builtin. |
| `consadt` | `type List[A] interface{ isList() }` + `List_Nil[A]` / `List_Cons[A]{V0 A; V1 List[A]}` | what design.md 4.1 codegen emits if `List` is compiled as an ordinary ADT, with no special-casing. Measured to separate "cons" from "cons as generic ADT". |

`x :: rest` / `[x, ..rest]` maps to two operations in both reps:
construction (`Cons`) and destructuring (`Uncons`, the `[x, ..rest]`
pattern). The two directions have very different costs on slices, which
is the crux of the comparison.

## Results

### Construction: prepend-heavy workload

Building a list of n elements by repeated `Cons` (`x :: rest` in a loop):

| n | slicelist | conslist | consadt |
| --- | --- | --- | --- |
| 100 | 15.9 µs, 42.2 kB, 100 allocs | 1.24 µs, 1.6 kB, 100 allocs | 1.92 µs, 2.4 kB, 100 allocs |
| 1,000 | 1.13 ms, 4.27 MB, 1,000 allocs | 12.6 µs, 16 kB, 1,000 allocs | 19.5 µs, 24 kB, 1,000 allocs |
| 10,000 | **99.7 ms, 428.5 MB**, 10,014 allocs | 142 µs, 160 kB, 10,000 allocs | 215 µs, 240 kB, 10,000 allocs |
| 100,000 | (skipped: O(n²) ≈ 10 s) | 2.01 ms, 16 MB, 100,000 allocs | 2.70 ms, 24 MB, 100,000 allocs |
| 1,000,000 | (skipped) | 17.8 ms, 16 MB, 1,000,000 allocs | — |

Single `Cons` against a fixed tail of length k (`BenchmarkCons`):

| k | slicelist | conslist | consadt |
| --- | --- | --- | --- |
| 0 | 10.4 ns, 8 B, 1 alloc | 12.2 ns, 16 B, 1 alloc | 19.4 ns, 24 B, 1 alloc |
| 100 | 322 ns, 896 B, 1 alloc | 12.3 ns | 19.2 ns |
| 10,000 | 20,094 ns, 80 kB, 1 alloc | 14.3 ns | 21.0 ns |

Slice prepend is O(k) per call, O(n²) in a loop: a fresh array of
len+1 is allocated and the whole tail copied, every time. Cons prepend is
flat O(1) (one cell, ~12–21 ns). At n=10,000 the slice rep is ~700×
slower for prepend-built lists — this is the decisive asymmetry.

### Destructuring: `[x, ..rest]` (Uncons)

O(1) with zero allocations on all reps: slicelist 0.45 ns, conslist
0.43 ns, consadt 1.54 ns (type-switch overhead, still noise-level).

### Memory retention of `rest`

`retention` package, `runtime.ReadMemStats` on `HeapAlloc` after two GCs.
Scenario: build a 1,048,576-element `List[int64]`, keep only an 8-element
suffix (`rest`), release the head.

| rep | retained bytes | share of original 8.4 MB array |
| --- | --- | --- |
| slicelist `l[n-8:]` | **8,390,896 B** | 100.0 % |
| slicelist `FromSlice(l[n-8:])` (copy suffix) | 2,560 B | — |
| conslist `Tail() × (n-8)` | 3,032 B | — |
| consadt `Uncons × (n-8)` | 8,448 B | — |

With pointer elements (`List[*block]`, 131,072 × 64 B) the retained
backing array also keeps every pointed-to object live: **9,438,680 B**
retained by the slice tail vs 2,456 B for cons.

A sub-slice is (ptr, len, cap) into one array object: `rest` keeps the
entire array — including the dropped prefix — alive, and nothing in the
slice records how much was dropped, so no runtime heuristic can detect
the waste later. The only escape is copying the suffix (one allocation),
e.g. a `List.compact`/`List.copy`-style stdlib function. Cons cells are
individually collectable, so a kept suffix retains exactly its own cells.

### Iteration: fold / map / range

| op | n | slicelist | conslist | consadt |
| --- | --- | --- | --- | --- |
| Fold (sum) | 1,000 | 244 ns | 850 ns | 1,398 ns |
| Fold | 100,000 | 22.8 µs | 87.6 µs | 144 µs |
| Fold | 1,000,000 | 258 µs | 1.06 ms | 1.56 ms |
| `for x := range l.All()` | 100,000 | 22.9 µs (native `range`: 22.2 µs) | 87.7 µs | 115 µs |
| Map | 1,000 | 2.86 µs, 1 alloc | 13.0 µs, 1,000 allocs | 31.6 µs, 1,000 allocs |
| Map | 100,000 | 270 µs, 1 alloc | 1.70 ms, 100,000 allocs | **8.15 ms**, 100,000 allocs |

Contiguous storage wins by ~4–6× on traversal (cache locality, no
pointer chase). `iter.Seq` on the slice rep costs the same as native
`range` (the iterator inlines away). consadt `Map` is written recursively
the way naive `match`-based codegen produces it — at n=100k the deep
recursion makes it ~4.8× slower than conslist's iterative map; a real
stdlib would build cells iteratively, but the O(n)-depth stack for naive
recursive Gom source is a real property of linked reps.

### Per-element memory

| element | slicelist | conslist | consadt |
| --- | --- | --- | --- |
| `int64` | 8 B | 16 B (8+8 next) | 24 B (8+16 iface word) |
| 32 B struct | 32 B | 40 B | 48 B |
| heap objects per n elements | 1 | n | n |

Cons reps pay a pointer per element (conslist) or an interface word
(consadt), and one GC-scanned heap object each — also relevant to GC
pause behaviour at scale, not just footprint.

### Go-interop conversions

| op | n | slicelist | conslist | consadt |
| --- | --- | --- | --- | --- |
| FromSlice | 100,000 | 160 µs, 1 alloc | 1.89 ms, 100,000 allocs | 2.68 ms, 100,000 allocs |
| ToSlice | 100,000 | 160 µs, 2 allocs | 665 µs, 2 allocs | 671 µs, 2 allocs |
| Len | 100,000 | 0.44 ns | 88 µs | 90 µs |
| Append (10k+10k) | — | 48.7 µs, 1 alloc | 189 µs, 10,003 allocs | — |

Boundary copies are already decided (decision: both directions copy).
Under the slice rep the copy is a single `memcpy` allocation and the rep
is *structurally identical* to `[]A`, so the boundary is a pure ownership
question. Under cons reps every boundary crossing additionally costs n
cell allocations — `FromSlice` is ~12–17× more expensive than the slice
rep's copy.

## Semantics comparison (items 1, 5, 6)

### Copy / ownership / aliasing

**slicelist** — `List[A]` IS `[]A`. The raw conversion `List[A](xs)` /
`[]A(l)` is free and **aliases**: a caller mutating `xs` after handing it
over changes the "immutable" list (demonstrated by
`slicelist.TestAliasingHazard`). Therefore `FromSlice`/`ToSlice` copy —
which is exactly the boundary contract already decided in
value-representation.md (`spec 8.5`). Inside the language, aliasing is
harnessed deliberately: `Uncons`/`Tail`/`l[k:]` share the backing array
in O(1). The contract "never write through a List's array" is enforced
on the Gom side (no mutation facilities) and is documentation-only for
Go callers — the same trust model as `[]byte` results from Go APIs.
A `nil` List is a valid empty list, so `var l List[A]` needs no special
handling.

**conslist** — cells are unexported and never mutated; a List cannot
alias a `[]A` at all (different structure), so boundary conversions
*must* copy and *cannot* leak. Persistence falls out for free: `Cons`
and `Tail` share cells safely. The zero value is a valid empty list.
There is no aliasing hazard in either direction — strongest ownership
story, bought with per-element pointer + allocation overhead.

**consadt** — same persistence and no-aliasing properties as conslist,
plus one extra: a type assertion copies the variant struct out of its
interface box, so Go callers get copies and cannot corrupt shared cells
even though the fields are exported (`TestNoMutationHazard`). Downsides:
`var l List[A]` is a nil interface — an *invalid* variant that panics in
`match` (correct per spec 8.6, but a footgun the other reps don't have),
and every destructure pays the unboxing copy.

### Go-side ergonomics

- slicelist: `len(l)`, `l[i]`, `for i, x := range l`, `slices` stdlib —
  idiomatic Go with zero impedance. Go callers must be told not to
  mutate, but the shape of the code is exactly what a Go programmer
  expects ("Go から使える" design goal).
- conslist: `l.Len()` is O(n), no indexing sugar; iteration needs
  `for x := range l.All()` (measured: same cost as the internal fold,
  ~1 ns/elem overhead over the pointer chase itself — negligible).
  Feels like a library type, not a builtin.
- consadt: destructure via type switch (`case List_Cons[A]`); `nil` is
  an invalid variant; and see the inference wart below.

### Interaction with Go interop

Passing `List` to a Go func taking `[]A` and back:

- slicelist: `ToSlice()`/`FromSlice()` are single-allocation `memcpy`s;
  the unchecked conversions (free, aliasing) exist if a future `Go.Slice`
  or `extern` rule ever wants zero-copy — the rep supports both
  contracts. `nil` round-trips correctly.
- cons reps: both directions always copy; `FromSlice` costs n heap
  allocations. `nil` maps to the empty list cleanly (conslist) or is an
  invalid variant (consadt).

## Side findings relevant beyond `List`

1. **Phantom type parameters break inference on sealed-interface ADTs.**
   With `type List[A any] interface{ isList() }`, A appears nowhere in
   the method set, so `List[int]` is structurally identical for all A:
   `Uncons(l)`, `Len(l)`, `ToSlice(l)` all fail with "cannot infer A" —
   even from a concrete `List[int]` — and need explicit `Uncons[int](l)`.
   This affects every function whose only source of A is the ADT
   argument, i.e. **any** generic ADT encoded per design.md 4.1
   (Option, Result helpers too). Generated code can always emit the
   explicit type arguments (the checker knows the type), so this is
   cosmetic for codegen but a wart for Go-side consumers and for
   hand-written helpers. Verified workaround: a self-referential marker
   `interface{ isList(List[A]) }` restores inference (A appears
   structurally) while keeping the seal. Worth feeding into P0-1/P0-6.
2. **ADT encoding gives free immutability.** Values pulled from an
   interface are copies, so exported variant fields cannot be used to
   mutate shared structure (see above). The same property does not hold
   for the slice rep (a shared array is genuinely shared).
3. **`nil` is the empty list only on slice/opaque reps.** On consadt a
   nil interface is an invalid variant; generated code must initialize
   temporaries to `List_Nil[A]{}`, not `nil`.

## Discussion

**Prepend is the only axis where cons wins, and it wins big (O(1) vs
O(n), ~700× at n=10k in a loop).** Everywhere else the slice rep is
better or equal: 4–6× faster traversal, ~3× less memory per element for
small A (and far fewer heap objects / GC roots), O(1) `Len`, O(1)-copy
boundary with `[]T`, native `len()`/`range`/indexing for Go callers,
valid zero value, no boxing.

The open question's worry — "`[x, ..rest]` のメモリ保持" — is real and
measured: a slice tail retains 100% of the backing array including dead
prefix and (for pointer elements) dead objects. But note the shape of
the hazard: it only bites when a *small suffix of a much larger list* is
kept long-term. The stdlib can offer `List.copy`/`compact` (one
allocation, releases the array) for that case, mirroring how Go itself
handles it today (`slices.Clip`, fullslice expressions) — the hazard is
a known Go idiom, not a new failure mode.

The cons rep's win condition — code that builds lists by prepend —
collides with a v1 reality: `Map`/`Fold`/`FromSlice`/literals (the
dominant list traffic, and all of `|>` pipelines) are *cheaper* on the
slice rep, and stdlib combinators can internally build result lists into
a local `[]A` buffer regardless of the public rep. The remaining gap is
user-level `x :: acc` accumulation (e.g. build-then-reverse idioms),
which is quadratic under slices; the honest mitigations are `Array a`
(already the mutable-array type in spec 3.1 — `Array` + `to_list` is the
natural "builder") and documenting the cost model.

The consadt measurements also argue against "just compile List as an
ordinary ADT": strictly worse than conslist on every axis (boxing,
type-switches, phantom-param inference, nil-is-invalid) with no upside —
if a linked rep were ever wanted, it should be the opaque builtin form.

**Recommended: slice representation** (`type List[A] []A`), keeping
`[x, ..rest]` semantics as-is but documenting its cost model, i.e.:

- `[x, ..rest]` *pattern* (and `Tail`/`drop`): O(1) view; document that
  `rest` retains the whole backing array, and provide a copying
  `List.compact`-style escape hatch in stdlib.
- `x :: rest` / `[x, ..rest]` *construction*: O(n); spec/std docs must
  not let users expect cons-list costs (value-representation.md already
  anticipates this clause). Incremental building goes through
  combinators or `Array` builders.
- Boundary with `[]T`: copy both directions (unchanged, spec 8.5);
  `Go.Slice` remains the zero-copy mutable alternative.
- No hybrid for v1. A chunked/unrolled list or reversed-storage-with-
  ownership could recover amortized O(1) prepend, but adds real
  complexity for a workload slice rep handles via `Array`; candidate
  for open-questions future work, not v1.

## Reproduce

```sh
cd experiments/p0/list-repr
~/sdk/go1.27.1/bin/gofmt -l .          # empty
~/go/bin/go1.27.1 vet ./...
~/go/bin/go1.27.1 test -v ./retention  # retained-bytes log lines
~/go/bin/go1.27.1 test -run='^$' -bench=. -benchmem ./...
```
