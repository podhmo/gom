# wave35 prompts 草案

- 状態: 草案
- 日付: 2026-11-23
- 前提: wave34 スタックがマージされた新しい main からレーンごとに
  ブランチを切る（lane 同士はチェーンでよい）

採否・実装可否・条件は wave 開始前インタビューで確定する
（docs/ja/wave-process.md）。wave34 の 3 decision draft
（scope-cleanup-extension=案 D・go-abi-stability=案 A・
local-let-polymorphism=案 A）の採否も同じインタビューで。

## レーン案

### A. `|?>` 回避経路の文書化（採択済み decision の実施）

- 出典: wave33 lane D decision（pipe-try-wording）案 C 採択済み
  — 残る作業は回避経路の文書化のみ
- 作業: `*rt.StepError.Step`・`rt.StepErr`・`Result.map_err`・
  `try`+`match` で文言を制御する方法を spec と
  gom-agent-guide に追記。decision の状態更新
- 規模: 小

### B. scoped-cleanup 案 D の実施（採択次第）

- 出典: wave34 lane B decision draft — 採択なら「代替経路を
  helper で書ける」の文書化が残る
- 作業: `run_result` の GoError 制約を越えるパターンを
  helper で書く例を guide（Scope 節）と spec に追記。
  decision の状態更新
- 規模: 小

### C. Go ABI 案 B の調査（採択次第・拡張余地）

- 出典: wave34 lane C decision draft の「案 B の余地」
- 作業: 調査 → decision。emit 名の衝突検出（編集で既存名が
  変わらない保証）を入れるなら何を検出器にするか — 生成名の
  一覧化・suffix 衝突・暗黙 member 名。実装まで行くかは調査
  次第
- 規模: 小〜中（調査）

### D. ラベル付き引数の制限緩和の調査

- 出典: open-questions Future work（型が未知の関数へのラベル
  付き適用、ラベル付き／なし関数の暗黙変換）
- 作業: 調査 → decision。緩和したい具体例の洗い出し（inline
  ラムダへのラベル適用など）、unification への影響範囲
- 規模: 中（調査）

### E. result-taint flag + gomi export（案 B 拡張の調査）

- 出典: wave34 lane A の条件付き採択（「B に拡張する可能性を
  残す」）— 外部パッケージの non-escaping 引数が戻り値経由で
  リークするケースの精度向上
- 作業: 調査 → decision。result-taint を `.gomi` にエクスポート
  する設計（`//gom:` directive の形・後方互換）と、
  retainedProducers の近似精度の残課題
- 規模: 中（調査）

## 繰り越し注記

- open-questions の大きい項（Chan の Send 制限・List ハイブリッド
  表現・インクリメンタルビルド・deriving）は規模が wave の
  lane に収まらないため引き続き繰り越し。Char↔Int32 は
  wave31 lane F で「ずれなし」確定済み
