# ラムダパラメータの `(x: T)` 型注釈

- 状態: adopted
- 日付: 2026-11-19
- 問い: ラムダのパラメータは bare ident / `_` / `()` のみで、
  型注釈の書き方がなかった
  （[problems/2026-11-01-lambda-param-forms](../problems/2026-11-01-lambda-param-forms.md) の
  typed-param 項目）— wave27 lane B の対応

## 決定

`(name: Type)` を採用 — `fn (x: Int) -> x + 1`、カリー化で
`fn (x: Int) (y: Int) -> x + y`。`fn` 宣言の `(name: Type)` 仮引数と
同じ形・同じ意味（パターンは ident 限定、ラベルなし）。
`(x)` のように型を省略した括弧形は `fn` 宣言と同じく
"needs a : Type annotation" のエラー。

- 採用理由: 宣言側の既存規則と揃う最小の拡張。check 側の
  `checkLambda` は `Param.Type` を既に消費しており、変更は
  syntax + format のみで収まる。
- 先送り: `(a, b)` などのパターンパラメータ、`(~x: T)` のラベル
  パラメータ（problems の残項目は引き続き未解決）。
- 効用: `Go.Any` want のラムダ（`fn a -> a == x` でパラメータが
  使用箇所から先に bind される拒否、implementation-gaps
  `E0401_go_any_lambda_use`）に `fn (a: Go.Any) -> a == x`
  という脱出経路ができる。

## 検証

- spec fixture: `ok_lambda_ann_param`（受理）、
  `err_lambda_ann_param`（`(x)` 型なしの E0101）
- tree-sitter `lambda_param` に `( ident : type_expr )` を追加、
  corpus 27 件パス
- `gom fmt` は注釈を `(x: Int)` の形で保持
