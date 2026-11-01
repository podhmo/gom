# P0-4: `Result[T, E]` representation and the `(T, error)` boundary

- 目的: choose the Go representation of Gom's `Result a e` and the
  shape of the `(T, error)` <-> `Result` conversion, per TODO.md P0-4.
- 関連: [decisions/value-representation](../decisions/2026-11-01-value-representation.md)
  (`Result` row of "P0 で決めること"),
  [decisions/result-pipe-auto-wrap](../decisions/2026-11-01-result-pipe-auto-wrap.md),
  [design.md](../design.md) 4.5/4.6 (`try` / `|?>` codegen),
  spec.md 3.1 (`GoError`), 8.3 (`(T, error)` -> `Result T GoError`),
  8.6 (invalid values), 9 (`|?>`).
- 実験コード: `experiments/p0/result-repr/` (standalone module
  `github.com/podhmo/gom/experiments/p0/result-repr`, go 1.27).
- 測定環境: go1.27.1 linux/amd64, AMD Ryzen 7 255,
  `go test -bench=. -benchmem -benchtime=1s`. 2026-11-02.

## Candidates

| package | representation | notes |
| --- | --- | --- |
| `sealed` | `Result[T,E] interface{ isResult() (T,E) }` + `Result_Ok[T,E]{V0 T}` / `Result_Err[T,E]{V0 E}` | the design.md 4.1 ADT encoding; marker fixed per the P0-1 finding (parameters in the signature, otherwise inference cannot bind them) |
| `flat` | `struct { Value T; Error E; Ok bool }`, all fields exported | the rep named in TODO.md |
| `flat2` | `struct { state ResultTag; Value T; Error E }`, tag unexported, `0=invalid / 1=Ok / 2=Err` | my refinement of `flat`: same layout and cost, but the zero value is invalid instead of reading as `Err(zero E)` |

`flat2` was added because the zero-value question (axis 4) turned out
to be the decisive axis, and the bool version's failure mode there is
bad enough to disqualify it (see below).

## Benchmarks

`Result[int64, error]` unless noted; `payload.Big` is a 1056-byte
struct (128×int64 + int64). "fused" = design.md 4.6 codegen
(`v, err := f(); if err != nil { return Err... }`), "naive" = a
`ResultFromGo` conversion materializing a Result at every step.

| benchmark | sealed ns/op | flat ns/op | flat2 ns/op | sealed alloc | flat alloc |
| --- | --- | --- | --- | --- | --- |
| ConstructOk | 8.8 | **0.49** | **0.44** | 8 B ~0† | 0 |
| ConstructErr | 16.3 | **0.56** | 0.56 | 16 B, 1 | 0 |
| ConstructOkBig (1 KB) | 400 | **41.7** | — | 1152 B, 1 | 0 |
| ConstructOkSlice | 8.4 | **0.21** | — | 8 B ~0† | 0 |
| FromGoPair Ok (fused) | 8.5 | 7.8 | — | 0 | 0 |
| FromGoPair Err (fused) | 54.2 | 39.2 | — | 65 B, 3 | 49 B, 2 |
| ResultToGo Ok | 1.5 | **0.43** | — | 0 | 0 |
| ResultToGo Err | 1.7 | **0.43** | — | 0 | 0 |
| GetOrElse Ok | 1.4 | **0.21** | 1.4 | 0 | 0 |
| GetOrElse Err | 1.5 | **0.21** | — | 0 | 0 |
| Pass small | **0.42** | 0.49 | — | 0 | 0 |
| Pass big (1 KB) | **0.42** | 13.9 | 13.9 | 0 | 0 |
| ParsePort Ok, fused | 29.6 | 12.5 | **11.5** | 16 B, 2 | 0 |
| ParsePort Err step1 | 237 | 223 | 231 | 224 B, 7 | 208 B, 6 |
| ParsePort Err step2 | 235 | 202 | — | 168 B, 7 | 136 B, 5 |
| ParsePort Ok, naive | 38.4 | 12.8 | — | 24 B, 3 | 0 |
| ParsePort Err step1, naive | 257 | 217 | — | 240 B, 8 | 208 B, 6 |
| ParseSum Ok (`try`×2) | 8.3 | 7.0 | **6.8** | 0 | 0 |
| ParseSum Err | 51.1 | 34.6 | — | 65 B, 3 | 49 B, 2 |
| LoadBig Ok | 435 | **83.1** | — | 1152 B, 1 | 0 |
| LoadBig Err | **70.1** | 107 | — | 32 B, 2 | 16 B, 1 |
| GetOrElse big | 82.7 | 68.7 | — | 0 | 0 |

