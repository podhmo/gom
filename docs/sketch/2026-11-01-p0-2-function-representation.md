# P0-2: Function-value representation — curried vs arity-aware closure

- **Purpose**: decide how Gom function values are represented in the
  generated Go code, feeding
  [decisions/function-representation](../decisions/2026-11-01-function-representation.md).
- **Related**: TODO `P0-2`, [design.md](../design.md) 4.4,
  [juxtaposition-and-currying](../decisions/2026-11-01-juxtaposition-and-currying.md),
  [labeled-required-arguments](../decisions/2026-11-01-labeled-required-arguments.md).
- **Experiment code**: `experiments/p0/fn-repr/` (own Go module).
- **Environment**: go1.27.1, linux/amd64, AMD Ryzen 7 255, `go test
  -bench=. -benchmem -count=3` (median-ish values quoted; all three runs
  agreed within noise).

## Candidates

Both packages implement the same hypothetical Gom module (`add3`, `add2`,
`inc`, `cmp_desc`, `transfer` with `~to`/`~amount` labels, `map`,
`zip_with`, `fold`, `mk_add : Int -> (Int -> Int)`), following the
design.md 4.4 rule that saturated calls to known functions compile to
direct multi-argument Go calls.

### Style A — curried (`curried/`)

A value of Gom type `T1 -> ... -> Tn -> R` is `func(T1) func(T2) ... func(Tn) R`.

- Known function used as a value → shared per-instantiation wrapper var
  (`var Add3Val = Curry3(Add3)`) or an inline `Curry3(Add3)` at each
  mention.
- Partial application → a nested closure (`func(b) func(c) { ... }`).
- Unknown value applied → `f(x)(y)(z)`.
- `CurryN`/`UncurryN` adapters for crossing to/from flat Go funcs.

### Style B — arity-aware (`arity/`)

A value of Gom type `T1 -> ... -> Tn -> R` is `func(T1, T2, ..., Tn) R` —
the Go func type *is* the "arity-aware closure"; a partial application is
an ordinary Go closure carrying the bound arguments, with the remaining
arity in its static type.

- Known function used as a value → the function itself (`add3` is
  `Add3`); no wrapper.
- Partial application → a flat closure of the remaining arity
  (`func(b, c) { return Add3(x, b, c) }`).
- Unknown value applied → still `f(x, y, z)`, because the remaining arity
  is known from the static type — **no intermediate closures, ever**.
- Defs whose declared arity is smaller than the type's arrow length
  (`fn mk_add (a) -> (Int -> Int)`) keep the declared-arity form for
  direct calls and get a flatten wrapper only when used as a value
  (`MkAddFlat`) — the one wrapper case in this style.

## Results

| Scenario (Gom source) | Style A — curried | Style B — arity | A/B |
| --- | --- | --- | --- |
| `add3 x y z` known, saturated | 0.42 ns, 0 alloc | 0.42 ns, 0 alloc | 1× |
| `add3` as value (shared wrapper var) | 0.42 ns, 0 alloc | 0.42 ns, 0 alloc | 1× |
| `add3` as value (inline wrapper per mention) | 15.1 ns, 24 B, 1 alloc | 0.42 ns, 0 alloc | ~36× |
| `f x y z`, f opaque (crossed a boundary) | **46.0 ns, 80 B, 2 allocs** | **1.06 ns, 0 alloc** | **~43×** |
| `f x y z`, f provable in same scope | 0.42 ns, 0 alloc | 0.42 ns, 0 alloc | 1× |
| `f x; (f x) y; (f x y) z` staged, opaque | **47.2 ns, 80 B, 2 allocs** | **1.06 ns, 0 alloc** | **~44×** |
| create partial `add3 x` | 11.0 ns, 16 B, 1 alloc | 11.0 ns, 16 B, 1 alloc | 1× |
| `g x y` on a stored partial, opaque | 16.4 ns, 24 B, 1 alloc | 1.90 ns, 0 alloc | ~9× |
| same, provable in same scope | 0.42 ns, 0 alloc | 0.42 ns, 0 alloc | 1× |
| `fold add2 0 xs` (1024 elems) | **21 000 ns, 32 KiB, 1024 allocs** | **241 ns, 0 alloc** | **~87×** |
| `fold` via `Uncurry2` adapter | 21 600 ns, 1024 allocs | — | conversion cannot remove chain cost |
| `FoldFlat` fed a flat Go func | 241 ns, 0 alloc | ≡ normal form | escape hatch = arity's normal form |
| `map strconv.Itoa xs` (1-arg param) | 15.5 µs | 15.6 µs | 1× (sanity) |
| `zip_with add2 xs ys` (2-arg param) | 23 900 ns, 1025 allocs | 2 610 ns, 1 alloc | ~9× |
| labeled partial `transfer ~amount:10` then `f x ~to:5`, opaque | 16.4 ns, 1 alloc | 1.90 ns, 0 alloc | ~9× |
| `slices.SortFunc` with Gom comparator (1024 elems) | **252 000 ns, 335 KiB, 10 724 allocs** | **20 500 ns, 0 alloc** | **~12×** |

