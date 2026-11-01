# 0012 コンパイラは Go で実装

- 状態: 採用
- 日付: 2026-11-01

## 背景

候補は Go と OCaml だった。OCaml はパターンマッチでコンパイラを書きやすい。

## 決定

Go で実装する。

## 理由

- `go/ast`・`go/printer`・`go/types`・`go/packages` をそのまま使える。Go パッケージの読み込みと Go コードの出力を自前で書かずに済む
- 単一バイナリで配布できる
- 将来のセルフホストにも移しやすい

## 影響

- 内部 AST は sealed interface＋type switch で表す（Gom が生成するコードと同じ形）。網羅性は go-sumtype のような静的検査ツールで補う
