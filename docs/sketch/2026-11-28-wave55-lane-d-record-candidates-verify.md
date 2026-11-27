# wave55 lane D: E0407 候補表示の調査

- 日付: 2026-11-28
- lane: [wave55 prompts](2026-11-27-wave55-prompts.md) lane D（調査のみ）
- decision: [record-ambiguity-candidates](../decisions/2026-11-28-record-ambiguity-candidates.md)

## 調査内容

現状の `recordByFields` は local `c.records` と import 済み `foreignRecs` を候補にし、ambiguous なら件数だけを E0407 に出す。`typeInfo` は declaration span `sp`、type constructor `con`、record field 情報を持つため、候補名・宣言位置・structured data はいずれも emitter 側へ出せる。foreign record は `TypeDecl` から写した span と package qualifier を持つ。

text renderer は structured data を表示しない。したがって data だけでは CLI 利用者が候補を読めない。一方、候補名を主 message へ入れると件数や qualified name で 1 行 message が長くなり、宣言位置も分からない。declaration label なら text/JSON/LSP が既に持つ副 span 機構へ乗り、候補 record の宣言へ直接誘導できる。

## 結論

候補表示は **declaration label + structured data** とする。label は text 向けの主な案内、data は機械可読の正とする。主 message は変えず、表示は 5 件まで、超過分は件数だけ note にする。実装は別 lane とし、この lane では compiler を変更しない。

lane A の quiet recovery 実装済み状態を前提にした。quiet recovery は ambiguous record 自体の root diagnostic を残すため、候補表示改善と競合しない。

## 実装時の確認点

1. 同一 file の 2 候補で declaration label が出る
2. 複数 file の local package で候補 declaration span が解決できる
3. import した `.gomi` の record で qualified name と span がどう出るか
4. 5 件超の候補で truncate note が出る
5. JSON output に deterministic な `data.candidates` が出る

## 検証

文書のみ。`go vet ./...` / `go test ./...`。