### Reading the numbers

- The decisive axis is whether the function value is **opaque** to the Go
  compiler (arrived as a parameter, was stored, or came from another
  package). In the curried style every boundary-crossing application of n
  args costs n−1 intermediate closure allocations + n indirect calls,
  because each intermediate value is produced by calling an opaque
  closure.
- In the arity style even a deliberately *unfused* staged application
  collapses to one indirect call: the intermediate partial applications
  are locally constructed literals, which Go devirtualizes away when they
  stay in scope. And when the IR does fuse (it always can — remaining
  arity is static), there is nothing to collapse in the first place.
- When both creation and full application are visible to the compiler in
  one inlinable scope (`let g = add3 1 in g x y` inside one function),
  both styles are equally free — Go eliminates the closures entirely.
- Higher-order functions over collections are the catastrophic case for
  style A: `f(acc)(x)` per element allocates once per element (32 B
  each), and the cost is inherent to the representation — routing the
  same value through `Uncurry2` barely changes it. The design.md 4.4
  "specialization" escape hatch (`FoldFlat`) only recovers the fast path
  when the call site can supply a *flat* function at the origin; a value
  that already crossed a boundary in curried form cannot be salvaged.
- In style B the normal form *is* the specialized form — `Fold`'s folder
  parameter `func(Acc, A) Acc` is exactly what the escape hatch would
  have asked for.

## Evaluation axis by axis

### 1. Call overhead

- Saturated known call: identical (design.md 4.4 already mandates direct
  calls).
- Partial-application *creation*: identical — one closure either way.
- Applying an unknown value: style A pays n−1 allocs + n calls; style B
  pays one call. Up to ~44× at arity 3.
- Per-element HOF cost: ~87× for `fold`, ~12× for a real Go API
  (`slices.SortFunc`).

### 2. Monomorphization of polymorphic definitions

The monomorphization *point* is the same in both styles — it is the
value-use site, matching the existing "決まったこと":

- `map` used as a value: style B emits `Map[int, string]` — a plain
  instantiation, done. Style A emits `Curry2(Map[int, string])` —
  instantiation *plus* a second generic-instantiated arity wrapper.
- `zip_with add2` partially applied: both emit a closure of the
  remaining arity; style A's result is nested, style B's is flat.
- Passing a polymorphic function *through* a polymorphic HOF: unchanged
  in both.

So style A strictly adds a wrapping step at every value-use site (or one
shared wrapper var per function × instantiation).

### 3. Cross-package public surface

- Style B exposes `func(T1, ..., Tn) R` — an idiomatic Go func type.
  Go callers pass plain Go functions (including functions Gom knows
  nothing about) and partial application is an ordinary closure.
- Style A exposes `func(T1) func(T2) ... R` — unidiomatic; Go callers
  must hand-write nested literals or use `CurryN` adapters.
- The reverse direction is decisive: a Gom function value passed to a Go
  API (`slices.SortFunc`, `http.HandlerFunc`-shaped callbacks,
  `fs.WalkDirFunc`, …) needs `UncurryN` in style A — allocation at the
  boundary *and* the nested call chain at every invocation. In style B
  the Gom value is directly assignable.
- Function types appearing in public signatures (e.g. a Gom API taking
  `handler : Event -> Int -> Unit`) read naturally only in style B.

### 4. Partial application of labeled arguments

Labels are erased and calls are positional in declaration order in *both*
styles, so this is mostly neutral:

- `transfer alice ~amount: m` (one arg remains): both emit
  `func(to int) int { return Transfer(from, to, amount) }` — a reordering
  closure created at the call site. Identical.
- `transfer ~amount: m` (two args remain): style A emits the nested
  `func(from) func(to)`, style B emits flat `func(from, to)` — and the
  usual apply-time difference applies on completion (16.4 ns + 1 alloc vs
  1.9 ns + 0 alloc for an opaque partial).
- The reordering closure is always created at the call site where the
  labels are given; source-order temporaries preserve evaluation order
  (verified by `TestLabeledEvalOrder` in both packages). The closure body
  must call the flat declared form, not the value form — in style A a
  naive implementation could be tempted to route through the curried
  chain, re-introducing per-call allocs.
- Remaining *labeled* args keep their labels in the Gom type but are
  positional in Go; fine in both styles.

