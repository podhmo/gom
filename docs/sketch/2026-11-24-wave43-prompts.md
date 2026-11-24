# wave43 prompts 草案

- 日付: 2026-11-24
- 前提: wave42 stack（#201→#202→#204→#205→#207）マージ後の
  main から開始。候補は台帳の open 項目から — **採否インタビューは
  merge 前に実施する**（wave42 の運用修正）

## 候補

### A. 9 要素超 spelled tuple の codegen gap（規模: 小〜中）

`rt` の tuple は `Tuple8` までしかないのに check が型位置の
tuple arity を縛らない — `(Int ×9)` を書くと生成 Go が
`undefined: rt.Tuple9` で `go build` に落ちる（wave41 lane B で
確認・fuzz seed#275 も同経路）。check で上限を診断するか rt を
拡張するかは未決 — 調査→decision か、決まっていれば実装まで。
参照: [implementation-gaps](../implementation-gaps.md)（
`rt.Tuple64` 相当を確認の項）

### B. String/Char/Bool リテラル→宣言型の代入（規模: 中）

wave42 lane C の残件 — 数値リテラルは `satisfies` の
`definedUnderlying` peel で受理したが、文字列系リテラルは
concrete 型なので等価 unify が直接落ちる。`type Name = {n:
String}` に `"x"` を代入するには等価 unify 側の peel が要る。
Go の untyped constant 規則との整合 — lane C の decision で
別規則として切り分けた経緯あり（実装するか、数値限定の
非対称を仕様として残すかを先に決める）。

### C. production `display`/文字列補間と evaluator の composite
###   表示のずれ（規模: 中）

evaluator は ADT/record/list/tuple を comparison format で描く
が、生成 Go の `display`・補間は `fmt.Sprint` に落ちる。
`lower` の `gomShowN`（assert_eq の描画、design.md §4.8）を
production 側へ転用する方向が ledger に記録済み。

### D. リテラル shift 除数の E0414 幅依存（規模: 小）

`7 / (1 >>> 1)` のような shift ノードは幅が未確定だと fold を
見送る — `255 <<< 60` のように幅で結果が分岐するケースは
E0414（定数ゼロ除算）を検出できず実行時依存。誤検出なしの
現状のままか、確定できる幅で fold するか。

### E. `gom fix` の `.gomi` 適合検査（規模: 小〜中・open-question）

適合診断を fix 候補プールに加えるか — E0601/E0603/E0604 に
safe fix がなく観測差ゼロで先送り済み（2026-11-05 の検討）。
価値が具体化していない限り保留推奨。

## 却下・対象外

- D-3 README / D-4 tutorial go.mod 整理: ユーザー指定で終盤作業
