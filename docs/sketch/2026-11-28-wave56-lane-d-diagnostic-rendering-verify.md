# wave56 lane D: details 昇格と snippet 統合の調査

- 日付: 2026-11-28
- lane: D（調査のみ）
- 対応 decision: [../decisions/2026-11-28-diagnostic-details-and-snippets.md](../decisions/2026-11-28-diagnostic-details-and-snippets.md)

## 調査範囲

`internal/diag/emit_text.go`・`diag.Diagnostic` の `Data`/`Details`/`Labels`、および既存 fixture の text 出力を確認した。repo の実装や fixture は変更していない。

## 現行の出力構造

- text renderer は `data` の key を一切解釈せず、header・primary snippet・labels・details・notes・fix titles だけを出す
- `details` を実際に使っている compiler diagnostic は E0301 の `patterns not covered` が中心で、`missing_patterns` data の人間向け表示を phase 側が作っている
- label は 1 件ごとに独立した snippet として描画される。同一ファイルで近接していても統合や行 dedup はない

## 具体例

`go run ./cmd/gom check -format text testdata/spec/E0407_record_candidates.gom` では、主 span に続いて次の 2 つの宣言 label が別 snippet として出る。

- line 3: `candidate record First declared here`
- line 4: `candidate record Second declared here`

宣言は隣接しているが、`-->`、`|`、label 行は候補ごとに繰り返される。候補 5 件まで許す E0407 では縦方向の出力が大きくなりうる。一方で各 label は別の宣言行を指しており、単純な同一行 dedup だけでは解決しない。統合するには複数 caret と label message の対応、multi-line span、統合する距離の閾値が要る。

E0301 の spec 例では、JSON の `data.missing_patterns` と人間向け `details: patterns not covered` が併存する。これは generic な data ダンプではなく、phase が欠落パターンの文字列化と順序を制御しているため読める。`data` に残すことで JSON consumer は文字列解析をせず済む。

## 評価

| 論点 | メリット | リスク | 判断 |
| --- | --- | --- | --- |
| `data` → `details` の汎用昇格 | 新しい診断で人間向け情報を出し忘れにくい | machine-facing key まで text に漏れる、JSON 表記が読みにくい、key 追加で text が変わる | 見送り |
| 診断ごとの手動 `details` | 表示文字列・順序・省略を phase が制御できる | 昇格の判断が個別実装になる | 維持 |
| 近接 snippet 統合 | E0407 の連続宣言候補で縦幅を減らせる | 複数 caret/label の layout が複雑、multi-line と隣接閾値の semantics が要る | 現時点では見送り |
| 候補上限 + structured data | text を短く保ちつつ JSON consumer は全候補を見られる | text で末尾候補の宣言位置は出ない | 維持 |

## 結論

- generic な `data` 昇格は採用しない
- 近接 snippet の汎用統合も現時点では採用しない
- `details` は診断ごとに人間向けの価値が確定したものだけを手動で足す
- 実装タスクは発生させない。再検討条件は「候補上限を超える頻出ノイズが fixture/利用例で確認できること」

## 検証コマンド

```sh
go run ./cmd/gom check -format text testdata/spec/E0407_record_candidates.gom
```

調査のみのため `go test` による回帰確認は不要。実装差分はない。
