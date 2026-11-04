# Cons-built stdlib list combinators are quadratic, and Gom cannot reach a mutable build buffer

- 状態: 未決
- 日付: 2026-11-05

## Problem

On the slice `List` representation (P0-3), construction-side
`[x, ..rest]` is O(n): `append(List{x}, rest...)` allocates and
copies the tail every step. Measured on the committed generated code
([sketch](../sketch/2026-11-05-p5-3-generated-code-perf.md)):

- `Map`/`Filter`/`Append`/`Rev`/`Range` are all O(n²) — e.g. `Map`
  n=2048 ≈ 5.9 ms / 17.7 MB, roughly 55× the linear ideal.
- The same is true of *user* code that accumulates by consing —
  build-then-reverse idioms are quadratic regardless of stdlib.

The P0-3 verification already anticipated the mitigation: "stdlib
combinators can internally build result lists into a local `[]A`
buffer regardless of the public rep". But there is currently **no
route from `.gom` source to such a buffer**:

- `Array a` exists in the type system (spec 3.1) but its member
  table is empty — nothing to allocate, write, or freeze with.
- `Go.Slice` cannot be indexed or assigned from Gom (no `[]` index
  syntax), and `List ↔ Go.Slice` conversion at the boundary copies.
- `import go` hides Go functions with func-typed parameters
  ("func-typed parameters and results are not supported yet"), so a
  `rt.MapSlice(f func(A) B, xs []A) []B` helper is unreachable.
- `extern` only re-binds exported Go package functions, cannot name
  generic ones, and cannot point a `fn` at a hand-written helper in
  the same package (`gom check` enforces `.gomi` against `.gom`).

## Options seen so far

- **`Array` builder + `to_list`**: the P0-3 sketch's named
  mitigation. Needs the `Array` member surface designed (alloc,
  index-write, freeze semantics, immutability hand-off).
- **Intrinsic/extern-impl for stdlib fns**: an attribute or `.gomi`
  marker letting selected stdlib declarations be implemented by
  hand-written Go in the same package (the `types.go` precedent).
  Needs a policy for which fns qualify and how `.gomi`↔impl
  correspondence is checked.
- **Compiler specialization**: lower recognizes the
  match-cons-recursion shape (`map`/`filter`/`append`-like) and
  emits index loops over a buffer. Needs a specialization policy —
  which shapes, which functions, verified equivalence — and is the
  same class of machinery user code would want for its own
  cons-recursions.
- **List representation change**: chunked/unrolled or cons
  representation was rejected at P0-3 for good measured reasons;
  revisiting is a much larger design.

Related open axis: `Fold`-style HOFs pay one closure allocation per
element because folder values are curried (`f(acc)` materializes
`func(A) B`); a flat-folder dual surface or call-site uncurrying is
the same "specialization policy" question from the value-repr side.
