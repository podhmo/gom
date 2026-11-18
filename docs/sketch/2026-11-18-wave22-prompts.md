# wave22 — prompts 草案

wave21 締め時点での草案。**採択が要る点は作成時に podhmo に聞く**
（prompts-drafting rules: 実行時に開いた問いを残さない）。各 lane は
1 branch = 1 PR・直列実行・Devin Review レビューの従来どおり。

## Lane candidates（優先順位・採択は podhmo）

### 案 1: LSP shared importer + member table 共有（wave19 で経路設計済み）

open-questions「LSP の Go import oracle の共有」の実行経路:
LSP server が workspace dir ごとに長寿命 `goimport.Loader` を1本
持ち回り、compile 毎に exportcache fingerprint（dep dir 単位の
max mtime+件数）を照合、miss で Loader ごと作り直し。`*types.Named`
identity が安定して member table を compile 間で共有できる。
per-change 数 ms 削減・費用対効果は中程度。LSP lane 不設置ルール
（wave21 prompts）どおり、採用するなら他 lane の直列サブタスク
として畳む。

- LSP lanes verify in-process over JSON-RPC。
- wave21 lane3 で `goimport.Loader` に mutex が入った — 共有前提の
  同期コストは既に支払い済み（同時利用は変わらず serial）。

### 案 2: flat パラメータの部分適用 timing 差の解消

wave20 lane2 の残存差: flat 宣言パラメータの `f a` は residual
到着まで発火を遅延する（decision 2026-11-18-flat-fn-params・
spec §1.3 に記録済み）。eval 側に residual partial を移植して
divergence を消すか。規模は小中 — 実装より先に「残存差が観測
可能なユーザー影響か」を判定する調査タスク向き。

### 案 3: `TypeArgs` の (b) 移行判定

wave21 lane1 は (a) optional-but-authoritative を採択。(b)
`Typ` canonical への移行は lower/codegen のフィールド撤去を伴う。
調査 lane（移行コストの棚卸し + 利得の再評価）か実装 lane かは
podhmo の判断。open-questions に future work として記録済み。

### 案 4: 並列ビルドの実測ベンチ

wave21 lane3 で並列化した `compileInputs` の効果計測。複数 dir・
dep 連鎖の fixture を作って wall-clock と .gomi 生成順を検証する
計測 lane。並列化の価値判定にも使える。

## Post-lane work（lane 全完了後）

round 記録・skill 同期・wave23 prompts 草案・台帳掃除。
