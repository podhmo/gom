# wave36 prompts

- 状態: 採択済み（2026-11-23 インタビュー — lane A–H 全採択）
- 日付: 2026-11-23
- 前提: wave35 スタック（#149→#153）全件マージ済みの新しい
  main から開始。引き続き単一エージェント直列運用、Devin
  Review が PR ごとの品質ゲート
- 開始条件: **wave35 マージ後に開始**（インタビュー＝prompts
  作成のみ。lane 実行はまだ行わない）

## レーン（採択済み）

### A. emit 名 verbatim 二重請求のエラー化（knownBugs 駆動）

- 出典: TODO — wave35 lane C で検出、knownCompileBugs 2 件
  （`type Foo_Bar`+`type FooBar` → `FooBar` ×2、
  `| Some_Opt | SomeOpt` → `T_SomeOpt` ×2 が壊れた Go を emit）
- 採択: **方針 (a) エラー化** — 衝突位置でエラーを出して生成を
  止める。(b) suffix 退避は不採用（公開 ABI 名が暗黙に変わるため）
- 作業: W0601 の verbatim 二重請求を新 E-code のエラーに格上げし
  壊れた Go を withhold。unitFailed への接続（codegen 診断の
  エラーで全出力を抑止）。クロスファイル型名衝突
  （`a.gom: Foo_Bar` + `b.gom: FooBar`）が sibling 抑制で両
  ファイルとも沈黙しないよう報告経路を確認。knownBugs 解除 +
  spec/decision 同期
- 規模: 小〜中

### B. unlabeled → labeled introduction の実装

- 出典: TODO — wave35 lane D decision 採択済み
- 作業: expected 型が `~x` を宣言する位置で produced のラベル
  なし param に位置対応でラベルを貸す（`Int -> Int` ≤
  `~x: Int -> Int`）。逆方向・混在は拒否のまま。spec §2 の
  「暗黙変換はしない」は lane D で先行同期済み
- 規模: 中（check の unify 経路）

### C. result-taint 推論 + 診断接続（案 B 前半）

- 出典: TODO — wave35 lane E decision 採択済み
- 作業: `Symbol.ResultTaint []bool`（結果が param i の生産物を
  運び得る）を fixpoint 推論 — `retainedProducers` + saturated
  call の callee の `ResultTaint` 伝播、相互再帰は保守 true で
  底打ち。escape 診断への接続（imported callee の偽陽性と
  transitive false negative を閉じる）
- 規模: 中

### D. `.gomi` `//gom:result-notaint` directive I/O（案 B 後半）

- 出典: 同上（案 B の export 半分）
- 作業: `//gom:result-notaint <name> <i>` directive の emit・
  parse・`ResultTaint` への復元。`//gom:noescape` と同じ極性
  （列挙位置 = false、欠落 = 保守 true）。依存: lane C
- 規模: 中

### E. `--strict` モードの実装

- 出典: open-questions — strict-mode decision 採択済み
  （「実装は次 wave 以降」と明記されていたものを今 wave で実施）
- 作業: `//gom:allow CODE` directive（初期は E0301/E0302 のみ
  relaxable）+ warnings→errors の exit flag
- 規模: 中

### F. `fn ~x ->` ラムダラベル構文

- 出典: wave35 lane D で future work へ deferred していたもの
- 作業: ラムダのパラメータにラベルを付ける構文（parser + check）。
  lane B の introduction と組になると効果大
- 規模: 小〜中

### G. ユーザー宣言の可変構造（調査 → decision）

- 出典: open-questions 未決 — `Ref`/`Array` 以外の可変構造を
  許すか。W0701/W0702 の可変判定は閉じた builtin リストで
  ユーザー宣言型を扱えていない
- 作業: 調査 → decision
- 規模: 中（調査）

### H. problems 残件の棚卸し（調査 lane）

- 対象: 未解決 `wrong-kind-diagnostics`・`interp-composite-
  display`、`gom-package-imports` の module scope 残件、
  `slow-tests` 継続分
- 作業: 各ファイルを再評価し、解消なら閉じる・残るなら台帳化 or
  lane 候補化を判定
- 規模: 小（調査）

## 繰り越し注記

- 大きい項（List ハイブリッド表現・chan Send 制限・並列化ルール・
  `|?>` 文言カスタマイズ）は wave サイズに収まらないため引き続き
  繰り越し
- lane A の方針 (a) はインタビュー採択。実装中の詳細設計は通常
  手順（decision draft → prototype → 状態更新）で記録する
