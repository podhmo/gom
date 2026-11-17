# `gom build -go` 検証レポート

wave-14 lane 2。`docs/decisions/2026-11-17-cli-gotool-invocation.md` の prototype 検証。

## 実装

- `internal/build/gobuild.go`: `RunGoBuild(gobin, dir, hasMain)`。`package main`
  のとき `-o <tmpdir>/out` を付けて cwd にバイナリを吐かせない
  （go build は main パッケージの成果物を cwd に書く）。exit-status の失敗は
  `GoBuildRun.Failed`（`go test` と同じ契約）、ツールの起動自体の失敗のみ error。
- `cmd/gom/gobuild.go`: `runGoBuilds(inputs, results, failed, dsts, rep)`。
  書き出した入力を出力先ディレクトリ（`filepath.Abs(Dir(dst))`）でグループ化し、
  `runGoTests` と同じ GoPosIndex 構築（`AddSourceMap` に `Result.Output` の
  source map）で `go build` を実行、stderr を `GoToolDiagnostics`（tool=`"go build"`）
  で fold。`Failed` が false のときは fold しない — `GoToolDiagnostics` の
  zero-lines fallback は「ツールが失敗したのに parse できる行がない」の報告なので、
  成功時の静かな stderr を流すと偽の E0001 になる（prototype で実際に踏んだ）。
- `cmd/gom/cli.go`: `gom build -go` flag。既定は off — transpile-only 契約は不変。

## 壁時計（単一ファイル main package、/tmp/goe2e）

| 計測 | 5 回計 | 平均 |
| --- | --- | --- |
| `gom build` | 0.020s | ~4ms |
| `gom build -go` | 0.654s | ~131ms |

`go build` 起動 + モジュール解決 + コンパイルで ~127ms/build。opt-in なので
既定経路にはコストを載せない。CI / 明示的ビルドで払う値として妥当。

## e2e（実 CLI、/tmp/goe2e）

- クリーンな `package main`: `gom build -go` exit 0、診断なし、cwd にバイナリなし
  （`-o` で temp へ）。
- 生成物に手書きの `z.go`（`package main` + `//line m.gom:2` +
  `var _ int = "not-an-int"`）を混ぜる: exit 1、診断が
  `source:"go"` `data.tool:"go build"` span=`m.gom:2` に着地
  （go が報告する `./m.gom:2` を `x.file()` が srcs の入力に解決）。
- 同じ壊れた sibling を `-go` なしで走らせる: exit 0、診断なし（opt-in 確認）。

## テスト

`cmd/gom/gobuild_test.go`:
- `TestBuildGoFlagSilent` — クリーン pkg で exit 0・診断なし。
- `TestBuildGoFlagFoldsError` — sibling z.go の `//line m.gom:2` が
  `source:"go"` + m.gom span に fold される。
- `TestBuildGoFlagOff` — flag なしでは同じ壊れた sibling を無視。

ResolveMatch の drift/ambiguous の正直性は wave-13 の
`internal/build/gopos_test.go`（`TestResolveMatch`/`TestGoToolDiagnostics*`）が
既に pin 済み; このレーンはその fold を `go build` に繋いだだけ。

## 残件

- `go vet` 未起動（decision の範囲外、`vet:` 前置詞つき出力は raw text のまま）。
- LSP 側の実起動は per-change レイテンシ + 非同期 publish の設計が要るため
  open-questions/TODO に open として残す。
