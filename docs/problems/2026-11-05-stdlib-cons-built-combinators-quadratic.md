# Cons-built stdlib list combinators are quadratic, and Gom cannot reach a mutable build buffer

- 状態: 一部解消（2026-11-18 — `Array` builder + `to_list` と `@extern fn` extern-impl 機構を採択・実装。`Map`/`Filter`/`Append`/`Rev`/`Range` が buffer loop の hand-written Go 実装になり O(n) 化（n=2048 で ~1000×, 1 alloc）。参照: [decision](../decisions/2026-11-18-cons-buffer-array-extern-impl.md)、[検証](../sketch/2026-11-18-cons-buffer-verification.md)。残件: ユーザーコードの cons 蓄積を自動で救う lower cons-recursion 特殊化は podhmo 採択で future work、`Fold` の要素ごとクロージャ確保は未解決。追記 2026-11-18: lower の cons-recursion 特殊化（[decision](../decisions/2026-11-18-cons-recursion-specialization.md)）でユーザーコードの match-cons 蓄積も buffer loop 化 — concat のような非 cons 形は残る。追記 2026-11-19: 残った非 cons 形（連鎖 append、fold 内 concat、相互再帰、呼び出し結果 2 度読み）を実測 — n=8000 で被覆形の ~600–900 倍、いずれも「成長する acc の全コピー」が機構。救済候補（SCC 拡張 / Array イディオム誘導 / 診断ヒント / 表現変更）は [wave23 調査](../sketch/2026-11-19-wave23-noncons-accum.md) に整理。**podhmo 採択: β イディオム誘導 + γ 警告 lint（W0801、lower の lintQuadraticAccum で 4 除外形を検出）を採用、α は将来拡張余地、δ は再検討候補として本ファイルに残す**（[decision](../decisions/2026-11-19-noncons-accum-policy.md)））
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

## Resolution

Wave 18 (2026-11-18) adopted the first two options together:

- **`Array` builder + `to_list`** — the `Array` builtin module ships
  `make`/`length`/`get`/`set`/`from_list`/`to_list` (the freeze is
  zero-copy with the documented hand-off contract).
- **Intrinsic/extern-impl** — the `@extern fn` attribute declares a
  signature whose body is a hand-written Go function in the same
  generated package; stdlib's five quadratic combinators moved to
  `stdlib/list/impl.go` buffer loops.

The **compiler specialization** option is deferred future work, and
the `Fold` per-element closure issue below remains open.

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
