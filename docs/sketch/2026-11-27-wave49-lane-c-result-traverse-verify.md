# wave49 lane C: Result traverse helpers 検証

## 実施

- `List.traverse_result (f: a -> Result b e) (xs: List a) -> Result (List b) e` を stdlib/list に追加した。要素順に `f` を適用し、最初の `Err` で停止する
- `Result.sequence : List (Result a e) -> Result (List a) e` を `Result` モジュールの compiler-owned forwarding member として追加し、実装は `stdlib/list.ResultSequence` に置いた
- `traverse_result` は既存の `map` / `filter` と同じく `@extern` + buffer-built Go にした。pure Gom の reverse accumulator では slice List 表現の cons コピーで O(n²) になるため
- spec §5、design の stdlib layering 説明、guide の stdlib 一覧、`result.gom` の forwarding コメントを同期した

## 結果

- `go run ./cmd/gom test stdlib/list`: pass（inline test に成功列・最初の Err・`Result.sequence` を追加）
- `go test ./stdlib/list -count=1`: pass（入力順、Err 後の要素を読まないこと、zero Result panic）
- `go test ./internal/stdlib ./internal/build -run 'TestStdlibGeneratedFilesAreFresh|TestSpec/ok_stdlib_conv|TestGolden/stdlib_conv' -count=1`: pass
- `testdata/golden/stdlib_conv.golden.go` で `list.ResultSequence` / `list.TraverseResult` への forwarding を確認

## 境界

- Option を Result へ畳む helper は入れない。wave49 の採択 scope は `Result.sequence` と `List.traverse_result` に限定される
- `Result.sequence` は `.gomi` には書けない。書くと `result` package に実装義務が生じ、`result -> list` の upward import で循環するため、compiler-owned forwarding member のままにする
