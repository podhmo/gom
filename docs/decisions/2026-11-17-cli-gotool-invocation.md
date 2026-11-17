# go tool の実起動は `gom build -go`（CLI 面）

- 状態: 採用
- 日付: 2026-11-17

## 背景

design.md 2.2 は「`go build` / `go vet` のエラーと panic スタックを
`.gom` の位置へ戻す」と定める。wave-13 で逆引き機構
（`SourceMap.Candidates`/`ReportedLine` + `GoPosIndex.ResolveMatch` の
exact/drift/ambiguous 分類）と `GoToolDiagnostics`/`gom test` への fold が
着地したが、go tool を実際に起動する場所が未決定だった
（implementation-gaps「`go build`/`go vet` のエラーを `.gom` 位置へ戻す
経路」、TODO「LSP: go-tool 診断の取り込み」）。

## 候補

- **`gom lsp` の per-change 経路**: CheckOnly コンパイルでは生成物がないため
  temp pkg dir への codegen 付き compile + `go build` が必要。
  ~48–50ms の CheckOnly 再コンパイルに ~55–90ms/pkg/change が乗る見積で、
  非同期 publish/retract の設計も要る。
- **`gom build` 既定動作化**: transpile-only の契約を変えてしまう。
  go.mod のない使い方（editor 統合、単体試行）で必ず失敗するようになり、
  全ビルドが遅くなる。
- **`gom build -go`（採用）**: 生成物がすでにディスクに乗る面に opt-in で
  実起動を足す。`gom test` の runGoTests が確立した directory grouping と
  GoPosIndex 構築をそのまま使える。

## 決定

`gom build -go` を新設する。書き出し後、出力先ディレクトリごとに
`go build` を走らせ、stderr の `file:line:col: msg` を
`GoToolDiagnostics`（tool=`"go build"`）で `.gom` span へ fold する。
起動自体の失敗は E0001 / SourceGo の go-tool 診断として正直に報告する。

- `-o` がモジュール外を指した場合は特別扱いしない — `go build` の失敗が
  そのまま報告になる。
- `go vet` はこの決定の範囲外（`vet:` 前置詞つき出力は引き続き raw text
  扱いの残件）。LSP 側の実起動は per-change レイテンシと非同期 publish の
  設計を要するため別項として open に残す。

検証: `docs/sketch/2026-11-17-cli-gotool-verification.md`。
`gom build -go` は `gom build` に対し ~127ms/build の追加コスト（単一ファイル
main pkg、~4ms → ~131ms）で、`go build` の `file:line` エラーが `.gom` span に
fold されることを実 CLI で確認した。
