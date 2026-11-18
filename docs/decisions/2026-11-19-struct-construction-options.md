# Go struct 構築機構の選択肢の固定（採否は未決）

- 状態: draft（採否は部分決着: (b) 採用・(a) 先延ばし —
  [decision](2026-11-19-struct-field-write.md)）
- 日付: 2026-11-19
- 問い: [decision 2026-11-17-struct-construction](2026-11-17-struct-construction.md)
  の再検討 — 採用 (c) factory-extern の前提だった障害（named int 算術・
  func-typed field 供給）が解消したので、構築機構の選択肢を改めて固定する

## 決定（draft）

採否は wave24 以降に podhmo が決める。本書は選択肢と比較材料の固定のみ。
調査・実験の記録は [sketch](../sketch/2026-11-19-wave23-struct-construction.md)。

## 選択肢

- (a) named Go struct 型に限定したコンポジットリテラル
  `http.Client { Timeout = ..., CheckRedirect = ... }` — 構文・型検査・
  lower/codegen の新面。partial init・unexported/embedded の規則設計が要る
- (b) `Ptr T` 経由の field 書き込み（`c.Timeout = ...`）+ ゼロ値構築
  （`Ptr.new ()` — 実装済み）。新規は代入構文と addressability 規則のみ
- (c') `@extern fn` のユーザーモジュール運用 — 実装済み。Go コードが
  生成パッケージ隣に移るだけで host 根絶とは言いにくい
- (c) 現状維持（factory は別パッケージの手書き Go）— 参考の維持案

## 判定材料の要約

- 言語完結性: (a) > (b) > (c') = (c)
- spec/実装の小ささ: (c') = (c) > (b) > (a)
- 値の不変性: (a)(c')(c) は侵さない。(b) は `Ptr` 限定なら既存の
  「ハンドル型経由の可変性」枠内
- 採否は排他でない: (a) 構築 + (b) 構築後調整の併用も可能。(c') は
  どの案を採っても残る使い道がある

## 影響（採否時）

- spec 8.3 節の「構築は Go 側の factory 関数に限る」の更新、(a)/(b) 採用なら
  元の decision を消さず本 decision から Supersedes で繋ぐ
