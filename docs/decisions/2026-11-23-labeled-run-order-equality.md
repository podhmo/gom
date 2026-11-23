# ラベル付き引数の並びは型同値で宣言順に比較する

- 状態: adopted
- 日付: 2026-11-23

Supersedes: [2026-11-02-ast-representation](2026-11-02-ast-representation.md)
の「`Equal` implements spec 2.1: consecutive labeled parameter runs compare
up to reordering」の記述。

wave36 lane F のレビュー指摘から。spec 2.1 は「連続するラベル付き引数の
並びを名前対応かつ宣言順で比較する」と定める — `~a: Int -> ~b: Int -> R`
と `~b: Int -> ~a: Int -> R` は別の型。旧実装の `types.Equal` はラベルの
連続 run を名前でソートして比較していたため、型同値としては同一視され
るのに生成 Go の位置呼び出しでは引数が入れ替わる経路が残っていた
（`unifyFunc` の introduction 修正は代入経路しか塞いでいなかった）。

## 決定

- `types.Equal` / `EqualLent` はラベル付きパラメータを宣言順で比較する
- `.gomi` の signature hash と `iface` の alphaEqual も run の順序を保持し、
  `.gomi` 側と実装側で run の並びが違う宣言はインターフェース不一致とする
  — `.gomi` のない古い契約でも spec の型同値と同じ規則に揃える

呼び出し側の `~x:` 記述順は従来どおり順不同（check がラベルから位置へ
解決して位置引数として emit する）。
