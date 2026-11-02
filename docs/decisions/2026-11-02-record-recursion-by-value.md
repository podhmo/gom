# レコード型は自分自身を値として内包できない（E0413）

- 状態: 採用
- 日付: 2026-11-02

## 決定

`type Point = {x: Point}` のように、レコードが自分自身を**値として**内包する
宣言は型検査でエラー（E0413）にする。相互再帰（`A` のフィールドが `B`、
`B` のフィールドが `A`）も同じくエラー。

再帰が許されるのは間接参照を介するときだけ:

- 関数型のフィールド（`{f: () -> Point}`）
- sum type のバリアントを通る再帰（`type Node = {next: Option Node}` —
  `Option` はインターフェース表現）
- ポインタ相当の表現を持つフィールド: `List` / `Ref` / `Ptr` / `Array` /
  `Chan` / `Go.Slice` / `Go.Map`

逆に、タプル・`Result`・`Task` のペイロードはインライン配置なので、
それらを通る再帰も検出してエラーにする（`{r: Result Point e}` は不可）。

## 背景

レコードは Go の struct にインラインで下りる。`type Point = {x: Point}`
をそのまま出力すると `type Point struct { X Point }` となり、Go 側が
「invalid recursive type」でコンパイルできない。つまり言語側で許すと
検査済みプログラムから壊れた Go が出る（fuzz の既知バグとして記録されて
いた）。sum type の再帰が合法なのは、バリアントが sealed interface に
下りて間接参照になるからで、spec 3.2 の「再帰型は許可する」はその表現を
前提にしていた。

## 検証

`internal/check/check_test.go` の `TestRecordByValueCycle` /
`TestRecordIndirectCycleOK` で、直接・相互・ジェネリック展開・
Result/Task/タプル経由の再帰が E0413 になること、関数・sum type・
ポインタ表現経由の再帰が受理されることを確認した。ユーザー向けの
振る舞いは `testdata/spec/E0413_*.gom` が固定する。
