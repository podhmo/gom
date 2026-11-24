# 8 要素超の spelled tuple — 匿名 struct で emit

- 状態: 採用（wave43 lane A で実装）
- 日付: 2026-11-24

## 決定

`rt` の tuple は `Tuple8` までしかないが、spelled tuple 型の arity
は check で縛らない。9 要素以上を書いた場合、`tupleType` は
`rt.TupleN` ではなく同じ `V0..Vn` フィールドを持つ匿名 struct
（`struct{ V0 A; V1 B; ... }`）を emit する。

## 理由

候補は「check で arity ≤ 8 を診断」「rt を拡張（TupleN を増やす）」
「codegen で匿名 struct に降りる」の 3 つだった。

- 8 個までという上限は Go の多値戻り値のルール（spec 8.4 節）で
  あって、ソース上の tuple 型の arity を縛る根拠はない。診断に
  変えると言語側に根拠のない制限を残す。
- `rt.TupleN` の拡張は機械的だが任意の上限が残り、結局その先で
  同じ問題が再発する。
- 匿名 struct は `rt.TupleN` と同じフィールド名（`V0..Vn`）を
  持つので、構築（`CompositeLit`）と `.Vn` アクセス・構造的等価が
  そのまま動く。型の同一性は構造的で、同じ要素型の tuple 同士は
  8 以下との混在でも正しく一致する（`rt.Tuple8` は named なので
  境界で一致しないが、8 と 9 の比較は起きない）。

## 検証

- golden: `testdata/golden/tuple9.gom`（9 要素の spelled tuple を
  引数パターン・リテラル構築・分解で使用）→ 匿名 struct emit
- E2E: 生成コードを Go module で `go build` + 実行し、
  `big (1,...,9) = 10` を確認 — 検証メモ:
  [docs/sketch/2026-11-24-wide-tuple-verify.md](../sketch/2026-11-24-wide-tuple-verify.md)
- `types.GoRepr`（`Go.try_as` の inhabits 判定）も 8 要素超を
  reflect の綴り `struct { V0 int64; ... }` で返す
