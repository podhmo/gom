# P2-4 Result パイプの局所境界の検証

- 日付: 2026-11-03
- Go: 1.27.3 darwin/arm64
- 決定: [result-pipe-local-boundary](../decisions/2026-11-03-result-pipe-local-boundary.md)

## 発見と prototype

既存 lower は spec 9.2 の Result.bind 意味に反し、失敗したパイプでユーザー関数から return していた。`Unit` 関数、トップレベル、パイプとエラー型が異なる周囲の関数では E0901 を生み、同じエラー型でも後続処理を飛ばした。さらに `1 |?> f` は数値型 meta を Result に固定しようとして E0401 となっていた。

通常経路を Unit 引数の即時適用クロージャへ変え、連続チェーンを単一境界で lower した。明示的 `try` のある式は決定木へ展開し、式を合成クロージャへ移動しないことで元の脱出先を保存した。新しい Core IR ノードや codegen による意味の判断は追加していない。

## 回帰テスト

- `TestEvalMatchesCompiled/pipe_result_value`: トップレベル、Unit、異なるエラー型の関数、失敗後のパイプ外継続、後段の `todo` が実行されないことを評価器と生成 Go で比較。
- `TestEvalMatchesCompiled/pipe_explicit_try_boundary`: RHS 引数の try はユーザー関数を脱出し、Err 入力では RHS の try 自体を実行しない。ラムダ内 try はそのラムダだけを脱出する。
- `TestPipeGoErrorChain`: Go から生成関数を呼び、errors.Is/As、元の NumError の同一性、wrap-once、try と既存 Err 入力の非ラップ、成功 payload、error-only Go 関数、Go 側ゼロ Result の panic を検証。無名ステップと `%` を含むファイル名も確認。
- `TestPipeTry` / `TestPipeTryBoundMeta`: EscapeResult の宛先が外側の関数でなく局所境界であることを検査。
- `TestPipeNamelessStepLineIndex`: 評価器へ UTF-8 ソースの LineIndex を渡し、無名ステップが byte offset ではなく行番号になることを検証。
- `testdata/spec/ok_result_pipe_value.*` と `testdata/golden/result_pipe.*`: 仕様の受理範囲と生成コードを固定。

## 制約とフォローアップ

明示的 try を含むパイプは、脱出先を保存するための保守的な決定木経路を使う。この経路の Go 呼び出しでは中間 Result が残る。通常チェーンの既知単項 Go ステップは raw `(T,error)` へ融合し、連続する成功段の間で Ok を再構築しない。残りの最適化は TODO P2-4b に分離した。

## 最終検証

`gofmt -l .` は出力なし、`go vet ./...` と `go test ./...` は成功。
決定 README は `go generate ./internal/tool/decisionsdoc` で再生成した。
Go 側の invalid Result は実行テスト済み。評価器にホストが渡す不正な
VVariant と Go のゼロタグの対応は、既存 problem の残る懸念として明記した。

## フレッシュレビューへの対応

指摘された `(strings.Count ("a", "a")) |?> next` の Go `int` → Gom `Int`
変換漏れは、このパイプ流入経路の責務なので修正が必要と判断した。
非 error Go 呼び出しの単一戻り値に、通常の `goCall` と同じ
`goPayloadConv` を適用した。複数戻り値は `wrapGoResult` の
`TupleFromGo` / `OptionFromGo` が既に要素変換を持つため、その経路を維持した。

監査用の実行回帰で、同じ経路の Unit 呼び出しが `tmp := runtime.Gosched()`
を生成する追加バグも確認した。これは呼び出しを discard 文として一度だけ
実行してから、明示的な Unit を次段へ渡すように修正した。一般の関数の
末尾で void Go 呼び出しを返す別経路は、この変更の対象外。

`TestPipeGoInflowConversions` は、トップレベルとローカルの `strings.Count`、
`utf8.DecodeRuneInString` の `(Int32, Int)` の要素変換、`strings.CutPrefix`
の `(String, Bool)`、`runtime.Gosched` の Unit を生成 Go の実行で検証する。
明示 try のあるパイプとブロックに包まれた Unit 流入も含め、
期待出力は `2/2/3/7/1/9/7`。spec fixture と
`result_pipe_go_inflow.golden.go` でも変換と Unit の文を固定した。
両バグは TODO に登録してから修正済みへ更新した。

レビュー対応後も `gofmt -l .` は出力なし、`go vet ./...`、`go test ./...`、
`git diff --check` はすべて成功した。
