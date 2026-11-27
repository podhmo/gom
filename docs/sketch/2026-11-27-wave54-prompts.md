# wave54 prompts

- 日付: 2026-11-27
- 状態: 草案
- 入力: [wave53 round](2026-11-27-wave53-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)

wave53 は init failure atomicity、callback result record propagation、診断 UX 次段の選択を処理した。wave54 は決定済みの診断実装を先に処理し、その後で残った診断設計と内部改善の優先度を決める候補を置く。

## lane A. E0401 provenance を if/match 腕へ伝播する（小〜中）

wave53 lane C で採択済みの実装候補（TODO DIAG-2）。

- 注釈からそのまま同じ expected type が供給される if/match 腕で、最終 conformance mismatch へ `expected type declared here` label を付ける
- 腕内で独立に失敗した constraint mismatch へ label を漏らさない negative fixture を先に red にする
- `testdata/spec` の期待 JSON と text output を同期する
- 呼出し引数・record field provenance、details 昇格、snippet 統合は lane に入れない

## lane B. record 未解決に伴う二次 E0401 suppression の調査（小、調査）

wave53 lane A で残った論点。backward inference が必要な `let seed = { ... }` では E0407 + E0401 が残る。二次 mismatch を suppression すべきか、E0407 側の文脈表示を改善すべきか、現状維持かを比較して decision へ絞る。実装は lane に入れない。

## lane C. details 昇格 / snippet 統合の調査（中、調査）

structured data の details 昇格基準と、近接 span を 1 つの text snippet へ統合するかを比較する。E0301 以外の production use 候補を集め、次に実装する 1 面だけ decision へ絞る。fragment example 検証は別 lane 候補とし、実装は入れない。

## lane D. checker 状態分割（中〜大、内部改善）

`typeTables`・deferred revisit queue・taint・lint state などを関心ごとの部分構造体へ分け、`expr.go` を構文種別ごとに分割する。振る舞いは変えない。diff を小さく保つため、まず state grouping だけ、または file split だけに scope を限定するか採否時に決める。

## lane E. benchmark 基盤（小〜中）

`bench/` に代表的な gom/手書き Go のペアを置き、`go test -bench` の比率を記録する。CI 実行や回帰警告は入れない。比較対象は直近の最適化候補に限定するか、広い言語機能 coverage を取るか採否時に決める。

## 見送り候補（少なくとも現時点では lane にしない）

- `gom explain` fragment example 検証 — 有効だが lane C の details/snippet 調査より優先度は低い。件数が多いため単独 lane でも重い
- rt 表示一元化 — 有効な内部改善だが、診断・checker 改善より後でよい
- match narrowing の次段 precision — DMMF で主用途は解消済み。guard/literal/field path は需要が出るまで保留
- stdlib `str` 拡張、Option traverse、panic capture adapter — 需要確認が弱い
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
