# 診断コード ↔ spec fixture カバレッジ調査 (wave31 lane D)

- 日付: 2026-11-22
- 目的: `internal/diag/codes.go` の codeTable 全コードについて `testdata/spec/` の fixture で起こせるかを棚卸しし、起こせるものは fixture を追加、起こせないものは除外理由を記録する。

## 結果

codeTable 81 コードのうち 61 件カバー（既存 fixture + 本 lane の 9 件追加を含む、ディレクトリケース含む）。残り 20 件は理由つきで fixture 対象外。

訂正: 初稿は「84 コード・59 カバー・25 除外」と誤記していた — W0001/W0002/E9999 は codeTable に無いテスト専用コードでカウント対象外、E0906 は既存 fixture（`eq_limit/`）があった（Devin Review 指摘）。

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
| E0602 | disallowed declaration in .gomi | E0602_gomi_body/ （ディレクトリケース: a.gom + m.gomi） |

## 除外と理由

**ドライバ層（`.gom` の内容では起こせない）**

- E0001 Go toolchain error — 外部ツールチェーンの失敗が必要
- E0002 invalid input path — コマンドライン引数の失敗
- E0105 invalid byte in literal — fixture ファイル自身が不正 UTF-8 になる必要があり、git 管理する `.gom` として不適。lexer のユニットテストでカバー済み

**予約・退役コード**

- E0204 unknown attribute — 発射地点なし（unknown attribute は構文面で E0101 に移行済み、番号は予約のまま）
- E0305 non-exhaustive select — Retired 済み
- E0503 implicit numeric conversion — 発射地点なし（暗黙変換は現在 E0401 mismatch で報告、番号は予約のまま）

**typed-AST 不変条件のガード（構文からは到達不能）**

- E0502 method call on a Gom value — resolve が Gom 値に KindMethod を付けるのは typed builder 経由のみ。`r.nosuch` は E0401 で先に落ちる
- E0704 select receive arm must bind var/`_` — 文法上 receive arm は `name <-` のみ受理し、パターンはそもそもパースされない（E0101）。check ユニットテストでカバー済み
- E0901 lowering invariant violated — 内部不変条件。ユーザー入力からは原理上到達不能

**パッケージディレクトリ / ローダ層（`build.CompileFiles` の spec パスからは到達不能）**

- E0601 missing .gomi / E0603 impl mismatch .gomi / E0604 stray .gomi — 発射地点は `cmd/gom/iface.go` の CLI パッケージチェックのみ（`gom check/build <dir>` がディレクトリ内の `.gomi` を発見する経路）。spec fixture はファイルリストを渡すので `.gomi` の発見・照合を通らない（実証: `E0603_gomi_mismatch` ケースは診断 0 件）
- E0606 package import cycle — 複数パッケージのロードが必要（gomimport ローダ経路）。`internal/gomimport/cycle_test.go` で広くカバー済み
- E0607 member emits colliding Go name — 宣言側の発射は iface.Check（CLI 経路）、参照側はローダが .gomi メンバに付ける EmitClash 経由。単一パッケージの CompileFiles では立たない

**eval 実行時 / 内部エラー（spec fixture はコンパイル診断のみ）**

- E9001–E9006 — eval フェーズ・ランタイム。eval テスト側の管轄

## 補足

- カバレッジの数え方: `codeTable` の全コードに対し `testdata/spec/**/*.expected.json`（ルート直下 + ディレクトリケース）に現れるコードをマッピング
- E0906 は `eq_limit/E0906_eq_budget.gom` で既にカバー済み — サブディレクトリに置かれるのは「`==` の展開がマルチ秒かかり、seed glob (`testdata/spec/*.gom`) に入れると FuzzCompile の 8s 予算を食う」という意図的な配置（fixture 内コメント参照）
- 「字句系は fixture にしない」方針は prompts どおり — ただし E0102–E0104/E0106 は `.gom` ファイルとして自然に書けるため fixture 化（E0105 のみバイトレベルのため除外）
