# labeled args into returned functions: end-to-end verification

- 日付: 2026-11-06
- 状態: verified — eval と compiled が全ケースで一致

## スコープと結論

[decision](../decisions/2026-11-06-label-args-into-returned-functions.md)
の検証。`f ~b: e` が callee の宣言 arity を越えて返される関数の
labeled パラメータを指すとき、従来はラベルが位置引数へと潰れて
`f(e)` の意味に落ちていた (`"100"` と誤答 / `E9004`) 。平坦化した
パラメータ位置でラベルを保持する仕組みを入れ、直接呼び出し・
部分適用・再分類の全経路で eval/compiled を一致させた。

## 変更点

| 箇所 | 内容 |
| --- | --- |
| `internal/check/call.go` | `CallInfo.Arity` (宣言 arity) に `FlatArity` を追加。`Flat()` は FlatArity 未設定時に Arity へフォールバック。`Validate` はマッピングと部分適用を平坦化ドメインで検査 |
| `internal/check/apply.go` | `checkGomApply` が関数チェーン全体を平坦化し、ラベルを平坦位置で照合。`classMappedCall` が宣言 prefix の飽和状況で `CallDirect`/`CallPartial` を分類 |
| `internal/check/reclassify.go` | 返り値関数レベルまで届く平坦インデックスのマッピングを保持 |
| `internal/eval/call.go` | `applyArgs` が深い位置のラベル引数を保留し、所有レベルに到達したら適用 — 早いパラメータが開いていれば eta 展開した closure を作る |
| `internal/lower/expr.go` | `declOrderArgs` が平坦位置で並べ替え。直接呼び出しは宣言 prefix を飽和させてから返り値関数へ深い引数を適用。部分呼び出しは未充填の平坦位置すべてを受ける Closure を生成。`lowestUnfilled` も平坦ドメインで走査 |

## 検証結果

- **`same ~b: 3 ~a: 1`**: 宣言 `fn same (~a: Int) -> ~b: Int -> Int` — `same(1)` の返り値関数に `3` が届き `300`。eval/compiled 一致 (compare `deep_label_args`)。
- **先行パラメータを開けたままの深いラベル**: `same ~b: 3` は `fn a -> same(a)(3)` に相当 — evaluator は eta 展開、compiler は Closure を生成、両側 `300`/`400`/`300`。
- **let 束縛・部分適用・並べ替え**: `let h = same ~b: 3; h ~a: 1` → `300`、`g ~b: 4 ~a: 1` → `400`、`third ~c: 7 ~a: 1 ~b: 2` → `721` (compare `deep_label_args`)。
- **再分類**: `CallUnknown` で検査された呼び出しが `KindFunc` に再分類されるときも平坦インデックスの ArgToParam を保持 — `f 0 ~b: 20 ~a: 1` が `g(1, 20)` の `-19` を返す (compare `deep_label_reclassify`)。
- **生成コード**: `labeled_args_deep.golden.go` で `tmp5 := same(1)` に続く `tmp6 := tmp5(3)` の二段適用を確認。
- **spec fixture**: `ok_labeled_args_cross_level.gom` が無診断で通る。

## 補足

`ArgToParam` が nil のときの意味 (位置恒等) は従来のまま —
非 nil のマッピングだけが平坦ドメインを参照し、従来の呼び出しの
構造は変わらない。
