# production の `display`/文字列補間は `gomShowN` 機構を再利用する

- 状態: 採用
- 日付: 2026-11-24

## 文脈

`"${expr}"` の splice は `strInterp` が `CallBuiltin{display}` に下げ、
codegen はスカラーだけ strconv、それ以外を `fmt.Sprint` に落としていた。
結果、ADT/record/list/tuple が evaluator の表示（`Ok 42`、`{ n = "x" }`、
`[1, 2]`、`(1, 2)`）と生成バイナリでずれた — Go struct 綴りの出力になる
（[problem](../problems/2026-11-04-interp-composite-display.md)）。

## 決定

test 側の `assert_eq` operand 描画用に既にある `ShowPlan`/`ShowDecl`
（`gomShowN`、design.md §4.8）を production `display` にも載せる。

- `strInterp` は `CallBuiltin{display}` に splice 式の `ShowPlan` を付ける
- codegen の `display` は builtin scalar を従来どおり strconv で描き
  （String は裸・Char はコードポイント — assert の quoted 表示とは別経路）、
  `ShowCall`/`ShowError` は minted decl と `rt.ShowError` に回し、
  それ以外は従来どおり `%v`
- composite 型の `gomShowN` は production からも参照されるため
  `ShowDecl.Prod` を設け、prod 側の decl は main 出力へ、test 側だけの
  decl は従来どおり `_test.go` へ。同一型は dedup 後に prod へ昇格する
- spec の補間の項に composite の表示形式を明記

## 却下した案

- 宣言型ごとの `String()` メソッド生成 → メソッド名の衝突規則と
  interface 化の設計が増え、`gomShowN` の再利用で足りる
- composite は `%v` で「実装依存」と明記 → 利用者向け文字列が
  evaluator と binary で食い違う根本のずれを残す

## 検証

- `tmp_probe/disp.gom` 相当の splice で `Rect ~w: 2 ~h: 3`、
  `{ a = 1, b = "x" }`、`(1, 2, true)`、`[1, 2, 3]`、`Some 42` が
  evaluator の表示と一致
- `testdata/golden/record_one_field.golden.go` 等の補間が
  `gomShowN` 呼び出しになる golden 差分
