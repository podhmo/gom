# wave54 lane C: record 未解決に伴う二次 E0401 調査

- 日付: 2026-11-27
- lane: [wave54 prompts](2026-11-27-wave54-prompts.md) lane C（調査）
- decision: [record-ambiguity-secondary-mismatch](../decisions/2026-11-27-record-ambiguity-secondary-mismatch.md)

## 目的

backward inference が必要な `let seed = { ... }` で残る E0407 + E0401 について、二次 mismatch suppression、E0407 側の文脈表示改善、現状維持を比較し、実装候補を 1 方針だけ decision へ記録する。lane では実装しない。

## 現状確認

`testdata/spec/E0407_record_context_no_backward.gom` では次の 2 件が出る。

```text
E0407 the record literal is ambiguous: 2 record types own these fields; annotate the type
E0401 type mismatch: expected Acc, got Unit
```

2 件目の `Unit` はユーザーが書いた型ではない。`checkRecordLit` が field-set resolution 失敗後に expected meta を `Unit` へ bind する recovery artifact で、それが後続使用へ流れて mismatch になる。checker の設計は root cause ごとに診断を絞り、recovery artifact には quiet meta を使う方針を既に持っている。

## 比較

| 方針 | 効果 | 実装見通し / リスク | 判断 |
| --- | --- | --- | --- |
| 二次 mismatch suppression | root cause の E0407 だけを残し、回復 artifact の `got Unit` を消せる | quiet meta という既存の仕組みに乗る。prototype では対象 fixture が 2 件から 1 件になり、`go test ./internal/check ./internal/build` では期待件数の diff だけが出た。backward inference を valid program へ導入するわけではない | 採用 |
| E0407 側の文脈表示改善 | 候補 record 名を出せば注釈先を選びやすい | `recordByFields` は候補数だけでなく候補型も集められる。ただし `got Unit` の二次診断は残る | 今回は見送り。独立した open question へ残す |
| 現状維持 | 二次 E0401 がたまたま `expected Acc` を教える場合がある | `got Unit` は compiler artifact で、修正位置を後続使用へずらす。one diagnostic per root cause の方針ともずれる | 見送り |

## prototype 観測

lane の実装には入れず、throwaway prototype で recovery 方針だけ確認した。

1. field-set resolution 失敗時に、`wantT` が meta なら `Unit` へ bind せずその meta 自体を `markQuiet` する。`wantT` が nil のときだけ新しい quiet meta を recovery type に使う
2. `E0407_record_context_no_backward` を実行すると diagnostics は E0407 1 件だけになった
3. `go test ./internal/check ./internal/build -count=1` では、既存 fixture が expected 2 / actual 1 で fail するだけで、他の fixture failure は出なかった
4. `seed` を `Other` へ注釈した変数へ流したあと `Acc` として使う probe では、E0407 に加えて `expected Acc, got Other` が残った。quiet recovery が独立した型矛盾まで隠すわけではない
5. `seed + 1` のような未解決 recovery 値への downstream constraint は root E0407 のみになった。注釈後に残る制約エラーは通常どおり出るが、注釈前の段階では root diagnostic を優先する挙動になる

prototype は破棄し、lane ブランチに implementation は入れていない。

## 結果

採用: **record resolution 失敗時の recovery type を quiet meta へ寄せ、二次 mismatch を抑制する**。

決定と red-first fixture 方針は [decision](../decisions/2026-11-27-record-ambiguity-secondary-mismatch.md) に記録した。E0407 の候補 record 表示は open question へ残す。backward inference 本体の実装は対象外のままである。

## 検証コマンド

文書・台帳のみの変更。最終検証は `gofmt -l .`（出力なし）、`go vet ./...`、`go test ./...`。
