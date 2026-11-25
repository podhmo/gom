# nested operand の括弧判定は静的型駆動にする

- 状態: 採用
- 日付: 2026-11-25

## 文脈

compiled の `gomShowN` は operand の静的型から表示 recipe を選び、
opaque な Go 値（underlying が scalar の named Go type を含む）を
composite の中では括弧で包む。evaluator oracle は従来、VRecord /
VVariant / VHost といった値の表現だけを見て括弧を決めていた。
named Go scalar は evaluator では `VFloat` / `VInt` / `VBool` /
`VStr` の scalar payload なので、`type SmallRatio float32` の値を
list / tuple / variant に入れた表示だけ eval と compiled で分岐した
（compiled: `[(0.3)]`、eval: `[0.3]`）。

## 決定

evaluator の nested operand 判定を値駆動から型駆動にする。
compiled の正は変えず、oracle 側に `writeTypedOperand` を持ち、
operand の静的型で括弧を決める。

- builtin scalar は括弧なし
- tuple と builtin List / Array は自身の bracket があるので括弧なし
- record、variant、`GoError`、opaque Go 値、関数値、type-erased
  operand は括弧あり
- underlying が numeric / bool / string の named Go scalar は opaque Go
  値として括弧あり — lane C が揃えたのは scalar payload の表示内容で、
  operand としての nominal category は変えない
- composite の要素・フィールド・variant payload には静的型を再帰的に
  伝え、width-32 の float と unsigned integer の表示も同じ経路で描く

## 却下した案

- eval の named Go scalar を `VHost` に包む → 算術・比較・peel 済み
  record の値意味論を変え、scalar として扱う全経路に影響する
- compiled 側の括弧を外して eval に合わせる → opaque Go 値と scalar の
  区別という gomShow の canonical を崩し、利用者向け表示を後方互換なく
  変える

## 検証

prototype 記録: [wave46 lane A writeOperand verify](../sketch/2026-11-25-wave46-lane-a-writeoperand-verify.md)。

- `splice_go_named_scalar_nested_operands` — list / tuple / variant の
  3 形で `SmallRatio` と `Label` が括弧付きになり、float32 payload は
  `1.2345679` と width-32 精度で evaluator / compiled 一致
  （[compare_test.go](../../internal/build/compare_test.go)）
- `TestEvalUnsignedRecordShape` — foreign named unsigned integer field が
  record 内でも符号なし表示と括弧を保つ
