# 診断コード ↔ spec fixture カバレッジ調査 (wave31 lane D)

- 日付: 2026-11-22
- 目的: `internal/diag/codes.go` の codeTable 全コードについて `testdata/spec/` の fixture で起こせるかを棚卸しし、起こせるものは fixture を追加、起こせないものは除外理由を記録する。

## 結果

codeTable 84 コードのうち既存 fixture 51 件 + 本 lane で 8 件追加 = 59 件カバー。残り 25 件は理由つきで fixture 対象外。

## 追加した fixture

| コード | 意味 | fixture |
| --- | --- | --- |
| E0102 | unterminated string literal | E0102_unterminated_string.gom |
| E0103 | unterminated block comment | E0103_unterminated_comment.gom |
| E0104 | invalid escape sequence | E0104_bad_escape.gom |
| E0106 | reserved word used as a name | E0106_reserved_word.gom |
| E0302 | unreachable match arm | E0302_unreachable_arm.gom |
| E0303 | refutable pattern in irrefutable position | E0303_refutable_let_pattern.gom |
| E0304 | or-pattern alternatives bind different vars | E0304_or_pattern_different_vars.gom |
| W0801 | quadratic list accumulation | W0801_quadratic_accum.gom |

## 除外と理由

**ドライバ層（`.gom` の内容では起こせない）**

- E0001 Go toolchain error — 外部ツールチェーンの失敗が必要
- E0002 invalid input path — コマンドライン引数の失敗
- E0105 invalid byte in literal — fixture ファイル自身が不正 UTF-8 になる必要があり、git 管理する `.gom` として不適。lexer のユニットテストでカバー済み

**予約・退役コード**

- E0204 unknown attribute — 発射地点なし（unknown attribute は構文面で E0101 に移行済み、番号は予約のまま）
- E0305 non-exhaustive select — Retired 済み
- E0503 implicit numeric conversion — 発射地点なし（暗黙変換は現在 E0401 mismatch で報告、番号は予約のまま）
- W0001/W0002 — 汎用 warning プレースホルダ。diag パッケージのテストでのみ登場、エミッタなし

**typed-AST 不変条件のガード（構文からは到達不能）**

- E0502 method call on a Gom value — resolve が Gom 値に KindMethod を付けるのは typed builder 経由のみ。`r.nosuch` は E0401 で先に落ちる
- E0704 select receive arm must bind var/`_` — 文法上 receive arm は `name <-` のみ受理し、パターンはそもそもパースされない（E0101）。check ユニットテストでカバー済み
- E0901 lowering invariant violated — 内部不変条件。ユーザー入力からは原理上到達不能
- E0906 equality comparator expansion exceeds budget — 実装リソース上限のガード。lower のユニットテストでカバー

**マルチファイル / .gomi モジュール層（spec fixture は単一ファイル）**

- E0601 missing .gomi / E0602 disallowed decl in .gomi / E0603 impl mismatch .gomi / E0604 stray .gomi — interface ファイル連携が必要。gomimport / モジュールテストでカバー
- E0606 package import cycle — パッケージ間
- E0607 member emits colliding Go name — 単一ファイル内の同名宣言は E0202 が先に立つ

**eval 実行時 / 内部エラー（spec fixture はコンパイル診断のみ）**

- E9001–E9006, E9999 — eval フェーズ・ランタイム・コンパイラ内部。eval テスト側の管轄

## 補足

- カバレッジの数え方: `codeTable` の全コードに対し `testdata/spec/*.expected.json` に現れるコードをマッピング
- 「字句系は fixture にしない」方針は prompts どおり — ただし E0102–E0104/E0106 は `.gom` ファイルとして自然に書けるため fixture 化（E0105 のみバイトレベルのため除外）
