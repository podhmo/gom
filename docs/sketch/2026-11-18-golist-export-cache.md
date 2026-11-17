# `go list -export` file cache — prototype 検証

日付: 2026-11-18
decision: [2026-11-18-golist-export-cache](../decisions/2026-11-18-golist-export-cache.md)（draft → 採用）

## 手順

1. ベースライン計測: `testdata/gocall` 相当の `gom check`（`import go` 3 件: fmt, strings, testdata/goiface）で `go list -export` の呼出回数と時間を `strace` 相当の wrapped gobin で計測 — 1 回あたり ~253ms（74/93/86ms）、`gom check` 全体 ~279ms の支配的コスト
2. file cache を実装（`internal/goimport/exportcache.go`）: key = package path + 実効 Dir + gobin identity(LookPath+size+mtime) + GOOS/GOARCH/GOFLAGS/GOWORK + go.work path/mtime + go.mod/go.sum mtime、entry = {export, dir, exportSize, exportMtime, srcMtime}、検証 = export stat + dir 直下 *.go の max mtime 再計算 + `-deps` の dep dir 再 stat
3. 単位検証（`exportcache_test.go`、wrapped gobin で subprocess 回数を計数）:
   - `TestExportCacheSecondLoaderSkipsGoList` — 2 台目の Loader は `go list` を 0 回呼ぶ
   - `TestExportCacheSourceEditInvalidates` — ソース編集（未来 mtime で確実に検出）で miss → 再取得
   - `TestExportCacheDepEditInvalidates` — dep のソース編集でも importer の entry が無効化される
   - `TestExportCacheDisabled` — `GOM_GOIMPORT_NOCACHE` で素通り
   - `TestGoExportRelativePath` — `Load("./foo")` 相当で `-deps` 最終行を root とみなす経路の pin
4. 実測: 2 回目の `gom check` で `go list` 0 回、~279ms → ~7ms（cache hit 時）

## 結果

- 採用に更新。LSP 側は compile 毎の Loader 作り直しでも同じ cache に乗るため keystroke 毎の subprocess が消えた（member table 共有は `*types.Named` identity の制約で open-questions に残す）
- レビューで見つかった dep 鮮度（依存 API は importer の .a に焼き込まれる）、go.work 自動発見の未 key 化、裸 gobin の identity 潰しを追補
