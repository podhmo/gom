# `gom test --` passthrough — prototype verification

[decision draft](../decisions/2026-11-05-gom-test-go-test-flags.md)
の検証記録。`gom test` の引数列を最初の `--` で分割し、後段を
各入力ディレクトリの `go test -json` 呼び出しへそのまま渡す。

## 実装

- `runTest`（cmd/gom/test.go）が `slices.Index(args, "--")` で
  FlagSet に渡す前に分割する。FlagSet 自身は `--` を終端として
  飲み込むので、先に切らないと go test のフラグが入力パスと
  して解釈される（実際、分割前は `stat --: no such file` で
  exit 2 だった）。
- `build.RunGoTest(gobin, dir, args...)` — `go test -json <args>`
  の形で実行。`-json` は常に先頭。
- go test 側の失敗（未知フラグ・ストリーム破壊）は既存の
  E0001 `source: "go"` 経路に乗る。

## 検証

```
$ gom test r.gom -- -run TestBeta -count=1
→ "tests":{"run":1,...} — TestBeta だけが走った

$ gom test r.gom -- -race
→ run:2, 両テストが race ビルドで通過（CGO ありの環境）

$ gom test r.gom -- -bogus-flag
→ go test の flag エラーが stderr に落ち、
  E0001 "go test failed before running tests" として exit 1。
  gom 側の usage error（exit 2）にはならない。
```

`cmd/gom` の `TestTestGoFlagPassthrough` が `-run` 選択と
`-bogus-flag` → E0001 をカバーする。

## 確認した制約

- `-json` を潰す指定（`-json=false`）はストリームを壊し
  undecodable → E0001。spec 12 節に注意書きを入れた。
- passthrough は全ディレクトリに同じ引数で適用される —
  パッケージ個別のフラグ指定は出来ない（非目標）。
- 非フラグの passthrough 引数は `go test` の意味でパッケージ
  パターンになる。`./...` 等を渡すと gom の入力範囲外の
  パッケージも走る — verbatim 契約どおりの挙動として放置。
