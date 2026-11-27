# wave53 lane C 診断 UX 次段調査

- 日付: 2026-11-27
- lane: wave53 lane C（調査）
- prompts: [wave53 prompts](2026-11-27-wave53-prompts.md)

## 目的

4 論点を比較し、次に実装する 1 面だけ decision へ絞る。実装は lane に入れない。

## 比較

| 論点 | 利用者価値 | 実装見通し | 判断 |
| --- | --- | --- | --- |
| E0401 provenance 伝播 | 高い。分岐や推論先へ mismatch 位置が移っても注釈位置を辿れる | `checkExprAt` / `expectedFrom` / `labels` を再利用できる。non-leak 規則の fixture が要る | 採用。最初は if/match 腕へ限定 |
| details 昇格基準 | 中。E0301 では有効だが、他 code の production use が少ない | 診断ごとの原文・整形・過剰出力の基準が要る。観測不足で抽象化しにくい | 見送り |
| 近接 span の snippet 統合 | 中。重複表示は読みにくいが致命的ではない | text renderer の layout 設計が要り、既存出力の差分が広い | 見送り |
| fragment example 検証 | 低〜中。coverage 衛生には有効 | 59 件規模の例整備か formalize が要り、利用者向け改善ではない | 見送り |

## lane A/B との関係

wave53 lane B は CLI の failure atomicity で、診断 UX の比較には直接影響しない。wave53 lane A は expected type propagation を callback result・pipe boundary まで広げた。これにより型推論は改善した一方、mismatch が残る場合の発生位置は注釈から離れやすくなる。したがって二次診断を潰すより先に、expected type の由来を追える provenance を厚くする方が診断 UX として一貫する。

## 現状確認

- `docs/decisions/2026-11-27-diagnostic-labels-details.md` は let/return 注釈の直接 mismatch までを実装済みとし、if/match 腕・呼出し引数への伝播を scope 外としている
- `internal/check/check_labels_test.go` には nested mismatch へ label を漏らさない negative test がある。provenance を広げる実装でもこの規則は維持する必要がある
- text renderer は primary span と secondary label を独立 snippet として描く。snippet 統合は provenance 実装なしでも可能だが、依存しない formatter 改善になる
- production の `AddDetail` use は E0301 が中心で、details 昇格基準を一般化する観測幅はまだ狭い
- `gom explain` examples は fragment として多数あり、coverage meta-test との関係は依然未決だが、今回の実装候補にするほど利用者価値が直結しない

## 結果

採用: **E0401 expected type provenance の if/match 腕への伝播**。

決定と red-first fixture 方針は [decision](../decisions/2026-11-27-diagnostic-ux-next-step.md) に記録した。呼出し引数・record field provenance、details 昇格、snippet 統合、fragment example 検証は open question へ残す。

## 検証コマンド

文書・台帳のみの変更。最終検証は `gofmt -l .`（出力なし）、`go vet ./...`、`go test ./...`。
