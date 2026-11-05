# `Go.conv` の `Go.Slice T ↔ List T` はコピーする

- 状態: 採用（実装・検証した — `Go.conv` のコピー化は testdata/golden/go_conv と testdata/gocall の freeze/thaw/bump で固定、`Go.Slice` 操作面は [検証](../sketch/2026-11-05-go-slice-ops-verification.md)。spec.md §8.4/§8.5 へ反映済み）
- 日付: 2026-11-04

## Context

open-questions: 「`Go.conv` の `Go.Slice T ↔ List T`（raw pun、コピーしない）を、暗黙の境界変換と同じコピーありに揃えるか、現状の pun を spec 8.4/8.5 に明文化するか」。現状の実装は、表現が同じことを使って `list.List[T](xs)` へキャストするだけ（[台帳](../implementation-gaps.md) の Go slice 項目）。コピーしないので、Go 側でスライスを書き換えると、不変のはずの `List` の中身が変わる。

## Decision

- `Go.conv` による `Go.Slice T ↔ List T` の変換は、暗黙の境界変換（spec 8.5）と同じくコピーする（`list.FromSlice` / `.ToSlice()`）
- コピーを避けたいときは `Go.Slice T` のまま扱う。そのために `Go.Slice` に読み取り専用の操作（length / get / 部分の取り出し / fold / to_list 相当）を足す。名前と形は実装時に決める
- `List` と同じ表現を持つ第三の型（View）は今は足さない（[problems](../problems/2026-11-04-shared-slice-view-type.md)）

## Alternatives

- *コピーしない pun を spec に明文化する*: `List` が不変であるという保証が `Go.conv` 1 つで破れる。`Go.conv` は明示的な変換なので、書いた側は気付けるが、受け取った側には分からない。
- *View 型を足す*: [problems](../problems/2026-11-04-shared-slice-view-type.md) を参照。利点はパフォーマンスで、今は必要になっていない。
