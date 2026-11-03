# P2-3 labeled arguments: end-to-end verification

- 日付: 2026-11-05
- 状態: verified — v1 規則は全フェーズ実装済み、回帰資産を追加

## スコープと結論

P2-3 の範囲は spec 2 章のラベル付き必須引数
([decision](../decisions/2026-11-01-labeled-required-arguments.md)):
宣言 `(~name: T)`、呼び出し `~name: e`、パンニング `~name`、
並べ替え時の評価順保存 (design.md 2.1)、部分適用、型の扱い (2.1)、
バリアントの同名記法 (2.6)。

確認の結果、実装は P2-2 の呼び出し情報の構造化とともにほぼ全量が
すでに入っていた。本ラウンドでは既存実装の規則ごとの検証と、
repo ルールが求める回帰資産 (spec fixture・golden・eval/compiled
比較) の追加を行った。新規の実装変更はない。

## フェーズごとの所在

| フェーズ | 所在 |
| --- | --- |
| syntax | `Arg{Label,Punned}`・`Param.Label`・`Field.Label`・`CtorArg.Label`・ラベル付き arrow 型 (`internal/syntax/expr.go`, `decl.go`, `pat.go`, `parse_expr.go`)。パンニング `~x` は parse 時に `~x: x` へ展開。`:` は貪欲で `f ~x : T` は `f (~x: T)` |
| check | `checkGomApply`/`checkGoApply` が `CallInfo.ArgToParam` (書いた順 → 宣言順 index) を構造化し E0404–E0408 を報告 (`internal/check/apply.go`)。ラベル run の比較は `unify.go` の `labeledRun`、型同値は `types/equal.go` `normalizeLabels` |
| lower | `declOrderArgs` が `ArgToParam` で宣言順へ並べ替え、評価順は ANF `Let` がソース順に保持 (`internal/lower/expr.go:749`)。`TestLabeledArgsOrder` がこの不変条件を固定 |
| eval | `applyArgs` が `CallInfo.ArgToParam` で引数を scatter (`internal/eval/eval.go`) |
| codegen | Core IR の `CallDirect`/`CtorApp`/`Closure` を手続き的に出力するだけ — ラベルは消去済み |

## 規則ごとの検証結果

- **規則1 (位置引数は宣言順、ラベルは順不同)**: `transfer ~amount: 1 ~to: "b" "a"` が `argtoparam=[2,1,0]` を持ち、
  生成 Go は `transfer(tmp9, tmp8, tmp7)` の宣言順。golden `labeled_args.golden.go` で固定。
- **規則2 (値は単純式1つ)**: parser が `~to: f x` を `~to: f` + 位置引数 `x` に分解
  (`parse_expr.go` の app-arg、`testdata/fmt/labels.*.gom`)。
- **規則3 (位置引数でラベル仮引数を埋められない・欠落/重複/未知はエラー)**:
  `transfer "a" "b" 100` は E0401 (`E0401_positional_for_labeled`)。
  重複は E0405、未知ラベルは E0406。欠落は Gom 呼び出しでは部分適用に
  なりエラーにならない (規則5と整合) — E0404 は Go 呼び出しのみで、
  `extern` 経由の `io.Copy ~src: f` で確認 (`E0404_go_missing_label`)。
- **規則4 (評価順は書いた順)**: 副作用つき引数を書いた順に ANF 束縛 —
  golden で `trace("to")` → `trace("from")` → `transfer(...)` の
  並びを固定。eval 側も同じ対応付け (compare テスト)。
- **規則5 (ラベル付き部分適用)**: `transfer "a" ~to: "b"` → 残余
  `~amount: Int -> String`、生成は 1 引数 closure。
  `transfer ~to: "d" ~amount: 1` → 位置引数だけが残る closure。
  両者とも eval/compiled で一致。
- **規則6 (バリアント同名記法)**: 宣言 `| Send (~to: T) (~amount: I)`、
  構築 `Send ~amount: 5 ~to: "b"` → `Tx_Send{To:"b", Amount:5}` の宣言順、
  パターン `Send ~to: who ~amount: n` → フィールド読み。compare で検証。
- **規則7 (パンニング)**: `~to` は `~to: to`。変数不在なら E0201
  (`E0201_label_pun_undefined`)。
- **2.1 (型の扱い)**: `~a -> ~b` と `~b -> ~a` は同じ型
  (`ok_labeled_args` の `apply_it add2`)。未解決型へのラベル呼び出しは
  E0408。ラベルなし関数値をラベル付き型へ代入は暗黙変換せず E0401
  (`E0401_unlabeled_for_labeled_fn`)。

## 追加した回帰資産

- `testdata/spec/`: `ok_labeled_args` (全規則の成功系) + 7 本のエラー系
  (`E0404_go_missing_label`, `E0405_duplicate_label`, `E0406_unknown_label`,
  `E0408_labeled_call_unresolved`, `E0201_label_pun_undefined`,
  `E0401_positional_for_labeled`, `E0401_unlabeled_for_labeled_fn`)。
- `testdata/golden/labeled_args.gom` — ANF 評価順、部分適用 closure、
  バリアント並べ替えを含む生成 Go。
- `internal/build/compare_test.go` の `labeled_args` ケース —
  eval の `applyArgs` と lowering の `declOrderArgs` が同一の対応付けを
  持つことを実行比較 (評価「順」自体は pure な言語では観測不能なため、
  順序の固定は golden と `TestLabeledArgsOrder` が担う)。

## 見つかったずれと記録先

- `docs/problems/2026-11-03-overapply-labeled-params.md` の記述が陳腐化:
  `mk () 5` が位置引数で `~x` を埋めるとあるが、`flattenFunc` が全
  arrow を畳むため `overApply` の `rparams` 分岐は到達不能になっており、
  現在は E0401 で落ちる (結果として規則3と整合)。doc に追記済み、
  台帳にも登録。残る未決は「この E0401 の文言でよいか」と
  「到達不能分岐の削除」。
- `docs/error-codes.md` E0404 の例は発火しない Gom 呼び出しを示していた
  (`transfer alice ~to: bob` は合法な部分適用)。Go callee の例に更新。
  Gom 側の文言自体は `docs/problems/2026-11-02-gom-missing-label-is-partial.md`
  が追跡する未決事項。

## 範囲外 (後続タスク)

- Go callee のラベルは `extern` 宣言でのみ宣言可能 (自動導出される
  Go シグネチャはラベルを持たない)。`extern` の変換規則全般は P3-5。
- `.gomi` のラベル記法は P4-1。
- ラムダのラベル付き仮引数 `fn (~x: Int) -> ...` は grammar.ebnf の
  PROVISIONAL `lambda-param-forms` (docs/problems/2026-11-01-lambda-param-forms.md)。
