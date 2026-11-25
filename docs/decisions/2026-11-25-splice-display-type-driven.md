# splice 表示は静的型駆動 — scalar 束縛の宣言型も record 形で描く

- 状態: 採用
- 日付: 2026-11-25

## 文脈

`type Flag = {on: Bool}` に `let f : Flag = true` と scalar literal を
代入したとき、`"${f}"` は eval では `true`、compiled では `{ on = true }`
と描かれていた（wave43 lane C のレビューで発見・台帳登録）。eval は束縛値を
scalar のまま保持し（1-field record は peel されて named scalar に emit
されるため値レベルで record を持たない）、compiled の gomShow は静的型から
renderer を選ぶため record 形になる — 同じ値の表示が構築経路で分岐する。

正は compiled 側にある: gomShow は型駆動であり、値がどう構築されたか
（literal materialize か record literal か）で表示が変わることはない。
生成コードでは `type Flag bool` に record 構造は残らず、両者は文字通り
同じ値である。

## 決定

evaluator の splice 表示を型駆動に揃える。値の表現は変えない — scalar は
scalar のまま保持し（算術・等価・peel 意味論を壊さない）、splice で描く
直前に静的型から合成の VRecord 形状を構成して comparison format で描く。

- `displayTyped(v, t)` が一本の入口: width-32 の float は float32 精度で
  描き（wave45 lane B）、それ以外は `typedDisplayValue` の合成形状を描く
- `typedDisplayValue` は非 generic な 1-field record 型の位置にある scalar
  を対応する record 形状に包み、peel chain（`type M = {u: UserId}` →
  `{ u = ({ v = 8 }) }`）・record フィールド・List 要素・tuple 要素に
  型に沿って再帰する
- generic な 1-field record（struct emit）は包まない — compiled も
  struct 形状で出るためずれない

## 却下した案

- eval の束縛値を VRecord 化する → scalar 算術・等価・peel 規則が全て
  影響を受け、値の意味論が構築経路に依存する
- compiled を scalar 表示に寄せる → record literal で構築した同じ値との
  整合が取れず、gomShow の型駆動の原則を壊す

## 検証

- `splice_scalar_literal_record_shape` / `splice_nested_peel_record_shape` /
  `splice_field_record_shape` / `splice_list_tuple_record_shape` —
  oracle と compiled の stdout が一致
  （[compare_test.go](../../internal/build/compare_test.go)）
