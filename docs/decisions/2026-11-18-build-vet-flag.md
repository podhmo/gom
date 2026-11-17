# `gom build -vet` — `go vet` の実起動経路

- 状態: 採用
- 日付: 2026-11-18

## 背景

design.md 2.2 は「`go build` / `go vet` のエラーと panic スタックを
`.gom` の位置へ戻す」と定めるが、実起動経路は `gom test`（go build
出力の fold）と `gom build -go`（[cli-gotool-invocation](
2026-11-17-cli-gotool-invocation.md)）、`gom lsp -go`
（[lsp-gotool-invocation](2026-11-17-lsp-gotool-invocation.md)）に
限られ、`go vet` を回す面はなかった（implementation-gaps「LSP 側の
`go build` / `go vet` 実起動経路」の残件）。wave18 lane4 の判定対象。

## 候補

- **`go vet` を `-go` に含める**: `-go` で build と vet の両方を走らせる。
  vet の起動コスト（パッケージの型検査を内蔵するので build と同程度）
  が `-go` の契約に黙って乗り、build だけ欲しい利用者に不要な時間を
  取る。
- **`gom build -vet`（採用）**: `-go` と並ぶ独立 opt-in。位置 fold の
  機構（GoPosIndex + `GoToolDiagnostics`）はそのまま使い回せる。
- **`gom run` の新設**: `go run` 相当の subcommand も gaps の同項に
  並んでいたが、実行形の設計（引数の扱い、workdir、成果物の置き場）
  が要るため本決定の範囲外 — open-questions に残す。

## 決定

`gom build -vet` を新設する。`-go` と同じ directory grouping で
`go vet`（`build.RunGoVet`）を走らせ、stderr の `file:line:col:` 行を
`GoToolDiagnostics`（tool=`"go vet"`）で `.gom` span へ fold する。

- `-go`・`-vet` は独立に指定でき、併用すると両方走る（build の後に
  vet）。
- `-vet` 時も incremental cache は `-go` と同じ理由で無効 — cache hit
  では vet に渡す生成物が無いため新規コンパイルを強制する。
- `vet:` 前置詞つきの catastrophic 出力と `# pkg` ヘッダ行は
  `file:line:col:` に parse されず、そのまま unpositioned な診断として
  通る — `go build` の unparseable 行と同じ扱い（parse の回線は
  `./` 前置詞を削って解決するので、vet の `./file.go:line:col:` 形は
  そのまま fold できる）。
- LSP 側の vet 起動は lsp-gotool-invocation の opt-in 機構を別途
  拡げる話なので範囲外。

## 検証

`cmd/gom/gobuild_test.go` の TestBuildVetFlagSilent /
TestBuildVetFlagFoldsFinding — clean パッケージでは無診断、
printf 形式違反を `//line` で `.gom` へ向けた sibling Go ファイルで
SourceGo + tool=`"go vet"` の fold を確認。
