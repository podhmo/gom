# `gom build` のパフォーマンス調査（pprof）

selfish 移植の流れで「`gom build` に明らかなチューニング対象があるか」を pprof で調べた記録。

## 計測方法

`gom` CLI にはプロファイル用フラグがないので、`cmd/gom/main.go` に `GOM_CPUPROFILE` 環境変数フックを一時的に差して `go tool pprof -top` で見た。`os.Exit` は defer を走らせないので `pprof.StopCPUProfile` は `run()` の戻り後に明示呼び出しする必要がある。

```go
func main() {
	if path := os.Getenv("GOM_CPUPROFILE"); path != "" {
		f, err := os.Create(path)
		if err == nil {
			pprof.StartCPUProfile(f)
			code := run(os.Args[1:], os.Stdout, os.Stderr)
			pprof.StopCPUProfile()
			os.Exit(code)
		}
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
```

syscall の内訳は wrapper gobin（`#!/bin/sh\necho x >> count\nexec go "$@"`）で subprocess 起動回数を数えて取った（exportcache_test.go の `countingGoBin` と同じ手法）。

## 計測結果

| 対象 | 時間 |
| --- | --- |
| `gom check` on `sandbox/selfish/gselfish`（~1200行, import go 16個, export cache warm） | 0.11s |
| `gom build` 全 `.gom`（108 dirs）一括, export cache warm | 3.15s |

- flat 1位は `Syscall6`（39%）= subprocess と stat/readdir の嵐。コンパイラ本体（syntax/check/lower/codegen）の関数で目立つ hotspot はなく、`CompileFiles` 累積で ~0.7s/3.15s
- `goimport` 周りが支配的: `exportCache.get` 33%、`go env` 等を含む `Loader.New` 系 22%、`gcimporter.Import`（export data の decode）25%

## 見つかったチューニング対象

1. **`exportCache.get` の dep dir 全走査**: hit 検証で import した全 package の `-deps` 全 dir に `srcStat` を実行。gselfish 1 build で **1308 calls / 208 unique dirs**（約6倍重複）、repo 全体で 5471 calls。→ **対応済み**: exportCache 単位で memoize（Loader 跨ぎで共有すると in-process の source 編集 invalidation が壊れるので cache 毎）
2. **`go env` subprocess が Loader 毎に走る**: repo 一括で 39 回。→ **対応済み**: gobin 解決+stat+`go env` を `goEnvSuffix` に括って process-wide memoize
3. **`go list -export -deps` が import 毎に逐次**: gselfish で 16 回 × ~80ms ≈ 1.3s。→ **対応済み**: `Loader.Prefetch` で `import go` 群を一括 `go list`（dep 帰属が取れないので entry の Deps は union で記録 — over-invalidate のみ）
4. **`gcimporter.Import` が module pool 毎に stdlib を decode し直す**: 25%。types.Package の process キャッシュは `*types.Named` identity が importer 毎に切れる問題があり、decision 2026-11-18 の却下案でも触れられている。open-questions の「LSP の Go import oracle の cache/再利用」と同根。→ 未対応

## 効果（`gom check` on gselfish）

| | before | after |
| --- | --- | --- |
| warm | 0.11s | 0.05s |
| cold（cache 空） | 0.86s | 0.17s |

## 残課題

- gcimporter の decode 共通化（4）: 構造的な問題なので別途検討
- `go list` batch は per-dir; module 内の複数 dir が同じ import を持つ場合、初回は dir 毎に prefetch が走る（warm 後は hit のみ）