† pointer-free 8-byte boxes amortize in the tiny size class — `allocs/op`
rounds to 0 but `B/op` shows real garbage (same effect as P0-1).

## Axis-by-axis findings

### 1. Calling convention — `(T, error)` <-> `Result`

**Go -> Gom** (the `(T, error)` -> `Result` mapping, spec 8.3). Two
shapes were measured:

- *Fused* (what design.md 4.6 emits inside `|?>` / `try`): the err
  check doubles as the Err arm, so no Result exists on the success
  path at all. Cost = the underlying Go call (~8 ns for
  `strconv.ParseInt`) plus one boundary Result construction:
  **~8.5 ns sealed / ~7.8 ns flat** on Ok — nearly identical, because
  the sealed box amortizes. On Err: 54 vs 39 ns — sealed pays one
  extra 16-byte box around the already-allocated `error`.
- *Naive* (`ResultFromGo` per step): sealed pays +1 alloc/step and
  ~30% more time in a 2-step chain (38.4 vs 29.6 ns). Flat is
  insensitive (12.8 vs 12.5) — its "conversion" is free anyway.

**Gom -> Go** (`ResultToGo` / `Unwrap`): flat unwraps by reading two
fields (0.43 ns); sealed pays a type switch (1.5 ns). Both trivial;
flat's version is also the more idiomatic-looking Go.

The fused conversion is confirmed right for both reps — keep design.md
4.6 as written. Its benefit is largest for sealed (one fewer box per
step); it is the difference between sealed being merely worse and
being much worse on `|?>` success paths.

### 2. `|?>` intermediate-value elision

Both reps support design.md 4.6's codegen exactly as specified: each
step expands to an err check + early return; the only Result that ever
exists is (a) the return value of a Gom step function, inspected in
place (`r.(Result_Err)` comma-ok / type-switch arm vs `!r.Ok` /
`r.Tag()`), and (b) the function's own boundary Result.

Measured on a 2-step chain (Go call then Gom step), success path:
**sealed 29.6 ns + 2 allocs / flat 12.5 / flat2 11.5, 0 allocs.**
Sealed's overhead is ~1 box per Result-returning step (16 B here) —
the `checkPortRange` return must box its `Result_Ok`. In a chain of k
Gom steps sealed pays k boxes; flat pays none.

Caveat discovered while writing the sealed version: the elision check
must keep a `default: panic(rt.InvalidVariant)` arm (i.e. a real type
switch). A bare comma-ok assertion (`if e, failed :=
r.(Result_Err[...]); failed`) routes a nil Result to the Ok path,
where extracting `V0` panics with the generic
`interface conversion: ... is nil` message — same arm `rt.InvalidVariant`
guards in every other `match` (test `TestNaiveAssertMisroutesNil`).

### 3. Large value types (1 KB payload)

The pattern is "construction cheap in flat, passage cheap in sealed":

- Construct Ok: **flat 42 ns vs sealed 400 ns** — a 1 KB `memcpy`
  beats a 1 KB heap box. Same for the full boundary call
  (`LoadBigOk`: 83 vs 435).
- Pass an existing Result by value: **sealed 0.42 ns vs flat 14 ns** —
  a 2-word interface vs a 1 KB struct copy, ~33×.
- Destructure (`GetOrElseBig`): flat 69 vs sealed 83 — both pay the
  1 KB unbox/field copy; the type assertion adds a little.
- Err path with big T: **sealed 70 vs flat 107** — sealed's Err box is
  16 B regardless of T; flat still copies the full 1 KB struct.
- Slices: `[]Result[int64,error]` is 32 B/elem contiguous in flat vs
  16 B/elem of interface + scattered 8-byte boxes in sealed.

Net: for big-T `|?>` chains (construct → return → destructure once)
flat still wins end-to-end. Sealed's edge only shows when a Result is
*stored or relayed* through several more boundaries — rarer for a
calling-convention type than for stored data.

### 4. Zero value — the decisive axis

- **sealed**: `var r Result` is a nil interface — the single invalid
  value, caught by the `default` arm: `gom: invalid sealed.Result
  value <nil>: not a declared variant`. A typed nil
  `(*Result_Ok)(nil)` lands the same way (variants are value types).
  `ResultToGo(nil)` panics — it cannot manufacture a success.
