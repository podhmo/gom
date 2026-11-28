# wave57 lane D3: `gom run` dependency `.gomi` cache 検証

- 日付: 2026-11-28
- lane: D3（小さい実装修正）

## 実施内容

dependency `.gomi` の compile-time bytes cache を process-global から compile invocation 所有へ変えた。`compileInputs` が cache を作り、`compileOptions` / interface check / diagnostic rendering へ渡す。`emitReport` は cache を借用するだけで解放せず、成功した `gom run` は generated code の compile・link・diagnostic rendering fallback がすべて終わった後、child process 開始前に cache を空にする。

## red-first

`TestRunReleasesGomiCacheBeforeChild` は `testdata/multifile` の dependency `.gomi` を読む successful run を使い、child 開始直前 hook で cache entry数を検査する。`gomi.clear()` を外すと `gom run child starts with 1 cached .gomi sources` で failし、戻すと pass した。

## fixture / test

- `TestRunReleasesGomiCacheBeforeChild`: successful `gom run` が child 開始前に dependency `.gomi` bytes を保持しない
- `TestDiskFallbackResolverUsesCompileTimeGomiBytes`: invocation-scoped cache に入れた exact bytes を disk fallback が使う非退行
- `TestEmitReportBorrowsGomiSourceCache`: report rendering が caller-owned cache を consume しない

## 検証

`gofmt -l .`（出力なし）、`go vet ./...`、`go test ./...`、`sh scripts/check-sumtype.sh` が通った。
