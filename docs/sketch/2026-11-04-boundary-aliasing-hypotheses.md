# 境界での別名と可変性 — 仮説（検証済み）

- 状態: 検証済み — 各仮説の判定は末尾の「検証結果」節（2026-11-05, `wt/boundary-aliasing`）
- 日付: 2026-11-04
- 関連: [decision: goconv-list-slice-copies](../decisions/2026-11-04-goconv-list-slice-copies.md)、[problems: shared-slice-view-type](../problems/2026-11-04-shared-slice-view-type.md)、[decision: go-interface-assignment-and-fields](../decisions/2026-11-03-go-interface-assignment-and-fields.md)

## 問い

`Go.conv` の `Go.Slice T ↔ List T` がコピーしない（pun）問題は、スライスに限った話か。struct やレコードでも同じことが起きるか。struct 自体の可変性（open-questions「Go の struct 型の構築・書き込み」）とどう関係するか。

整理の軸は、値の **持ち主**（Gom / Go / 借りているだけ）× **可変性**（不変 / 読み取り専用で共有 / 可変）× **可視性**（抽象 / 読み取り専用 / 全公開）。

## 仮説

以下はコードと文書を読んだうえでの推測で、どれも動かして確かめてはいない。

### Gom → Go の struct を読む向き

- **H1**: 値型の Go struct から exported field を読むと、読んだ時点の値を得るだけで別名は生じない（spec 8.3、field は読み取り専用）
- **H2**: `Ptr T` 越しに読むと、Go 側の後からの書き換えが次の読み取りで見える。`Ptr` は W0701 の可変リストに入っているので、これは意図どおり
- **H3**: Go struct の `[]T` 型の field を読んだときの扱いが決まっていない。decision go-interface-assignment-and-fields は「container copy conversion は入れない」としているので、E0501 になるか、`List` へコピーするか、`Go.Slice` で返るかを確かめる必要がある
- **H4**: field への代入や struct の構築（open-questions）を許すと、`Go.Slice` に書き込みを許すのと同じ問いになる — 「読み取り専用で借りる」と「可変で共有する」をどう区別するか

### Go → Gom のレコード・`List` を受け取る向き（spec 8.7）

- **H5**: `List` は `list.List[A] = []A` なので、生成された Gom の API から `List` を受け取った Go のコードは、元の配列を書き換えられる。その `List` が Gom 側のトップレベルの `let` や他の値と共有されていれば、Gom から見た不変性が破れる
- **H6**: Gom のレコードは Go の struct（値）として生成される。Go 側が field に代入しても、変わるのは Go 側のコピーだけ。ただし field に `List` があれば、H5 と同じく元の配列は共有されている
- **H7**: sum type のバリアント（sealed interface を実装する struct）も、ポインタで渡っていれば H6 より強く共有される可能性がある（表現を要確認）

つまり、「`List` は不変」という保証は Gom の内側でしか成り立っておらず、境界は Go 側の行儀に頼っている、という仮説。

## 他言語との比較（記憶ベース。ドキュメントで未確認）

- **MoonBit**: field は `mut` を付けない限り不変。struct の可視性は、修飾なし（外からは抽象）、`pub`（読み取り専用、外から構築・変更できない）、`pub(all)`（外から構築・変更できる）の 3 段階。Gom がいま Go の struct を扱う形は `pub` 読み取り専用に近い
- **OCaml**: レコードは `mutable` 指定の field 以外は不変で、更新は作り直し。C の FFI では値のブロックへのポインタをそのまま渡し、C 側が `Store_field` などで書き換えることもできる — 不変を守るのは C 側の責任（言語は防がない）

## 検証で決めたいこと（方向の候補。どれも未決）

