# wave52 lane B: `gom init` standalone checksum 検証

- 日付: 2026-11-27
- lane: wave52 lane B
- decision: [`gom init` の standalone checksum](../decisions/2026-11-27-init-standalone-checksum.md)

## 実装した規則

`gom init` の toolchain dependency 解決を `downloadToolchainStandalone` へ切り替え、常に `GOWORK=off` で `go mod download github.com/podhmo/gom` を実行する。さらに generated code が import する `github.com/podhmo/gom/rt` / `github.com/podhmo/gom/stdlib/result` を同じ環境で `go list` し、local `replace` の参照先がない、または release artifact が toolchain package を持たない project を成功扱いにしない。caller の `go.work` や `go.work.sum` に解決を依存する project も成功扱いにしない。失敗時は release version を使うか project の `go.mod` に explicit な `replace` を書くよう促す。

module 外 `gom run` が使う scratch module 側の `downloadToolchain`、寿命、cache、workspace 無効化は変えていない。

## fixture / test

- `TestDownloadToolchainStandaloneIgnoresWorkspaceReplace`: parent `go.work` の local replace では通常解決が成功することを確認したうえで、standalone resolution が失敗し、エラーが `GOWORK=off`・release version・explicit replace を示すことを固定した
- `TestDownloadToolchainStandaloneReleaseModuleWritesSum`: file-based module proxy の release module `v0.0.1` を使い、既存 `go.sum` がある場合・ない場合の両方で init 先の `go.sum` に checksum が残ることを固定した
- `TestDownloadToolchainStandaloneRejectsMissingLocalReplace`: local `replace` 先がないとき `go mod download` だけでは検出できない失敗を toolchain package resolution が actionable error にすることを固定した
- `TestInitReleaseProjectBuildsOutsideWorkspaceReadonly`: parent workspace 内で release module を要求する project を init し、workspace 外で `GOFLAGS=-mod=readonly` の `gom run` が成功することを固定した
- `TestDownloadToolchainStandaloneDoesNotUpgradePinnedRelease`: pinned release に `stdlib/result` がなく newer release にあるとき、validation failure が `go.mod` を upgrade しないことを固定した
- `TestInitExistingOlderGoModuleRollsBackDependency`: `go 1.26` project で validation が失敗したとき、追加した dependency・`main.gom`・`go.sum` を残さないことを固定した
- 既存 `go.mod` への additive edit は `TestInitExistingGoModuleAddsToolchain`、replace-only の保持は `TestInitExistingGoModuleWithReplaceOnly`、init template の実行は `TestInitTemplateRuns` が従来どおり固定する

## 検証コマンド

```sh
go test ./cmd/gom -run 'TestInit|TestDownloadToolchainStandalone|TestScratchModule' -count=1
```

実施時点で成功。wave 完了条件の `gofmt -l .`・`go vet ./...`・`go test ./...` は PR 更新前に実行する。

## 残課題

- 実際の tagged release はまだ存在しないため、release module fixture は file-based module proxy で合成している。公開 release ができた後も contract は同じである
- standalone resolution に失敗した場合、作成済みの `go.mod` を残すかrollback するかは今回の範囲外。既存の非上書き契約を変えていない
