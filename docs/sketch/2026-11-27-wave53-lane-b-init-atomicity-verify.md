# wave53 lane B: `gom init` failure atomicity 検証

- 日付: 2026-11-27
- lane: [wave53 prompts](2026-11-27-wave53-prompts.md) lane B
- decision: [init-failure-atomicity](../decisions/2026-11-27-init-failure-atomicity.md)

## red-first

実装前に `cmd/gom/scratch_test.go` へ 2 つの test を足し、次を確認した。

```text
--- FAIL: TestInitNewModuleValidationFailureRollsBackCreatedFiles
    scratch_test.go:302: go.mod exists after failed init: <nil>
--- FAIL: TestInitMainWriteFailureRollsBackAllInitChanges/new_module
    scratch_test.go:333: go.mod exists after failed init: <nil>
--- FAIL: TestInitMainWriteFailureRollsBackAllInitChanges/existing_module
    failed init left go.mod modified
```

対象は prompts の採択どおり、validation failure で残る新規 `go.mod` / `go.sum`、および validation 後の `main.gom` write failure で残る新規作成 file / 既存 module 変更である。

## 実装後の固定内容

- 新規 init: release module の download が `go.sum` を書いた後に `stdlib/result` validation が失敗しても、`go.mod` / `go.sum` / `main.gom` は残らない
- 新規 init + 既存 `main.gom`: validation failure でもユーザー既存の `main.gom` は byte 単位で残り、init が作った `go.mod` / `go.sum` だけ消える
- 新規 init + `main.gom` write failure: validation に成功した後でも `go.mod` / `go.sum` / `main.gom` は残らない
- 既存 module + `main.gom` write failure: additive edit された `go.mod` は元の bytes へ戻り、既存の空 `go.sum` は空のまま残る
- 成功時だけ stdout の `created` / `kept existing` を出す
- backup は directory entry の存在を `Lstat` で見る。dangling symlink の `go.mod` を rollback が誤って消さないことを固定した

## 検証コマンド

```text
go test ./cmd/gom -run 'TestInit(NewModuleValidationFailureRollsBackCreatedFiles|MainWriteFailureRollsBackAllInitChanges)' -count=1
go test ./cmd/gom -run 'TestInit(NewModuleValidationFailureRollsBackCreatedFiles|MainWriteFailureRollsBackAllInitChanges|ExistingOlderGoModuleRollsBackDependency|CreatesTemplates|DoesNotOverwrite|FailureKeepsBrokenGoModSymlink)' -count=1
go test ./cmd/gom -count=1
```

両方 pass。最終検証は `gofmt -l .` empty、`go vet ./...` pass、`go test ./...` pass。

## 残論点

- directory 作成自体は prompts の採択どおり rollback 対象外のまま。project file atomicity のみを固定した
- write failure の inject は `runInitWithWriter` の test seam で行い、本番経路は従来どおり `writeNewFile` を使う