1. OCaml 型: 不変は Gom の内側だけの保証とし、spec 8.7 に「Go 側は受け取った `List` を書き換えてはならない」と契約として明記する
2. 境界でコピー: 生成した API の入口と出口で `List` をコピーする（spec 8.5 の Go 呼び出し側と対称になる。コストがかかる）
3. 型で防ぐ: `List` を Go から見て抽象（メソッド経由の読み取りのみ）にする — MoonBit の `pub` 相当。生成コードを Go から使う書き味（[generated-code-goals](../decisions/2026-11-01-generated-code-goals.md)）とのトレードオフ

## 検証の手順（案）

- H1–H3: `testdata/gocall` 風の fixture に、値型の struct・ポインタ・`[]T` 型 field を持つ Go 型を置き、Gom から読んで Go 側で書き換えた後の値を観測する
- H5–H7: Gom で `List` / レコード / バリアントを返す API を生成し、Go のテストから書き換えて、Gom の関数を再度呼んだときに変化が見えるかを観測する
- MoonBit・OCaml の記述は公式ドキュメントで確かめてから残す

---

## 検証結果 (verification, 2026-11-05, `wt/boundary-aliasing`)

All values below are observed results, not code reading. The e2e
driver `cmd/gom.TestEndToEnd_GoCallsGom` runs `gom check` → `gom
build` → `go build` → `go test` over the fixture module.

### Fixtures added

| file | role |
| --- | --- |
| `testdata/gocall/box/box.go` | Go-owned `Box{N, Name, Xs []int64, Is []int, XXs [][]int64}` behind one shared `*Box` (`Shared()`), a value-copy constructor (`Snapshot()`), and in-place mutators (`MutateN`/`MutateX`/`MutateXX`/`Reset`) |
| `testdata/gocall/boundary/boundary.gom` + `boundary.gomi` | probes: `h1_value`, `h2_ptr`, `h3_slice_field`, `h3_slice_field_value`, `h3_nested_field`, plus the shared `List`/record/variant surface (`shared`, `sharedList`, `sharedHead`, `freshList`, `echo`, `getPair`, `pairXsHead`, `getWrap`, `getNum`, `wrapXsHead`) |
| `testdata/gocall/boundary/boundary_test.go` | Go consumer asserting every observed value |
| `testdata/spec/ok_go_slice_field.gom` | `[]int64` field reads as `List Int` — pointer and value receivers, conformance into `List.length`, and the `Go.Slice` escape going through a second copy |
| `testdata/spec/E0501_go_slice_field_split.gom` | `[]int` field is hidden — E0501 at the selector |
| `testdata/spec/E0101_go_field_write.gom` | `b.N = 5` fails at the parser |
| `testdata/goiface/fixture.go` | gained `SliceBox{N, Xs []int64, Is []int}` for the spec pins |

### H1 — confirmed

`h1_value` binds `b: box.Box` from `box.Snapshot()` (a `*shared`
copy), reads `b.N`, runs `box.MutateN 100` on the Go side, then
re-reads `b.N` plus a fresh `p.N` through `box.Shared()`:
observed `(1, 1, 101)`. The generated code is a plain `tmp := b.N`
at the source position — a value-typed field read produces a
detached scalar copy; no alias is created, and the copied struct
never sees Go's later write.

### H2 — confirmed

`h2_ptr` reads `p.N` through `Ptr box.Box`, mutates via
`box.MutateN 100`, re-reads: observed `(1, 101)`. Reading through a
`Ptr` observes Go's own state — consistent with `Ptr` being in the
W0701 mutable set (`Ref`/`Array`/`Ptr`/`Go.Slice`/`Go.Map`,
[decision](../decisions/2026-11-04-mutable-sharing-lint.md)).

### H3 — resolved, and refined

The `[]T`-typed field reads as **`List T` — a per-read boundary
copy** — not `Go.Slice`, not E0501. `FieldOf` maps the field type
through `mapBoundaryType` (the same mapping a parameter or result
gets), so spec 8.5's `[]T → List T` copy applies to field reads:
generated code is `tmp := p.Xs; before := list.FromSlice(tmp)`
(`ConvSliceFromGo`). Observed `h3_slice_field` = `(10, 99)`: the
pre-mutation binding keeps `[10 20 30]`'s head while the second
read snapshots `[99 20 30]`. Each selector evaluation copies, so
repeated reads track Go's mutations one snapshot at a time.

