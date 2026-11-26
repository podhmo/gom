# `&&` / `||` は短絡評価し、complex な右辺は Core IR の分岐へ閉じ込める

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave49-lane-a-shortcircuit-verify.md)）
- 日付: 2026-11-27

## 決定

`&&` / `||` は短絡評価する。`false && x` と `true || x` では `x` を評価しない。

Core IR への lowering は次の形にする。

1. 右辺が atom だけで済むなら `core.Binary` を使ってよい。atom は Core IR の
   不変条件として effect を持たず、Go の `&&` / `||` 自身も lazy なので、
   右辺を enclosing な ANF seq で事前評価しない限り意味は変わらない
2. 右辺に call・match・ネストした演算など complex な評価があれば `core.If`
   へ下ろし、右辺の束縛列を選択された branch body 内に置く

これは最適化ではなく意味の一部である。定数条件の folding はこの決定に
含めない。

## 背景

evaluator は `false && x` / `true || x` で右辺を評価しなかったが、lower は
両 operand を先に atom 化していた。右辺が `todo ()` や副作用を持つ呼び出し
だと、生成コードでは短絡前に評価され、evaluator と compiled の意味が
ずれていた。

選択肢は 2 つあった:

1. Core IR に lazy な logical operator node を足し、codegen が短絡形を選ぶ
2. 既存の `core.If` を使い、右辺の評価を branch body へ閉じ込める

(2) を採る。`core.If` は既に条件式の評価・分岐・型検査の不変条件を持ち、
codegen に新しい意味判断を持ち込まずに済む。右辺が atom の場合だけ
`core.Binary` へ残すのは生成コードの readability のためで、この場合は atom
の事前評価がなく、Go の演算子自体が lazy なので短絡規則を保てる。

## 実装

lower の `binaryExpr` は `&&` / `||` だけを特別扱いする。右辺を独立した
seq で lowering し、結果が atom で束縛列も空なら `core.Binary`、そうでなければ
その束縛列を持つ branch body 付きの `core.If` を返す。`&&` の else は
`false`、`||` の then は `true`。この `core.If` には `ShortCircuit` 印を付け、
`@tailrec` の末尾位置判定からは外す — Core の制御フロー上は分岐結果でも、
ソース上は operator の operand であり、spec の末尾位置ではない。

## 検証

- `internal/build/shortcircuit_test.go` で evaluator / compiled の一致を確認
- `testdata/spec/ok_bool_short_circuit.gom` で構文・型検査の fixture を固定
- `testdata/golden/let_rec.golden.go` と `stdlib/cmp` の generated artifact で
  生成コード差分を確認
