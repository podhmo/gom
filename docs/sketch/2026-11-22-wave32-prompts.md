# wave32 prompts 草案

- 日付: 2026-11-22
- 状態: 草案 — wave32 開始前のインタビューで採否を確定する（docs/ja/wave-process.md 規約）

## テーマ案

wave31 で implementation gaps の棚卸しは完了（未解消 0）。次は繰り越した採択済み設計の実装を主軸に。

## lane 候補

### A. `--strict` 実装（優先復帰候補）

- 出典: docs/decisions/2026-11-22-strict-mode.md（採択済み、split design）
- 作業: 採択済み split design の二本立て — (1) `//gom:allow E0301` ディレクティブ（relaxable フラグを持つコードを warning 格下げ、初期は E0301/E0302 のみ。型エラー系の格下げや severity 一括フィルタは採用外）(2) `--strict` フラグ（warnings を error 扱いの exit code、CI 固定用途）。対象ファイル: codes.go（relaxable フラグ）・syntax のディレクティブ収集・build/cmd の報告適用・spec §12
- 規模: 中

### B. E0404/E0401 文言統一（decision draft 採否待ちの実装）

- 出典: docs/decisions/2026-11-22-e0404-gom-call-domain.md（draft）+ lane B problems 棚卸し
- 作業: E0404 は Go 専用維持・E0401 に未適用ラベルの文言案を実装（採用確定後）
- 規模: 小

### C. imported record 曖昧候補集合（decision draft 採否待ちの実装）

- 出典: docs/decisions/2026-11-22-imported-record-literal.md（draft — 現状維持 A 推奨）
- 作業: 採択が B/C なら unannotated literal の候補スコープを絞る実装
- 規模: 中（B/C の場合のみ）

### D. `|?>` 文言・scope 拡張

- 出典: wave30 lane D open-questions 繰り越し
- 作業: エラーメッセージ/文言の見直し + Scope 脱出判定の適用範囲拡張検討
- 規模: 小〜中

### E. 診断コード残件の fixture 化検討（任意）

- 出典: docs/sketch/2026-11-22-diag-fixture-coverage.md
- 作業: E0906（budget guard）等、除外としたもののうち fixture 化可能なものがあれば追加。多くは意図的除外なので薄い
- 規模: 小（採否は要相談）

## open-questions（採択時に聞く）

1. `--strict` の対象コード集合 — 全 warning 昇格 vs 選択式
2. lane B/C は decision draft の採否が前提 — 不採なら lane 自体を落とす
3. 6 lane すべてが docs/small 系なら wave32 はテーマ「決着済み設計の実装」で 2〜3 lane に絞るか
