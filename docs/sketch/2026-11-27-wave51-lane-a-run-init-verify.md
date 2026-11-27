# wave51 lane A: module 外 `gom run` / `gom init` 検証

## Red-first

実装前に `cmd/gom/run_test.go` へ次のテストを追加し、red を確認した。

- `TestRunOutsideModuleSynthesizesScratch`: `go: go.mod file not found` で失敗
- `TestRunOutsideModuleLocalDependency`: `gom.local/scratch/lib` が E0203 で解決できず失敗
- `TestInitCreatesTemplates` / `TestInitDoesNotOverwrite`: `gom init` 未実装で失敗

## 実装

- `gom run` は module のない run package directory に invocation-scoped な `go.mod` を `O_EXCL` で作り、終了時に削除する
- scratch module path は `gom.local/scratch` で固定し、子 directory の module-aware import を既存の module-local transitive emit に乗せる
- toolchain 依存は release build info / source checkout / `GOM_TOOLCHAIN_ROOT` から決める
- `gom init [-dir DIR] [MODULE-PATH]` を追加し、既存ファイルを残しながら `go.mod` と `main.gom` を生成する
- decision: [2026-11-27-module-external-run-init](../decisions/2026-11-27-module-external-run-init.md)

## 固定した振る舞い

| 項目 | 決定 |
| --- | --- |
| scratch module root | run package directory。親を推測しない |
| module-aware import | `gom.local/scratch` 配下のみ module-local。sibling project は `gom init` へ |
| 寿命 | 1 invocation。終了時に削除 |
| cache | gom 側に専用 cache を持たず、Go build/module cache に任せる |
| 既存ファイル | scratch/init ともに上書きしない。作成は `O_EXCL` |
| init template | `go.mod` + `fmt.Println("hello")` する `main.gom` |

## 検証

実装後の targeted test:

```text
go test ./cmd/gom -run 'TestRunOutsideModule|TestInit' -count=1
ok github.com/podhmo/gom/cmd/gom
```

最終確認:

```text
gofmt -l .  # empty
go vet ./...
go test ./...
```

いずれも成功。

対象:

- `TestRunOutsideModuleSynthesizesScratch`
- `TestRunOutsideModuleLocalDependency`
- `TestInitCreatesTemplates`
- `TestInitDoesNotOverwrite`
- `TestInitTemplateRuns`

## Review follow-up

- release module version を使う scratch/init では checksum が必要なため、合成・初期化直後に `go mod download github.com/podhmo/gom` を行うようにした
- 既存 `go.mod` に Gom 依存がない init では additive に `require`（開発版では `replace`）を足す。module 行と既存 require は書き換えない
- source checkout path の空白は go.mod quoted string にする
- 親 `go.work` があっても scratch workspace で invocation を閉じ、scratch 管理ファイルの mtime を固定して export cache の再生成を抑える
- 空の explicit module path は拒否する

追加検証: `TestModPathLiteralQuotesWhitespace`、`TestScratchModulePinsAndRemovesWorkspaceFiles`、`TestInitRejectsEmptyModulePath`、`TestInitExistingGoModuleAddsToolchain`。親 `go.work` が対象外 module だけを `use` する directory で手動実行し、scratch file が残らず exit 0 を確認した。

## 残論点

- scratch module では外部 dependency の追加を編集できない。依存管理が必要な project は `gom init` を使う
- scratch root はあくまで run package directory であり、複数 package project の恒久 module path とは別物である
