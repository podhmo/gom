# 関数適用は並置 `f x y`、関数はカリー化

- 状態: 採用
- 日付: 2026-11-01

## 背景

最初の案は `f(a, b)` 形式の呼び出しで、カリー化はしなかった（Go の関数シグネチャと 1:1 に対応させるため）。しかし言語の書き味として、ML / Haskell 流の並置が重要だと判断した。一方で、SML / OCaml の構文全体（`let ... in` など）や Haskell の記号的な構文は好まない。

## 決定

- 関数適用は並置 `f x y`。型の適用も並置（`Result Unit TxError`）
- 関数はすべてカリー化する。部分適用の専用構文は持たない
- ブロックと制御構造は波括弧のまま

## 影響

- コード生成で arity 解析が必要になる。既知の関数への引数が揃った呼び出しは Go の多引数呼び出しに直接変換し、普段のコストを増やさない
- 関数値はカリー化形 `func(A) func(B) C` で持つ → [function-representation](2026-11-01-function-representation.md) で arity 形に確定（末尾の追記を参照）
- パイプ `x |> f y` は `f y x`（データを最後の引数に取る）規約になる
- 改行だけでは「次のトークンが引数か」を決められないので、改行の規則が必要になった（[no-offside-rule-and-gom-fmt](2026-11-01-no-offside-rule-and-gom-fmt.md)）
- 後置 `?` が読みにくくなった（[try-keyword](2026-11-01-try-keyword.md)）

## 追記

- 「関数値はカリー化形 `func(A) func(B) C` で持つ」という表現面の話は、[function-representation](2026-11-01-function-representation.md) で arity 形（`func(A, B) C`）に決まった。言語の意味としてのカリー化（`f x` が残りを取る関数になる、部分適用の専用構文を持たない）と、並置の構文はこの決定どおり変わらない
