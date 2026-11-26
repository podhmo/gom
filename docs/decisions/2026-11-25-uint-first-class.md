# `Uint` / `Uint64` を第一級整数型にする

- 状態: 採用
- 日付: 2026-11-25

## 文脈

Go の `uint` / `uint64` はこれまで公開面に入れられず、`math/bits` や
`text/tabwriter` のような unsigned API は handwritten padding か別の回避策を
必要とした。wave47 lane D では `Byte` を変えず、`uint16` / `uint32` /
`uintptr` を開かない範囲で、幅 64 の unsigned integer を第一級化する。

## 決定

- builtin `Uint` を追加する。Go 表現は `uint64` で、literal、四則・ビット
  演算、比較、構造等価、表示、`Go.conv` を `Int` と同じ格で扱う。演算は
  uint64 の wrap-around で、リテラルは静的位置の uint64 範囲を要求する
- builtin `Uint64` は `Int` に対する `Int64` と同じ境界綴りである。通常の
  値表現は `Uint` と同じ `uint64` だが、`@method` の raw signature では
  `uint64` を正確に要求する位置に使う。raw `uint` の位置には `Uint` を使う
- Go import では `uint` / `uint64` を Gom view `Uint` に読む。raw view では
  `uint` を compiler 内部の `Go.Uint` として保持し、既存の `Int` / `Go.Int`
  と同じ境界変換を行う。`uint16` / `uint32` / `uintptr` は引き続き公開しない
- evaluator の `VInt` は width-64 bit pattern に加えて unsigned interpretation
  を保持し、type-erased display でも compiled reflection と同じ符号なし表示に
  揃える

## 却下した案

- Go の `uint` を machine width のまま Gom `Uint` の表現にする → 32/64 ビットで
  言語意味が変わる。`Int` が int64 表現で raw `int` を境界変換する既存モデルに
  合わせる
- `Byte` を unsigned integer 演算へ一般化する → lane D の採択範囲外。`Byte` は
  byte I/O 用の既存仕様を維持する
- `uint16` / `uint32` / `uintptr` も同時に開く → 幅の組合せと boundary 変換が
  増える。需要が確認されている `uint` / `uint64` に限定する

## 検証

prototype 記録: [wave47 lane D uint first-class verify](../sketch/2026-11-25-wave47-lane-d-uint-verify.md)。
