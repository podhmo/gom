# 部分適用 lint の調査（wave39 lane C）

## 問い

部分適用・`Unit` 残り関数値を lint で警告するか、どこまでを警告
対象にするか。

## 実測

`fn add (x: Int) (y: Int) -> Int` に対して:

- `add 1` を mid-block に置く → E0402（Int -> Int を持つ文は
  Unit でない）。`ignore (...)` の fix が出る
- `ignore (add 1)` → 警告なしで通る
- `let _ = add 1` → 警告なしで通る
- `let g = add 1` → 正当（束縛して後で適用するのはカリー言語の
  正常形）

## 結論

「関数値を捨てる面」は `ignore` と `_` 束縛の 2 箇所のみ。それ
以外（文位置）は E0402、型が要求する位置は HM の型が厳密に守る。
警告対象は Func 値の捨てに限定するのが釣り合う — decision:
docs/decisions/2026-11-24-partial-application-lint.md。
