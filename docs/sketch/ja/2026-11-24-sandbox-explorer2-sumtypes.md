> 転記元: [PR #196](https://github.com/podhmo/gom/pull/196) の `sandbox/sumtypes.md`（sandbox 探索 run 2）

# moonbit-sum-types の指針を sandbox に照らした監査メモ — 2026-11-24

参照: https://blog.podhmo.workers.dev/slops/moonbit-sum-types
（「Go の形を脱いで MoonBit の sum types に寄せた」。基準: 排他状態を Bool/sentinel で表さない、
状態固有フィールドは variant の payload、「無い」は Option、閉じた語彙は enum、
性質は持ち主の型へ、match は網羅、Go との一致は振る舞いで取る。）

全コードは記事を読む**前**に書いたもの。そのまま照らして「変えたもの」「見送ったもの」「言語側の穴」を分ける。

## 変えた / 当てはまったもの

- **`shop/domain` は指針どおりの形だった**（監査で合格側の確認）。
  `ProductCode`/`OrderQuantity`/`PlaceOrderError` は enum、`Unvalidated*` 系は外部入力のまま、
  `ValidatedOrder`/`PricedOrder` は「その段階だけ意味を持つ」フィールドを持つ別型、
  `to_product_code` は閉じた語彙 `"W-"`/`"G-"` を境界で enum に変換、match は全網羅。
  `_ ->` が残るのは `to_product_code`（任意の分割結果に対する open な入力）だけで、指針の例外として妥当。
- **`rmv` の `exec: Bool` → 見送り（真の2値）**。`DryRun|Execute` にしても命名が明瞭になるだけで、
  守れる不変条件は増えない（subtree と同じ線引き）。ただし `rename_one` の戻り値を Bool 化は
  exit code 対応のために入れた — Bool だが「真の2値」のまま。
- **`pscan probe -> Bool` → 見送り**。open/closed は payload を持たない 2 値。
- **csvcut/pscan の `Result _ String`（引数エラー）→ 見送り**。呼び出し側は print するだけで
  文字列を再解釈しない。「`""`/sentinel が再解釈されるか」が判断点なのでメッセージのまま。
- **`jsonl2csv` の `None -> ""`（欠損セル）→ 正しい形**。Option のまま持って出力境界で `""` に
  変換しているので指針どおり（ItemList の例と同じく「境界で serialize」）。
- **`keys_of` の `seen: Go.Map String Bool` → 見送り**。Go 側に `Map K Unit` がなく、
  値を読まない set 用途。`true` は sentinel ではなくデッドデータ。

## 言語側の穴（指針を適用しようとして gom では書けない）

- ~~`.gomi` は record のコンストラクタを隠せない~~ → **訂正: 隠せる**。`.gomi` で
  `type ValidatedOrder` と bare 宣言（`= { ... }` を付けない abstract 宣言）にすると、
  import 側からの record literal 構築が E0401 で拒否される（実検証済み）。
  `shop/domain/domain.gomi` を修正して `ValidatedOrder`/`ValidatedOrderLine` を abstract 化し、
  「`place_order` を通さないと `ValidatedOrder` を作れない」を型で強制できた。
  `PricedOrder` は receipt 描画のために main 側がフィールドを読む必要があるため concrete のまま
  （出力データなので直接構築されても害が薄い、という線引き）。
- **1 ファイル内で同形 record を重ねると E0407 の注釈が要る**（`UnvalidatedOrder` → `ValidatedOrder`
  のような「同じフィールドを持つ段階型」パターンで自然に発生）。機能ではなく DX の摩擦。

## Devin Review が拾った形（指針と独立だが関連する指摘）

- `pscan` の `List.range lo hi` が上端を含まない（`1-1024` で 1024 をスキャンしない）— 修正済み
- `rmv` が既存ターゲットを `os.Rename` で上書き・失敗時も exit 0 — 修正済み
- `jsonl2csv` が scanner エラーで部分結果を返す / 大整数が Float に丸まる / キー順が非決定的 — 修正済み
- `csvcut` が欠損列を落とす（`pick` が filter していた）— `Option.get_or_else ""` に修正
- `until` が UTC 基準で日付を比較 — `ParseInLocation time.Local` に修正
- `passgen` が `-n abc` や非正数を黙って受理 — usage エラーに修正

## 結論

今回のツール群は「閉じた語彙・排他状態・欠落」を扱う場面で自然に enum/Option を使えており、
指針を後から適用しても大きな変更は出なかった。見送り線引き（真の2値、境界変換、set 代用の
`Map K Bool`）も記事の基準表にそのまま置ける。監査当初「コンストラクタ非公開が書けない」と
判断した点は Devin Review の指摘で覆り、`.gomi` の abstract 宣言で実現できた — DMMF の
「検証済み型を経由なしに作らせない」は gom でも型レベルで守れる。
