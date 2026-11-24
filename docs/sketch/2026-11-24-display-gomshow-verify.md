# display composite 表示（gomShow）の検証記録

- 日付: 2026-11-24
- 対象: wave43 lane C — `"${expr}"` が composite を gomShow 経由で
  描く件（[decision](../decisions/2026-11-24-display-composite-gomshow.md)）
- 目的: decision が参照するプロトタイプ検証の記録（prototype →
  adopted の要件）

## 検証方法

`/tmp/probe`（`replace` 済み module）で `gom run`（compiled）と
`gom build` + `go build` の両経路で同一 .gom を走らせ、splice
出力を比較した。結果は `testdata/realworld/display` として
fixture 化し stdout を verbatim で pin（run の再現手順は
`cmd/gom/realworld_test.go` を参照）。

## 結果

```
{ name = "n", age = 3 }
[1, 2] (({ name = "n", age = 3 }), 7) Circle ~r: 1.5
[a b] (([a b]), 1)
Err (error("boom"))
```

両経路で一致。確認した面:

- record `{ name = "n", age = 3 }`、list `[1, 2]`、tuple
  `((…), 7)`、ADT `Circle ~r: 1.5` は composite 形式
- `Go.Slice` は whole-splice / ネストの両方で `%v`（`[a b]`）—
  レビューで指摘された nested 経路の Gom list 化（`["a","b"]`）
  は `showKindOf` の `Go.Slice` → `showRuntime` 修正で解消
- `GoError` は評価器と同じ `error("text")` 形式。typed-nil error
  の `Error()` panic は fmt 同様 `%!v(PANIC=…)` に rescue

## 残件

- `let f : Flag = true`（1-field record へのスカラー代入）の
  splice は eval が `true`・compiled が `{ on = true }` — 値の
  保持表現差（scalar vs record）に由来する表示の揺れ。表示の正
  （正準形）を決める decision が要る: eval を VRecord 化すると
  スカラー算術が壊れる、codegen をスカラー化すると record
  literal との整合が取れない。implementation-gaps と #216 の
  review thread に記録済み。
- 型パラメータを残す composite（generic 関数内の `List a` 等）
  は package 側で要素を描けず `%v` に落ちる — spec に但書追記済み。
