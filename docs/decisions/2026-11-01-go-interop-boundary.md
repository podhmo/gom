# Go 相互運用の境界

- 状態: 採用
- 日付: 2026-11-01
- Supersedes: [go-calls-as-tuples](2026-11-01-go-calls-as-tuples.md) のうち `(T, bool)` と数値型の自動変換

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

## 検証の追記（2026-11-02、P0-5）

`go/types` を使った実地の検証（[sketch: P0-5](../sketch/2026-11-02-p0-5-go-types-mapping.md)）で以下が確認された。

- Go の型は `*types.Named`（named scalar・named interface・named struct を含む）を同一性のキーとして保持する。`types.Identical` は named type の object 同一性を見るため、**1 回のコンパイルでは 1 つの読み込みセッション（`packages.Load` / importer）を共有する必要がある**。セッションをまたぐと同名の型でも Identical にならないことが実験で確認された
- `(T, error)` の判定は `types.Identical(末尾結果, universe error)` で実装できる。`any` は export data 上では `*types.Alias`、`interface{}` と書かれた場合は `*types.Interface` と、表記によってノードが変わるため、`Unalias` 後に empty interface かを見る構造的な判定が必要（alias の有無では検出できない）
- `Go.conv` の可否は `types.ConvertibleTo`、interface への代入可否は `types.AssignableTo`、nil 代入可否は `AssignableTo(untyped nil, T)`、メソッド・フィールド解決は `types.LookupFieldOrMethod` で実装できることが確認された。ジェネリック呼び出しの制約検査は `types.Instantiate(…, validate=true)` をそのまま使える
- `nil` になりうる型は underlying が pointer / slice / map / chan / func / interface / `unsafe.Pointer` のもの（named type は基底型に従う。`*types.TypeParam` の underlying は制約 interface を返す罠がある）。`*T`・interface・func を返す Go 関数の戻り値は nil を持ちうる値として扱う
- Go のジェネリック関数の制約判定は `IsMethodSet() || IsComparable()`（+ union/`~` の検出）で分類できる。ただし `any`/`comparable` だけの厳格な境界では `slices`/`maps`/`cmp` の主要な関数（`slices.Contains` `[S ~[]E, E comparable]` など）が呼べないことが分かった。`~[]E`/`~map[K]V` 形状の制約まで広げるかは P3-3 実装前に再検討する（open-questions.md）
- Go の struct 型のフィールド参照・コンポジットリテラル（`srv.Addr`、`http.Server{Addr: ...}`）は spec 8.3 の対応表にない。v1 で扱うかは未決（open-questions.md）
