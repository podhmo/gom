# 0019 Go 相互運用の境界

- 状態: 採用
- 日付: 2026-11-01
- Supersedes: 0005 のうち `(T, bool)` と数値型の自動変換

## 背景

`import go` の難しさは構文ではなく、Go の型システムと実行時の振る舞いを Gom にどう見せるかにある。暗黙の変換が多いと、Go のライブラリを誤った意味に翻訳する。

## 決定

- `(T, error)` は `Result T GoError` に自動変換する
- `(T, bool)` は既定ではタプルのまま。`Option T` にするのは、標準のバインディングか `extern` で comma-ok と宣言した関数だけ
- Go の named type は Gom でも別の名前的な型として保つ。基底型との変換は `Go.conv` で明示する。型は `go/types` の型の同一性を保ったまま扱う
- 数値の暗黙変換は情報が落ちないものだけ。Go の型なし定数は期待される Gom の型になる
- Go の interface は名前的な型として見せ、Go 由来の値は `go/types` の代入規則に従って渡せる。Gom の型が Go の interface を実装することは v1 ではできない
- `map[K]V` は不透明型 `Go.Map K V`
- Gom が表現できない型を含むシンボルは公開しない。`import go` は失敗させず、参照したときに理由付きの診断を出す
- Go のジェネリック関数は、制約が `any` か `comparable` のものだけ呼べる
- `extern` では型だけでなく変換規則も指定できる

## 影響

- `io.Reader` / `io.Writer` などを使う標準ライブラリが v1 から使える
- Go ABI の安定性と、Gom の型が interface を実装する方法は未決