- **flat**: `var r Result` is `{0, nil, false}` — which *reads as a
  legal `Err(zero E)`*. No invalid state exists at all, so:
  - `match` takes the Err arm with `Error = nil`, silently;
  - worse, `ResultToGo(zero)` returns `(zero T, nil)` — **a Go
    caller's forgotten `var r` becomes a silent success with a zero
    value**, the worst failure mode available;
  - if the nil error reaches the `|?>` auto-wrap, the result is
    `fmt.Errorf`'s `%!w(<nil>)` garbage *and a non-nil error*
    (`TestWrapNil`);
  - corrupt combinations (`{Value: 1, Ok: true, Error: set}`) are
    unrejectable — no arm to check.
- **flat2**: `var r Result` has tag 0 = invalid: `GetOrElse`,
  `ResultToGo`, and generated `match` all panic with `InvalidVariant`,
  matching sealed's failure surface. A Go-written literal
  `Result{Value: 1}` (tag can't be set outside the package) also
  panics. Cost: nothing measurable — `ParsePortOk` 11.5 vs 12.5 ns,
  same layout size; only `GetOrElseOk` ticks up to 1.4 ns (3-way tag
  switch vs bool if).

One shared hole remains in every rep: `Err(nil)` — `Result_Err{V0:
nil}` (sealed) or `Err[T](nil)` (flat/flat2) — is a *valid* variant
carrying a degenerate payload; nothing catches it, and the wrap path
renders it as `%!w(<nil>)`. This is a payload-level hole identical in
kind to `Option_Some{nil}` in P0-1 — the tag fixes only the rep-level
zero. Note that `Err(nil)` is unreachable from *generated* code (the
`(T, error)` conversion maps nil error to Ok), so the hole only bites
Go-side misuse.

`Is Result{Ok:false} a valid Err or invalid?` — with the bool
discriminant it is unavoidably a valid (degenerate) `Err(zero E)`;
there is no bit pattern left for invalid. With a 3-state tag the zero
value is invalid at zero cost.

### 5. Go-side ergonomics

- sealed: construct via composite literals (`Result_Ok[V]{...}`),
  match via type switch — consistent with every other generated ADT,
  but the type-switch ceremony is heavier than what a Go programmer
  writes for error handling.
- flat: `r.Ok`/`r.Value`/`r.Error` with direct literals — the most
  Go-native shape, but unsafe (above). `Ok(3)`/`Err(e)` helpers cannot
  infer the other type parameter — Go callers must write
  `Ok[int64, error](3)`; generated code always knows the args.
- flat2: `r.IsOk()`, `v, err := r.Unwrap()`, `r.Tag()` for
  decision-tree switches; constructors are functions because the tag
  is unexported. Slightly less sugary than flat's exported bool,
  strictly safer.
- `==`: equivalent in all reps — fieldwise/interface equality; error
  payloads compare by Go's interface equality (identity for
  pointer-shaped errors) in all three.

### 6. `%w` wrapping path

Identical in all reps — the wrap happens on the `error` value, not the
Result. `fmt.Errorf("step: %w", err)` preserves `errors.Is`/`errors.As`
(verified: `*strconv.NumError` reachable through the wrap). Also
reproduced the already-noted wart: `strconv.ParseInt`'s error already
contains the function name, so the auto-wrap prints
`strconv.ParseInt: strconv.ParseInt: parsing "notanum": ...` — the
double-prefix problem recorded in result-pipe-auto-wrap's 保留 section.

### 7. E = `error` vs E = `GoError`

spec 8.3 maps `(T, error)` -> `Result T GoError`, and spec 3.1 lists
`GoError` as a nominal builtin. Three instantiations were tried:

- `Result[T, error]` — simplest; works everywhere.
- `type GoError = error` (alias) — identical type; the name exists for
  documentation only. Zero cost.
- `type GoError interface{ error }` (named interface) — *values*
  convert implicitly both ways (identical method sets), but
  **instantiations are invariant**: `Result[int, GoError]` does not
  satisfy `Result[int, error]` — `isResult() (int, GoError)` !=
  `isResult() (int, error)`. A Gom package exporting
  `Result[T, GoError]` would be incompatible with Go helpers
  instantiated on `error`. (Verified: compile error, `goerr/`.) This
  is a compile-time failure not silent misbehavior — the fixed marker
  makes it loud — but it is pointless friction.

Recommendation: instantiate E with the builtin `error` in generated
code; keep `GoError` as the Gom-side nominal name (optionally an alias
for readability of generated signatures).

## Discussion

Result differs from a general ADT in kind: it is Gom's **failable
calling convention**, not stored data. Its lifetime is
construct→check→destructure, dominated by exactly the operations flat
wins by 2–20× — and it is the single hottest sum type (every failable
function boundary produces one). Sealed's structural advantages
(boxed-sharing for stored/passed-around values, uniformity with the
P0-1 ADT encoding, open-variant symmetry) are wasted on it: there is
no variant extensibility to protect, and the `|?>` elision already
keeps Results out of the dataflow between steps.

