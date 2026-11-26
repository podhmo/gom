# stdlib の Result traverse helpers — sequence / traverse_result

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-27-wave49-lane-c-result-traverse-verify.md)）
- 日付: 2026-11-27

## 決定

stdlib に次の 2 つを追加する。

- `List.traverse_result : (a -> Result b e) -> List a -> Result (List b) e`
- `Result.sequence : List (Result a e) -> Result (List a) e`

どちらも要素を入力順に処理し、最初の `Err` で残りを処理せずにその `Err` を返す。すべて `Ok` なら payload を入力順に集めた list を `Ok` に入れる。不正な zero `Result` は match と同じく panic する。Option を Result へ畳む helper は追加しない。

`Result.sequence` は `Result` モジュールの面に載せるが、実装は `stdlib/list` に置く compiler-owned forwarding member とする。`List.traverse_result` は `list` package の `@extern` とし、手書き Go が 1 回の buffer 構築で実装する。

## 背景

DMMF の validation pipeline では `List.map validate input |> Result.sequence` の形が頻出するが、stdlib に traversal 面がなかった。

配置には既存の層制約がある。`result` は葉、`option` は `result` に、`list` は `option` に依存する DAG なので、`result` package に `sequence` を素直に実装すると `result` が `list` を import して循環する。これは `Result.to_option` / `Option.to_list` と同じ問題なので、同じく下層モジュール名・上層パッケージ実装の forwarding member にする（[stdlib-conv-placement](2026-11-18-stdlib-conv-placement.md)）。

実装方式は pure Gom と `@extern` の 2 案があった。pure Gom の reverse accumulator は末尾再帰にできるが、slice 表現の `List` では cons が 1 要素ごとに既存 backing をコピーするため O(n²) になる。既存の `map` / `filter` / `concat` と同じく、順序付きで list を構築する処理は buffer-built Go の方が stdlib 流儀に合うため `@extern` を採る。

## 実装

- `stdlib/list/list.gom` / `.gomi` に `traverse_result` を宣言し、`stdlib/list/impl.go` の `TraverseResult` が本体を持つ
- `internal/stdlib` の `conversionModules` が `Result.sequence` を `list.ResultSequence` へ forward する
- `Result.sequence` は `.gomi` に書けないため、`result.gom` のコメントと spec §5 のモジュール説明が公開面の記録を持つ

## 検証

- `stdlib/list/list.gom` の inline test: 成功列・最初の Err・`Result.sequence`
- `stdlib/list/boundary_test.go`: 入力順、最初の Err で停止、zero Result の panic
- `testdata/spec/ok_stdlib_conv.gom` と `testdata/golden/stdlib_conv.golden.go`: surface と generated Go の forwarding 先
