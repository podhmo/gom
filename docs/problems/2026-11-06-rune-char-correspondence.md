# Go `rune` と `Char` の対応

## 背景

spec §8.3 の対応表は以前「Go の `rune` → `Char`」と読める記述だったが、実装は

- 名前のある / 基本型としての Go `int32`（`rune` はその別名）→ `Int32`
- 型なし rune リテラル（`'a'`）→ `Char`

という分離になっている（`internal/goimport/mapping.go`）。Go では `rune` が `int32` のエイリアスなので、型レベルでは両者を区別できない — `rune` を `Char` に写像すれば `int32` まで `Char` になってしまう。spec を実装に合わせて直した（2026-11-06 の wording batch）。

## 残る懸念

- **摩擦**: Go の `rune` 引数 API（`unicode.IsUpper`、`strings.Map` など）には Gom 側から `Int32` を渡す必要があり、手元の `Char` 値は `Go.conv c : Int32` を通す。逆に rune を返す API の結果を `Char` として使うにも `Go.conv` が要る。境界変換なしに渡せる見た目の型が違う、という違和感が残る。
- **`Char` の境界変換**: `Char` の Go 表現も `rune`（＝ `int32`）なので、`Int32` と `Char` の間は機械的に相互変換可能。`Go.conv` の明示を残すか、Char ↔ Int32 の暗黙の橋渡しを許すかは未決定。v1 は明示のまま。
- **`byte`（`uint8`）**: 同じ構図のエイリアス問題が `byte` にもあり、こちらは未対応型として公開面から外れている。`rune`/`byte` のようなエイリアスを「別の Gom 型」に写像する方針自体が、エイリアスである以上一貫して成立しない。

## 現状の扱い

spec §8.3 に「`rune` は `Int32`、`Char` はリテラル / 型なし rune 定数 / `Go.conv` で得る」と明記した。変換の摩擦を解消するか（暗黙変換や専用の組み込みを足すか）は、ユースケースが集まってから再検討する。
