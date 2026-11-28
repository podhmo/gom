# diagnostic details 昇格と近接 snippet 統合の扱い

- 状態: 採用（調査済み・いずれも汎用機構は見送り）
- 日付: 2026-11-28

## 決定

1. `data` を text renderer が汎用に `details` へ昇格する機構は採用しない。`data` は machine-facing の正のままにし、人間へ見せる価値が確定した値だけを、診断を作る phase が整形責任を持って `details` / `labels` / `notes` へ個別に置く
2. 近接した同一ファイル span を text renderer が汎用に 1 つの snippet へ統合する機構も現時点では採用しない。副 span は診断ごとに必要なものだけを厳選し、候補列挙には件数上限と structured data を使う

## 比較した例

`testdata/spec/E0407_record_candidates.gom` の text 出力では、主 span の後に `First` と `Second` の宣言 label が別 snippet として出る。2 つの宣言は隣接行でも、現行は `-->`・`|`・label 行をそれぞれ繰り返す。候補が増えると縦に長いが、wave56 lane A/B では候補数上限（E0407 の label は 5、E0201 の候補は 3）と `data.candidates` による機械可読表現を既に入れている。全量が JSON に残るのは E0407 で、E0201 は JSON/note とも最大 3 件である。

反対に E0301 の `missing_patterns` は machine-facing data として残しつつ、人間向けには既存の `details: patterns not covered` が必要な代表例である。この種の一覧は generic な data ダンプではなく、phase が表示名と順序を選ぶべきである。

## 理由

- JSON consumer は `data` の key と型に依存する。text renderer が全 key を表示すると、機械向けの補助値まで人間向け出力へ漏れ、key 追加が text の互換性問題になる
- `data` の値は文字列・数値・配列・object が混在し、JSON 表記のままでは読みにくい。整形は診断ごとの意味判断なので renderer に押し込めない
- snippet 統合は同一行に複数 caret、複数 label message、multi-line span、隣接判定の閾値を扱う必要がある。特に label message と caret の対応を保つ layout は単なる行 dedup では済まない
- 近接 snippet の重複はあるが、現状の主な発生源は候補列挙であり、既に件数上限がある。表示コストより renderer の複雑化と誤配置リスクのほうが大きい

## 今後の基準

- `details` を増やしてよいのは、人間が次に取るべき行動を決めるのに必要な一覧または表で、phase が人間向けの表示文字列を既に確定できる場合だけ
- `data` だけを増やす変更は text output を変えない。text にも出す場合は診断ごとの fixture で過剰出力を審査する
- snippet 統合は、上限内の表示でも 1 診断あたりの snippet 行数が利用例で常態化し、複数箇所で編集体験を妨げる頻度が確認できたら別途再検討する。その場合は単一行複数 caret の prototype から始める

調査記録: [sketch/2026-11-28-wave56-lane-d-diagnostic-rendering-verify](../sketch/2026-11-28-wave56-lane-d-diagnostic-rendering-verify.md)。
