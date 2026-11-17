# `go list -export` の process 越し file cache

- 状態: 採用（実装・検証した — `TestExportCacheSecondLoaderSkipsGoList`（2 台目の Loader は `go list` 0 回）・`TestExportCacheSourceEditInvalidates`（ソース編集で invalidation）・`TestExportCacheDisabled`（NOCACHE 素通り）。実測: `gom check` で `go list` 3 回 ~253ms → cache hit 時 ~7ms に）
- 日付: 2026-11-18
- 問い: open-questions「Loader の作り直し」/ wave17 lane 2

## 実測

`gom check`(3 つの `import go`: fmt, strings, testdata/goiface)1 回あたり `go list -export` は import した package 数と同じ 3 回、合計 ~253ms(74/93/86ms)。`gom check` 全体の ~279ms のうち subprocess が大半を占める。同一 process 内では `pkgs` map と go/types importer が既に dedup するので重複はなく、コストは全て process 越しの繰り返し。

LSP は compile 毎に `compileOptions` が新しい Loader を作るため、keystroke の度に同じ `go list` を回している — file cache はここにも無条件で効く(shared member table は `*types.Named` identity の制約で共有不可、open-questions 行き)。

## 設計

- 格納先: `os.UserCacheDir()/gom/goexport/<key>.json`(1 entry 1 file、書き込みは tmp+rename の atomic)。`GOM_GOIMPORT_CACHE_DIR` で差し替え、`GOM_GOIMPORT_NOCACHE` で無効化。cache が読めない・書けない時は常に素通り(compile を壊さない)
- key: package path + 実効 Dir + go binary の identity(`exec.LookPath` で PATH 解決した path + size/mtime — 裸の `"go"` を `filepath.Abs` するだけでは cwd 相対の存在しない名前になり全 toolchain が同じ identity になる)+ GOOS/GOARCH/GOFLAGS/GOWORK 値 + 有効な workspace file(GOWORK 指定、未指定時は Dir から上に辿る go.work 自動発見、`off` は発見自体を無効化)の path/mtime + Dir から上に辿った最寄り go.mod の path/mtime + go.sum mtime
- entry: `{export, dir(export 対象 package のソース dir), exportSize, exportMtime, srcMtime}` — `srcMtime` は dir 直下の `*.go` の max mtime(書込時に記録)
- 検証(hit 時): export file の stat(size+mtime 一致) + srcMtime の再計算が一致。Go build cache は content-addressed なので、ソース編集後は `go list` が別 path を返す — こちらは path が同じでも srcMtime が変われば miss するので古い .a を読まない。依存パッケージの export API は importer の .a に埋め込まれるため、依存のソース dir も `go list -export -deps` の同じ呼出で記録し hit 時に srcMtime を再検証する(-deps は出力を増やすだけで build コストは不増)
- `go list -export -f '{{.Export}}|{{.Dir}}'` の 1 回呼出で export path と dir を同時取得(呼出回数を増やさない)

## 却下案

- **export path の存在だけで検証**: build cache の古い entry は trim されるまで残るので、ローカル package の編集後に古い .a を掴む。不採用
- **`go env GOMOD`/`GOROOT` を key に使う**: subprocess がもう 1 本必要になり節約分を相殺する。go.mod walk と goBin の identity で代替
- **member table(resolve.Package)の共有**: `*types.Named` の identity が importer 毎に切れるため loader 越しに共有できない(LSP 向けの本筋の改善は open-questions「LSP の Go import oracle の cache/再利用」に残す)
