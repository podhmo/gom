# `go list -export` の prefetch 一括化と key/srcStat の memoize

- 状態: 採用（実装・検証した — `TestPrefetchBatchesGoList`（2 import の miss が `go list` 1 回で温まる）。実測: `gom check` on sandbox/selfish で cold 0.86s → 0.17s、warm 0.11s → 0.05s）
- 日付: 2026-11-24
- 関連: docs/decisions/2026-11-18-golist-export-cache.md（この決定の上に乗る改良）

## 背景

`go list -export` の file cache で subprocess は消えたが、pprof で見ると残ったコストは全て toolchain 連携の syscall に集中していた:

- `exportCache.get` の hit 検証が entry の `-deps` 全 dir に `srcStat`（readdir + 全 `.go` stat）を実行する。stdlib 系の dep dir は import する package 毎に重複するため、gselfish 1 build で 1308 calls / 208 unique dirs（約 6 倍の重複）
- `newExportCache` が Loader 毎に `go env` subprocess を起動する（modKey 毎に Loader が作られるので repo 一括 build で 39 回）
- cache miss は `import go` clause 毎に逐次 `go list`（gselfish で 16 回 × ~80ms ≈ 1.3s）

## 設計

- **srcStat の memoize**: exportCache 上に `dir → (mtime, count)` の map を持つ。memo は cache 単位で process-global にしない — 新しい Loader は必ず source を読み直す必要がある（in-process の source 編集で invalidate するテスト `TestExportCacheSourceEditInvalidates` がこれを要求する）
- **`go env` + gobin 解決の memoize**: `pre` の gobin 由来部分（LookPath→abs→stat→`go env GOOS GOARCH GOFLAGS`）を `goEnvSuffix(gobin)` として `sync.Map` で process-wide に memoize する。dir/GOWORK/GOMOD 由来部分は従来通り Loader 毎。`go env -w` の mid-process 反映を諦めるが、build は toolchain が安定している前提で動くので問題ない
- **`Loader.Prefetch(paths)`**: compile 前に `import go` path 群をまとめて `go list -export -deps` に投げる。`go list` は複数 package 引数を取れるので miss N 件を subprocess 1 回で settle する。`-deps` の flat 出力は per-package の dep 帰属が取れないため、entry の Deps には batch 全体の union を記録する（over-invalidation のみで under はない）。canonical ImportPath が取れない relative path (`./foo`) は prefetch 対象外で従来の lazy path に任せる。`go list` が失敗しても lazy path が diagnostic を出すので prefetch は黙って諦める
- 呼び出し側: `compileDir` が既に dep graph 用に AST を parse しているので、そこで `imp.Go` decl を収集して `gl.Prefetch(goImports[dir])` する。.gomi の one-off compile も `ParseInterface` 経由で同様に prefetch する。parse error のある file は resolve 前に reject されるので prefetch 対象から外す（`go list` は `-mod=mod` 環境下では go.mod を触り得るため）

## Cache の寿命（sampling の粒度）

- `srcStat` memo: **exportCache 単位**（Loader ごと）。同じ Loader 内の再検証は省くが、新しい Loader は source を読み直す — in-process の source 編集が即座に invalidate される
- `goEnvSuffix` memo: **process-wide**（gobin ごと）。`go env -w` の mid-process 変更は拾わない
- on-disk export entry: 従来通り（`2026-11-18-golist-export-cache` 参照 — hit 検証は srcStat の再サンプリング）