Sub-findings:

- **element-split fields stay hidden**: `Is []int` reports E0501
  "type []int splits int and int64 inside a container" at `.Is` —
  the same `divergentReason` as `[]int` parameters
  (`E0501_go_slice_field_split.gom`).
- **the copy is one level deep**: `XXs [][]int64` reads as
  `List (Go.Slice Int)` — the outer slice clones (`list.FromSlice`)
  while every element stays a `Go.Slice` sharing Go's backing.
  `h3_nested_field` observed `(55, 55)`: the inner write
  `MutateXX(0,0,55)` is visible through *both* the pre-mutation
  "snapshot" and a fresh read. A `List (Go.Slice Int)` is an
  immutable-typed value carrying mutable, Go-owned elements —
  consistent with the documented nested-slice rule, but it means
  "`List` is immutable" holds only element-wise.
- **no shared-view escape**: annotating the read `Go.Slice Int`
  still conforms — through the `List → Go.Slice` conversion
  (`xs.ToSlice()`, a second copy). There is no way to obtain a
  `Go.Slice` that aliases a struct's slice field
  (`ok_go_slice_field.gom` fn `v`).
- corollary: a detached value copy's `[]T` field still aliases Go's
  backing in Go's representation (`Snapshot()`'s `Xs` is the same
  slice) — only the copying field-read insulates it. If the field
  read had yielded `Go.Slice`, `h3_slice_field_value` would observe
  the mutation; observed `(10, 77)` instead.

### H4 — premise confirmed, question stands

`b.N = 5` is E0101 at the parser — there is no assignment syntax at
all, so "field writes" cannot be exercised today
(`E0101_go_field_write.gom`); Go struct literals likewise do not
exist. The borrow-read-only vs shared-mutable question the
hypothesis raises lands on whichever decision introduces writes —
unchanged.

### H5 — confirmed, and stronger than hypothesized

`let shared : List Int = [1, 2, 3]` generates
`var Shared list.List[int64]` and `SharedList` returns it verbatim
(`return Shared`). Observed in `boundary_test.go`:

- `got := SharedList(); got[0] = 99` → `SharedHead()` reads `99`:
  a returned `List` is the package var's own `[]int64`; Go's write
  reaches Gom's package state.
- the exported `let` is itself a writable Go package var:
  `boundary.Shared[1] = 42` and wholesale rebinding
  `boundary.Shared = []int64{7}` both take effect — stronger than
  the hypothesis's element-write: the *binding* is mutable, not
  just the contents.
- **`List` parameters are un-copied at this surface too**: `echo`
  is `func Echo(xs list.List[int64]) list.List[int64] { return xs }`
  — mutating the caller's `in[0]` after the call is visible in
  `out[0]`. The spec-8.5 copies live at *Gom's own call sites*;
  the generated API surface takes and hands back raw `[]int64` in
  both directions. Spec 8.5 does not pin down this surface —
  candidate gap-ledger note (below).
- nuance for the "shared top-level let" precondition: a
  *generalized* `let` (`let e = []`) emits a niladic thunk
  `func e[A any]() list.List[A]` — fresh per call, nothing shared
  (`testdata/golden/let_poly.golden.go`). The hazard needs a
  monomorphic binding (annotated or concrete), which is what emits
  a package `var`. A per-call literal (`freshList`) shares nothing
  either.

### H6 — confirmed with the predicted caveat

`var pairVal Pair = Pair{N: 7, Xs: …}`; `GetPair` returns the
struct by value. Observed: `p.N = 99` stays Go-local
(`GetPair().N` still `7`) while `p.Xs[0] = 99` reaches the package
(`PairXsHead()` → `99`). Records copy; `List` fields inside them
still share backing.

### H7 — refined: no stronger sharing

