# wave55 prompts

- 作成日: 2026-11-27
- 状態: 採択済み（podhmo interview 完了）
- 採用 lane と順: **B → A → D → C → E**

## lane B. 継続行 hint 実装（小〜中）

podhmo 採択方針: **実装する**。

- [continuation-line-diagnostics](../decisions/2026-11-27-continuation-line-diagnostics.md) を実装する
- 関数型の mid-block statement で、同一 block の次 statement が後続行かつ深い字下げのときだけ、E0402 へ `if these lines were meant as one application, use |> or wrap the whole application in parentheses` の note を付ける
- 関数本体 block と match/if arm block を対象にし、ファイルスコープ E0101 と block 末尾式だけの E0401 には広げない
- 診断 suppression、NL-2 変更、字下げだけで括る自動 fix は入れない
- decision の red-first fixture 方針どおり、note 期待を先に red にしてから実装する

## lane A. record quiet recovery 実装（中）

podhmo 採択方針: **実装する**。

- [record-ambiguity-secondary-mismatch](../decisions/2026-11-27-record-ambiguity-secondary-mismatch.md) を実装する
- field-set resolution 失敗時は `wantT` の meta を `Unit` へ bind せず quiet にし、`wantT` がないときだけ quiet meta を recovery type にする
- backward inference を valid program へ導入しない。ambiguous/no-match の root diagnostic をすでに出した recovery だけ静かにする
- 独立した `expected Acc, got Other` などの矛盾は残す
- decision の red-first fixture 方針どおり、既存 `E0407_record_context_no_backward`、独立 mismatch、no matching record の fixture を先に red にしてから実装する

## lane D. E0407 ambiguous record literal の候補表示調査（小・調査のみ）

podhmo 採択方針: **調査し、decision へ 1 方針だけ記録する。実装は lane に入れない**。

- ambiguous record literal の E0407 で、候補 record 名、宣言位置 label、structured data のどれを出すべきか比較する
- lane A の quiet recovery 実装結果を前提に、二次 E0401 suppression と混ぜずに評価する
- 候補数が大きい場合の上限、text/JSON の表現、`gom explain` の扱いを決める
- backward inference 本体、structural/anonymous record type は対象外

## lane C. DIAG-2: E0401 provenance を if/match 腕へ伝播（中）

podhmo 採択方針: **実装する**。

- TODO DIAG-2 を実装し、注釈からそのまま同じ expected type が供給される if/match 腕について、最終 conformance mismatch に `expected type declared here` label を付ける
- 腕内の独立 mismatch へ漏らさない negative fixture、text/JSON output、spec/decision 更新を含める
- 呼出し引数・record field provenance は対象外

## lane E. E0201 typo/import 補完の最小面調査（中・調査のみ）

podhmo 採択方針: **調査し、decision へ 1 方針だけ記録する。実装は lane に入れない**。

- undefined name の typo 候補、module member typo、未 import Go package に対する `import go` fix の 3 案を比較する
- 編集距離・候補数上限・fix applicability・誤修正リスクを決める
- 少なくとも 1 案を却下し、次に実装する最小面を 1 つだけ decision へ記録する
- 大きな import 管理や LSP 連携は対象外

## 採否インタビュー結果

- A/B/C/D/E をすべて採用
- lane 順は B→A→D→C→E