### 5. `List.map`-style specialization escape hatch

- Style A requires a second, flat-parameter variant of every hot HOF
  (`Fold` + `FoldFlat`), plus `UncurryN` adapters when the value arrives
  in curried form. The flat variant only helps if the function *origin*
  is flat; converting a curried value still pays the chain per call
  (measured: `curried-uncurry` ≈ `curried`, 21.6 µs vs 21.0 µs).
- Style B needs no specialization for arity reasons — the declared form
  is already flat. Specialization remains available for other reasons
  (e.g. unrolling), orthogonal to representation.

## Other qualitative points

- **Codegen complexity**: style A needs two coexisting forms for every
  multi-arg function (direct + curried value) and the IR must track
  which; style B has one form, with flatten wrappers only for
  function-returning defs (`declared arity < arrow length`, rare).
- **`fn mk_add (a) -> (Int -> Int)`**: style A gets this for free (the
  curried form is natural here); style B emits `func(int) func(int) int`
  for direct calls and `MkAddFlat` for value use. This is the one case
  where style A is simpler — but function-returning defs are far rarer
  than partial application and HOF use.
- **Debuggability**: curried call chains put anonymous `funcN` frames in
  stack traces; arity keeps real function frames.
- **Codegen must not flatten across declared arity**: `fn g (a) = { print
  a; fn b -> e }` must run its outer body at `g x`, so style B compiles
  it to the declared-arity form (shown by `MkAdd`/`MkAddFlat`).
- **Rejected alternatives** (not implemented, reasoning only):
  interface/box-based "function objects" (`type Fn2[A,B,R] interface{
  Apply1(A) Fn1[B,R]; Apply2(A,B) R }` or `struct{ f any; args []any }`)
  lose static types, add interface dispatch and boxing, and are worse
  than either candidate on every axis above.

## Recommendation

**Adopt style B — the arity-aware representation: a Gom value of type
`T1 -> ... -> Tn -> R` is the Go type `func(T1, ..., Tn) R`.**

Rules implied by the measurements:

1. The canonical Go type of a Gom function type flattens the whole arrow
   chain.
2. A def `fn f (a1)...(am) = e` compiles to `func f(A1,...,Am) R'` —
   honoring declared arity so outer-body evaluation order is preserved.
   `R'` may itself be a flattened func type.
3. Value-position use of `f` is direct when m equals the arrow length of
   f's type; otherwise emit a small flatten wrapper
   (`func(x1..xn) { return f(x1..xm)(xm+1..xn) }`).
4. Every application — known or unknown — compiles to a single Go call
   with as many arguments as the remaining static arity allows; partial
   application emits a closure of the remaining arity.
5. `CurryN`/`UncurryN` adapters exist only if needed for rare Go APIs
   that themselves take/return curried shapes (not needed for the stdlib
   patterns tested).

Style A (curried) is rejected: no measured case where it wins, a forced
allocation chain on every boundary-crossing application, a per-element
allocation inside every multi-arg HOF, adapters at every Go interop
boundary, and an unidiomatic public surface.

## Suggested update for decisions/2026-11-01-function-representation.md

```
- 状態: 採用（P0-2 の実験で決定。根拠: docs/sketch/2026-11-01-p0-2-function-representation.md）

## 決まったこと（追記）

- 関数値は arity 付きの形で持つ。Gom の型 `T1 -> ... -> Tn -> R` の値は
  Go の `func(T1, ..., Tn) R`（矢印チェーンを平坦化した形が canonical）
- 定義 `fn f (a1)...(am) = e` は宣言どおりの arity の Go 関数
  `func f(A1,...,Am) R'` にする（外側の本体が外側の呼び出しで評価される順序を保つため）
- 値として使うとき: 宣言 arity = 型の矢印長ならそのまま参照。宣言 arity が
  小さい（関数を返す関数）場合だけ平坦化ラッパーを生成する
- 呼び出しは既知・未知を問わず、残りの arity ぶんの引数で 1 回の Go 呼び出しにする。
  部分適用は残り arity のクロージャを呼び出し側に生成する
- カリー化形 `func(A) func(B) C` は不採用。実測で、境界を越えた関数値の適用に
  n-1 個の中間クロージャ割り当て（arity 3 で ~43 倍）、高階関数のループ内で
  要素ごとの割り当て（fold で ~87 倍）、Go API への受け渡しに Uncurry アダプタが
  必要（SortFunc で ~12 倍）だった
- ラベル付き引数の部分適用は両方式で共通: ラベルが宣言順とずれる呼び出し側で
  並べ替えクロージャを作り、本体は常に宣言形（フラット）を呼ぶ
```