Variants are **value structs** behind the sealed interface
(`Wrap_WList`, value receiver `isWrap()`) — not pointers, so the
hypothesis's "if passed by pointer" case does not arise:
`w.(boundary.Wrap_WList)` copies the struct exactly like the
record. Sharing is gated entirely by payload representation —
`wl.Xs[0] = 99` reaches `WrapXsHead()` → `99` through the `List`
payload's backing, while `GetNum().N` stays `8` after `n.N = 99`.
Payloads typed `Ref`/`Ptr`/`Array`/`Go.Slice`/`Go.Map` would share
mutable cells verbatim — consistent, since those types are mutable
inside Gom too.

### MoonBit / OCaml — checked against official docs (one correction)

- **MoonBit**: mostly confirmed. Struct fields are immutable unless
  declared `mut`; type visibility is **four** kinds, not three —
  `priv` (invisible), default = abstract (name only, representation
  hidden), `pub` = readonly (visible representation; outside
  construction *and* mutation forbidden), `pub(all)` = fully public
  (construct/read/modify where the fields allow). Gom's read-only
  exported-field view of Go structs indeed matches `pub` readonly.
  Sources:
  [Managing Projects with Packages](https://docs.moonbitlang.com/en/stable/language/packages.html),
  [tour: mutable fields](https://tour.moonbitlang.com/custom-types/mutable-field/index.html).
- **OCaml records**: confirmed — immutable unless the field
  declaration carries `mutable` (`field-decl ::= [mutable]
  field-name : …`); update is `r.f <- v` on mutable fields or
  functional update `{ r with f = v }`. Sources:
  [manual 5.4 typedecl](https://ocaml.org/manual/5.4/typedecl.html),
  [docs: Mutability and Imperative Control Flow](https://ocaml.org/docs/mutability-imperative-control-flow).
- **OCaml C FFI**: confirmed — records are zero-tagged heap blocks;
  a C stub receives the `value` pointer itself and can read
  (`Field(v, n)`) *and* write (`Store_field(b, n, v)` →
  `caml_modify`) — nothing in the FFI prevents writing an
  "immutable" block; the discipline is on the C side (the `Obj`
  API spells it out: `set_field` "MUST NOT be called on immutable
  blocks"). Same posture Gom's `List` takes toward Go callers.
  Sources:
  [manual 5.5 intfc §2.2 blocks / §4.3 accessing blocks](https://ocaml.org/manual/5.5/intfc.html),
  [Obj](https://ocaml.org/manual/5.4/api/Obj.html).

### Post-verification summary

| value shape | mutability visible to Go |
| --- | --- |
| `List` param / result / exported `let` | raw `[]int64` both ways — element writes reach Gom state; the `let` var is also rebindable |
| record / variant | Go gets a struct copy — only `List`/reference-representation payloads share backing |
| Go struct field read | scalar = snapshot (H1); `Ptr` = observes Go writes (H2); `[]T` field = per-read `List` copy (H3), shallow at one level |

### TODO / ledger candidates surfaced here (not filed — reported)

- **exported `let` → writable package var**: a `.gomi` `let` emits
  `var Shared …` — Go can rebind the binding itself, the sharpest
  observed violation of Gom-side immutability. If `.gomi` constants
  are meant to be read-only, an accessor or façade is needed; if
  the `var` is intended, spec 7/8.7 should say so. Contract gap,
  not a compiler bug.
- **spec 8.5 copy scope**: "両方向ともコピーする" is observed only
  at Gom's own call sites; the generated-API surface (spec 8.7)
  passes `list.List[A]` verbatim in both directions. Either 8.5
  should scope itself to Gom-originated calls, or the exported
  surface needs defensive copies (cost). Candidate
  `implementation-gaps.md` entry — spec ambiguity.
- **`List (Go.Slice Int)`**: nested `[]T` materializes an
  immutable-spelled type holding mutable Go-owned elements (H3
  sub-finding). Documented behavior, but the "`List` is immutable"
  guarantee deserves a per-element caveat wherever it is stated.
