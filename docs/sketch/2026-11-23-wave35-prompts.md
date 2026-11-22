# wave35 prompts

- 状態: 採択済み（2026-11-23 インタビュー — lane A–E 全採択）
- 日付: 2026-11-23
- 前提: wave34 スタック #147 全件 squash merge 済みの新しい
  main からレーンごとにブランチを切る（lane 同士はチェーンでよい）
- 併せて確定した wave34 残 decision: scope-cleanup-extension=
  案 D（現状維持+文書化）、go-abi-stability=案 B（衝突検出込み — 
  draft 推奨の案 A を越えて採択）、local-let-polymorphism=案 A
  （現状維持）

## レーン（採択済み）

### A. `|?>` 回避経路の文書化（採択済み decision の実施）

- 出典: wave33 lane D decision（pipe-try-wording）案 C 採択済み
  — 残る作業は回避経路の文書化のみ
- 作業: `*rt.StepError.Step`・`rt.StepErr`・`Result.map_err`・
  `try`+`match` で文言を制御する方法を spec と
  gom-agent-guide に追記。decision の状態更新
- 規模: 小

### B. scoped-cleanup 案 D の実施（案 D 採択済み）

- 出典: wave34 lane B decision — 案 D 採択済み。「代替経路を
  helper で書ける」の文書化が残る
- 作業: `run_result` の GoError 制約を越えるパターンを
  helper で書く例を guide（Scope 節）と spec に追記。
  decision の状態更新
- 規模: 小

### C. Go ABI 案 B: emit 名衝突検出の実装

- 出典: wave34 lane C decision — 案 B 採択（draft 推奨の案 A を
  越えて）。emit 名の衝突検出を入れる
- 作業: 生成名の一覧化・suffix 衝突（同 base の `x1`/`x2`
  追い出し）・暗黙 member 名（sum variant struct・marker・
  test 名）を検出する機構。E0607 が止めるのは base 名が同じ
  `.gomi` メンバの衝突までで、生成 suffix への追い出し
  （非公開 decl が `Foo1` を先取りして公開 decl が `Foo11` に
  ずれる等）は対象外 — lane C の検出対象。検出器の形
  （validator / lint / `--strict` 系）は調査しながら決める
- 規模: 中

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
