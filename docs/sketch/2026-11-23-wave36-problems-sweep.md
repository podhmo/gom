# wave36 lane H — problems 残件の棚卸し

prompts の対象4件を現状コードに照らして再評価。

## 結果

| ファイル | 判定 | 理由 |
| --- | --- | --- |
| `2026-11-02-wrong-kind-diagnostics` | **解消** | 問題文の条件「診断出力が実ファイルで運用されてから決める」は LSP 導入後に満たされ、E0201 のコード表エントリが wrong-kind 用途を明示的に記述する形で「leave it」が文書化済み。`data` の `namespace`/`resolved_kind` で consumer は形状を区別できる |
| `2026-11-04-interp-composite-display` | **未解決・lane 候補** | production `display` は composite を依然 `fmt.Sprint` に落とす（evaluator との表示乖離）。`gomShowN` 機構（design.md §4.8）を production 側へ転用する方針が既に検討材料として揃っている — 次 wave の prompts 候補 |
| `2026-11-05-gom-package-imports` | **解消** | 残件だった 4 (module scope) は実装済みだった: `gomimport` lookup が importing file の module context で `go list` を走らせ、dependency・replace target・workspace sibling の `.gomi` を解決する（`extDirs`/`extMods`）。全 6 項目が閉じた |
| `2026-11-05-slow-tests` | **対応中のまま** | 継続的な改善プログラムで単一 lane の対象ではない。2026-11-06 計測（フル ~31s・`-short` 4.0s）が最新。残構成要素は TestEvalCompiledDiff・直列中規模群・NativeBinaryCache 検証 |

## lane 候補（次 wave の prompts へ）

- **composite display**: `lower` の `gomShowN` 機構（テスト側、assert_eq の operand 描画）を production `display`/`str_concat` 経路へ転用し、ADT/record/list/tuple の `${v}` 補間を evaluator の comparison format に揃える。compare suite の `answer` 回避制限が解消できる。規模: 中

## 触らなかったもの

- `docs/problems/` の他エントリ（未評価の多数）は今回の prompts 対象外 — 個別の wave で順次。