The one real win sealed holds is the invalid-value surface — and the
experiment shows that surface is recoverable in the flat layout by
replacing `Ok bool` with an unexported 3-state tag, at zero measured
cost. That leaves exported-bool `flat` with no justification: its
zero value isn't merely weird, it actively manufactures a fake
success at the `(T, error)` boundary.

The cost of choosing flat/flat2 is codegen non-uniformity: `Result`
gets a bespoke encoding while user ADTs keep the sealed interface —
constructors become calls (`result.Ok[T,E](v)`), `match` on a Result
emits a `Tag()` switch instead of a type switch. Acceptable: `Result`
is already the special type (`try`, `|?>`, auto-wrap all special-case
it), and the decision-tree compiler can treat the tag as an ordinary
3-way discrimination.

If the project later prefers strict uniformity over this performance
profile, sealed is a valid fallback: it is ~10–20 ns/call slower on Ok
paths, boxes once per Result-returning call, and is clunkier for Go
consumers — but needs no bespoke encoding.

## Recommendation

**Adopt flat-with-invalid-tag (the `flat2` shape) for `Result a e`:**

```go
type Result[T, E any] struct {
	state ResultTag // unexported: 0 = invalid (zero), 1 = Ok, 2 = Err
	Value T
	Error E
}
```

with generated `Ok[T,E](v)` / `Err[T,E](e)` constructors,
`Tag() ResultTag` for decision-tree matching, `IsOk()` and
`Unwrap() (T, E)` for Go ergonomics, and `default:
panic(rt.InvalidVariant)` on every generated Result discrimination.
(`Value`/`Error` stay exported for readability and Go-side
destructuring; post-construction field writes can still corrupt — same
trust model as variant fields in sealed.)

**Conversion approach** (unchanged from design.md 4.6, now measured):

- `(T, error)` -> `Result` fuses into the err check; never call a
  per-step conversion helper inside `|?>`/`try` (the naive form costs
  sealed +1 box/step).
- `Result` materializes only at Result-returning call boundaries.
- `Result -> (T, error)` for Go consumers is `Unwrap()`/`ResultToGo`.
- `E` instantiates as builtin `error` (`GoError` as alias/name only);
  do not emit a named `GoError` interface as a type argument.
- The `%w` auto-wrap is rep-agnostic; the `Err(nil)` ->
  `%!w(<nil>)` garbage path is unreachable from generated code and
  shared by all reps anyway.

## Proposed update to decisions/2026-11-01-value-representation.md

「決まったこと」に追加し、「P0 で決めること」の `Result` 行を消す:

> `Result a e` は `struct { state ResultTag; Value T; Error E }`（tag は
> 非公開、0=invalid/1=Ok/2=Err）。sealed interface ではなく、bool の
> 判別子でもない: bool 版はゼロ値が `Err(zero E)` として読め、`(T,
> error)` 境界で `(zero, nil)` の偽の成功を作るため。tag 版はゼロ値を
> `rt.InvalidVariant` の対象に戻しつつ、構築・判定・分解・`(T,
> error)` 変換で sealed interface の 2〜20 倍の性能を保つ（Ok パスで
> ボックス割り当てなし）。`E` は組み込みの `error` を型引数に使い、
> 名前付き `GoError` interface は使わない（generic instantiation の
> 不変性により `Result[T, GoError]` が `Result[T, error]` と別型に
> なる）。match 上の `Result` の分解は `Tag()` スイッチ＋
> `default: InvalidVariant`。ユーザー定義 ADT は P0-1 のとおり
> sealed interface のまま（Result だけの特別扱い）。
> 根拠: docs/sketch/2026-11-02-p0-4-result-representation.md。

Note: design.md 4.1/4.6 の `Result_Err[...]{V0: ...}` /
`r.(Result_Err[...])` の記述はこの決定を反映して `result.Err(...)` /
`r.Tag()` ベースに書き換える必要がある（P0-6 の反映タスクで扱う）。

## Reproduce

```sh
cd experiments/p0/result-repr
~/sdk/go1.27.1/bin/gofmt -l .          # empty
~/go/bin/go1.27.1 vet ./...
~/go/bin/go1.27.1 test -v ./sealed ./flat ./flat2   # invalid-value logs
~/go/bin/go1.27.1 run ./example                     # ergonomics demo
~/go/bin/go1.27.1 test -run='^$' -bench=. -benchmem ./sealed ./flat ./flat2
```
