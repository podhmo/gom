# wave34 prompts 草案

- 状態: 採択待ち
- 日付: 2026-11-23
- 前提: wave33 スタックがマージされた新しい main からレーンごとに
  ブランチを切る（lane 同士はチェーンでよい）

採否・実装可否・条件は wave 開始前インタビューで確定する
（docs/ja/wave-process.md）。

## レーン案

### A. 結果経由 Scope リークの検出（result-danger）

- 出典: wave33 lane C の decision draft（採択次第）
- 作業: 案 A の実装 — danger 位置の呼出しで結果型が関数を含む
  とき、引数ラムダの本体結果位置を danger で walk。ローカル
  callee は retainedVars で絞る、opaque は保守的 true。
  spec fixtures・decision の状態更新・台帳の項目を [x] に
- 規模: 中

### B. scoped cleanup の拡張調査

- 出典: open-questions Future work（scoped cleanup 拡張）
- 作業: 調査 → decision。GoError 以外のエラー型で cleanup の
  失敗を合流させる版（結合関数を明示的に受け取る）と、取得と
  登録を一度に書く糖衣（`s.closing` 等）のどちらに重みを置くか。
  wave30 で Deferred → 残った論点の切り分け
- 規模: 中（調査）

### C. Go ABI 安定方針の文書化

- 出典: open-questions Go 相互運用節（唯一の未決）
- 作業: 調査 → decision。生成 Go の型名・シグネチャ・バリアント
  struct 名をバージョンをまたいでどこまで安定させるか。現状の
  実際の揺れどころ（emit 名の claim 順・suffix 重複回避・
  @method 名）を棚卸して方針文を書く
- 規模: 小〜中（調査）

### D. Core validator の CallGo コールバック arity 検査

- 出典: open-questions Future work 末尾
- 作業: 実装 — `CallGo` に渡すコールバックの arity が Go 側宣言と
  合うか `core.Validate` で E0901 として出す。FuzzCompile が拾う
  壊れ方を手前で検査。go/types 依存を増やさず types の表現で
  検査できるかが論点
- 規模: 中

### E. ローカル let 多相の調査

- 出典: open-questions Future work（ローカル let 多相）
- 作業: 調査 → decision。relaxed value restriction を検討。
  現行 value restriction の境界例と、OCaml の relaxed の
  差分コスト
- 規模: 小〜中（調査）

## 繰り越し注記

- wave33 の 3 decision draft（lane C/D/E）は wave34 開始前の
  インタビューで採否を確定する — 採択なら lane A/C/E の内容が
  変わり得る。案が棄却された lane は外す
- 台帳の未解消は lane A の対象のみ
