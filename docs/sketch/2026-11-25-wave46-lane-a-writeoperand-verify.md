# wave46 lane A writeOperand 型駆動化の検証

2026-11-25。対象は evaluator oracle の nested operand 括弧 canonical。

## 手順

`testdata/goiface` の named Go scalar を使い、list / tuple / variant の
operand 位置で evaluator と compiled binary の splice 出力を比較した。
`SmallRatio` には f64 と f32 で最短 digits の変わる `1.23456789` を使い、
括弧だけでなく width-32 payload も同時に確認する。unsigned integer 側は
`Mode`（uint32）と `Big`（uint64）の record field で確認した。

## 結果

- `TestEvalMatchesCompiled/splice_go_named_scalar_nested_operands` —
  `[(1.2345679)]:((1.2345679), ("x")):Some (1.2345679)` で一致
- `TestEvalUnsignedRecordShape` —
  `{ mode = (4294967295) };{ big = (9223372036854775808) }`
- `splice_generic_list_runtime_fallback` — 型パラメータを残す list は compiled と同じく全体を runtime fallback に倒し、要素へ typed operand 括弧を足さない
- `splice_try_as_named_scalar_operand` — `Go.try_as` が ctor symbol を持たない builtin Option でも payload 型を回復し、`Some (1)` と表示
- `TestEvalRuntimeFallbackKeepsConcreteUnsignedLeaf` — 型パラメータを含む tuple の runtime fallback でも concrete uint64 leaf は `9223372036854775808` と符号なし表示を保つ
- `go test ./internal/eval ./internal/build -count=1` 通過

## 残りの非目的

generic に erase された operand 自体の描画内容は従来どおり Go `%v` と
evaluator display がずれ得る。lane A が揃えたのは operand を括弧で包む
canonical と runtime fallback の適用範囲であり、type-erased composite の
要素区切りや struct 綴りの完全な再現ではない。
