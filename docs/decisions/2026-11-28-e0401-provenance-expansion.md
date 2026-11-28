# E0401 expected type provenance の拡張範囲

- 状態: 採用（wave57 lane A で実装済み）
- 日付: 2026-11-28

## 決定

E0401 provenance の次の実装面は **record field の値**に限定する。record type が解決済みで、field declaration の型が declaring type の type parameter を含まない場合だけ、field 値の最終 conformance mismatch に field 宣言位置を `expected type declared here` label として付ける。generic record でも、その field 自体が `n: Int` のように型パラメータを含まなければ対象とする。block 最終式・if/match 腕へ同じ expected type がそのまま供給される経路では、既存の direct let/return provenance と同じ規則で label を伝える。

呼出し引数、lambda 本体、演算子内部、generic record field、unresolved/ambiguous record field には今回の実装で provenance を付けない。

## 事実として正しい経路

| fixture 形 | 例 | label を付けてよいか | 理由 |
| --- | --- | --- | --- |
| monomorphic record field 直接値 | `type R = { x: Int }` / `{ x = "bad" }` | yes | `x: Int` が mismatch の `expected Int` を直接宣言している |
| monomorphic record field の if/match 腕 | `{ x = if c { 1 } else { "bad" } }` | yes | 同じ `Int` が腕の最終 conformance までそのまま供給される |
| record field 内の独立 constraint | `{ x = 1 + "bad" }` | no | `expected a numeric type` は `x: Int` ではなく `+` の constraint から出る |
| type parameter を含む record field | `type Box a = { v: a }` / `Box Int` | no | 宣言は `v: a` であり `Int` を直接宣言していない。`expected type declared here` は事実として偽になる。一方 `type Count a = { n: Int, items: List a }` の `n` は型パラメータを含まないので対象 |
| monomorphic 呼出し引数 | `fn takes (x: Int)` / `takes "bad"` | 意味上は yes | `(x: Int)` は expected type の正しい由来だが、現行の `check.Signature` は parameter span を持たない |
| 呼出し引数内の独立 constraint | `takes (1 + "bad")` | no | record field と同じく constraint の由来は callee 注釈ではない |

## 実装候補の境界

- `checkRecordLit` が解決した `fieldInfo` の `sp` を使う。local type declaration と import `.gomi` の両方で `fieldInfo.sp` は既に保持されている
- field 値は `checkExprAt(f.Value, ft, fi.sp)` に変えるだけでよいが、`fi.typ` が declaring type の type parameter を含む場合は `diag.Span{}` を渡す。substitution 後の型ではなく宣言に書かれた型が exact な場合に限る
- if/match/block へは既存の `checkExprAt` の規則がそのまま効く。join のため detached にした腕、bare `Go.Any`、独立した演算 mismatch へは漏らさない
- 呼出し引数は別途 provenance source が要る。`check.Signature` は `Type` だけを持ち、parameter 注釈 span を持たない。curried/flattened parameter、labeled argument、deferred member replay の index mapping を span 付きで設計してから扱う
- Go function / method の Go source 注釈位置は `.gom` の位置へ戻せないため、この decision の対象外

## 却下・後送り

- 呼出し引数 provenance: 後送り。意味上は有用だが、partial に local function だけへ付けると import/builtin/Go callee と挙動が揺れ、span 所有の設計が必要
- type parameter を含む record field: 後送り。`Box Int` の outer annotation と `v: a` の field declaration の両方を示す複合 provenance が必要で、単一の `expected type declared here` では説明できない。generic record の monomorphic field はこの制限に含めない。型パラメータを含むかは alias 展開前の宣言型式で判断し、`x: Fixed a` は alias `Fixed` が `a` を捨てても対象外とする
- label 文言の変更: 今回は行わない。文言を `field type declared here` などへ分けると既存 E0401 label の意味を広げすぎる

調査記録: [sketch/2026-11-28-wave56-lane-c-provenance-expansion-verify](../sketch/2026-11-28-wave56-lane-c-provenance-expansion-verify.md)。
