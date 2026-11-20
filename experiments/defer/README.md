# defer 相当機能の実験

Go の `defer` が解いている問題を Gom の流儀でどう解くかを比べる実験。
Go の `defer` 構文の移植は目的にしない。レポートは
[docs/sketch/2026-11-21-defer-survey.md](../../docs/sketch/2026-11-21-defer-survey.md)、
採否待ちの案は
[docs/decisions/2026-11-21-scoped-cleanup.md](../../docs/decisions/2026-11-21-scoped-cleanup.md)。

| ディレクトリ | 内容 | 実物か |
| --- | --- | --- |
| `survey/` | Go AST で `defer` の置き場所・対象・named result 操作・recover を分類する CLI | Go |
| `fake/` | acquire / release をログに積む擬似リソース（file・mutex・tx・panic） | Go |
| `aloan/` | (A) 今日の Gom で書くローンパターン | `.gom` → 生成 `.go` |
| `dscope/` | (D) 明示的スコープハンドル `with_scope (fn s -> ...)` + `scope/` ランタイム | `.gom` → 生成 `.go` |
| `scope/` | (D) のランタイム（LIFO の cleanup スタック、Result 連携、panic 時の実行） | Go |
| `cgen/` | (E) (D) をコンパイラ組み込みにしたときの生成 Go の目標形（named result + `defer`） | 手書き Go |

`dscope/` と `cgen/` は同じシナリオテスト（ログ順・エラー文言）を共有し、
両方通る。`aloan/` のテストは panic 時にロックが漏れることを固定している。

```sh
# 集計（標準ライブラリ / テスト側）
go run ./survey $(go env GOROOT)/src
go run ./survey -tests $(go env GOROOT)/src
# .gom の再生成（各ディレクトリで）
(cd dscope && gom build scenarios.gom)
(cd aloan && gom build scenarios.gom)
go vet ./... && go test -race ./...
```

`dscope/extern.go` は `@extern fn with_scope*` の手書き実装で、`scope.Run` /
`scope.RunResult` へ委譲するだけ。
