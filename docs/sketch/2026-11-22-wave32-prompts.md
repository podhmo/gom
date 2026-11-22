# wave32 prompts

- 日付: 2026-11-22
- 状態: 採択済み — インタビューで確定（docs/ja/wave-process.md 規約）

## テーマ

決着済み設計の実装。wave31 で implementation gaps の棚卸しは完了
（未解消 0）、繰り越した採択済み設計を主軸に 3 lane。

## lanes

### A. `--strict` 実装

- 出典: docs/decisions/2026-11-22-strict-mode.md（採択済み、split design）
- 作業: 採択済み split design の二本立て
  (1) `//gom:allow E0301` ディレクティブ（relaxable フラグを持つコードを
      warning 格下げ、初期は E0301/E0302 のみ。型エラー系の格下げや
      severity 一括フィルタは採用外）
  (2) `--strict` フラグ — **採択済みの振る舞い**: 裸の `--strict` は全
      warning を error 扱いの exit code、`--strict=<codes>`（例
      `--strict=E0301`）で昇格対象を選択式に絞れる
  対象ファイル: codes.go（relaxable フラグ）・syntax のディレクティブ
  収集・build/cmd の報告適用・spec §12
- 規模: 中

### B. E0401 文言改善（未適用ラベルを診断に載せる）

- 出典: docs/decisions/2026-11-22-e0404-gom-call-domain.md（採択済み）
- 作業: E0404 は Go 呼出し専用のまま維持。部分適用で残った関数型が
  非関数の期待型と照合できないとき、E0401 の診断に「callee がまだ
  `~x:` を要求している」を `data` または注記として載せる。overapply
  側の `expected ~x: Int -> Int, got Int -> ?n` の `?n` 表記も同じ枠で
  読みやすくする。意味は変えず文言のみ
- 規模: 小（「部分適用の残りである」ことの位置追跡が要る）

### C. `|?>` 文言・scope 拡張（旧 lane D）

- 出典: wave30 lane D open-questions 繰り越し
- 作業: エラーメッセージ/文言の見直し + Scope 脱出判定（E0419）の
  適用範囲拡張検討
- 規模: 小〜中

## 採択時に決まったこと・落とした lane

- `--strict`: 裸指定 = 全 warning 昇格、`<codes>` 指定で選択式
  （インタビュー確定）
- 旧 lane C（imported record 曖昧候補集合）: 案 A 現状維持を採択
  — 実装作業なし。decision を採択に更新（
  docs/decisions/2026-11-22-imported-record-literal.md）
- 旧 lane E（診断コード fixture 残件）: 落とし。除外分はほぼ意図的除外
- lane C/D を含めた 5 lane 案は 3 lane に絞った
