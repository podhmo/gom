# wave54 prompts

- 日付: 2026-11-27
- 状態: 採択済み（podhmo interview、2026-11-27）
- 入力: [wave53 round](2026-11-27-wave53-round.md)、[TODO](../../TODO.md)、[open-questions](../open-questions.md)、[implementation-gaps](../implementation-gaps.md)

wave53 は init failure atomicity、callback result record propagation、診断 UX 次段の選択を処理した。wave54 は wave53 で古くなった DMMF recommended form を先に更新し、診断 UX の残論点を調査し、最後に checker の大きい内部整理を行う。

採用 lane は **B → C → F → G**。lane A（E0401 provenance 実装）は podhmo 採択で wave54 から外し、TODO DIAG-2 として次 wave 以降へ送る。lane G は state grouping と file split の両方を行う。

## lane B. DMMF fit example の recommended form 更新（小）

podhmo 採択方針: **実装する**（TODO DMMF-1）。

- wave53 lane A で pipe 経由 `Result.map` lambda 内の無名 record literal が推論できるようになったため、`experiments/dmmf-fit` の recommended form と wave52 lane D レポートの古い案内を更新する
- 実際に無名 literal へ書き換えて compile/test を確認し、型名前置 literal を残すべき位置と外せる位置を分ける
- 言語実装や spec は変えない。experiment / 関連レポートの更新に留める

## lane C. record 未解決に伴う二次 E0401 suppression の調査（小、調査）

podhmo 採択方針: **調査し、decision へ 1 方針だけ記録する。実装は lane に入れない**。

- backward inference が必要な `let seed = { ... }` で残る E0407 + E0401 を対象に、二次 mismatch suppression、E0407 側の文脈表示改善、現状維持を比較する
- record propagation 本体や backward inference 実装は対象外
- 採用した方針と red-first fixture 方針だけ decision へ記録し、残りは open question へ戻す

## lane F. 継続行らしき字下げ行の誤パース診断の調査（中、調査）

podhmo 採択方針: **調査し、decision へ 1 方針だけ記録する。実装は lane に入れない**。

- `List.fold` の適用を複数行へ書いたときの E0407/E0402/E0401 連鎖を対象に、専用診断への畳込み、既存診断への括弧/pipeline hint 追加、現状維持を比較する
- NL-2 の文法変更、字下げだけで括る自動 fix、独立式の意味を変える rewrite は対象外
- false positive を避ける判定条件と、既存 error recovery との境界を decision へ記録する

## lane G. checker の state grouping + file split（中〜大、内部改善）

podhmo 採択方針: **state grouping と file split の両方を実装する。振る舞いは変えない**。

- `checker` の平らな field を `typeTables`、inference/meta 状態、pipe/record propagation 状態、taint/lint/診断状態など責務ごとの部分構造体へ分ける
- `expr.go` を pipe、control flow、literal、Go interop、select/concurrency など構文種別・責務ごとのファイルへ分割する
- 移動と意味変更を混ぜない。公開診断、spec fixture、生成コード、lint 結果は変えない
- review しやすいよう state grouping と file split を別 commit に分けるが、1 lane = 1 PR は維持する
- 完了条件は通常どおり `gofmt -l .` 空、`go vet ./...`、`go test ./...`。必要なら既存 test だけで振る舞い不変を確認する

## 採否インタビュー結果

- B/C/F/G を採用。E0401 provenance 実装、details/snippet 調査、E0201 typo/import 補完、fragment example 検証、benchmark 基盤は見送り
- lane 順は B→C→F→G
- lane A（DIAG-2 implementation）は wave54 では実施しない。TODO DIAG-2 として残す
- lane G は state grouping と file split の両方を実装する

## 見送り候補（少なくとも現時点では lane にしない）

- lane A: E0401 provenance を if/match 腕へ伝播する実装 — 決定済みだが、podhmo 採択で wave54 からは外し TODO DIAG-2 として残す
- details 昇格 / snippet 統合の調査 — 有効だが、wave54 は二次 E0401 と継続行診断を優先する
- E0201 typo/import 補完 — 利用者価値はあるが、wave54 の診断調査は C/F に絞る
- `gom explain` fragment example 検証 — 有効だが件数が多く、利用者向け診断改善より優先度は低い
- benchmark 基盤 — podhmo 判断で現時点の価値は低い
- rt 表示一元化 — 有効な内部改善だが、checker 整理より後でよい
- match narrowing の次段 precision — DMMF で主用途は解消済み。guard/literal/field path は需要が出るまで保留
- stdlib `str` 拡張、Option traverse、panic capture adapter — 需要確認が弱い
- `gom install`、Go 定数セット、cgo/unsafe/reflect、セルフホスト — future work 維持
