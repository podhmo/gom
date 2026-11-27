# wave52 lane C: 診断 coverage positive assertion 検証

- 日付: 2026-11-27
- lane: wave52 lane C
- decision: [診断 coverage positive assertion 規約](../decisions/2026-11-27-diagnostic-coverage-positive-assertions.md)

## red-first

`TestScanGoTestCodeEvidenceRequiresPositiveAssertion` を先に追加し、negative comparison・negative helper・無関係 literal が evidence 扱いされて失敗することを確認した。comment literal は従来どおり evidence にならないことを固定した。

## 実装

- scanner は例外表が指名した test 本体内で、承認済み positive assertion helper `requireCodes` への direct argument にある diagnostic code literal だけを evidence とする
- nested `t.Run` 内の direct call は evidence に含める
- E0606 の例外表 evidence test（`TestImplImportCycleMutual` / `TestIfaceImportCycleMutual`）を package-local `requireCodes` による positive assertion へ揃え、既存の data 検査は残した
- すべての Go test assertion の意味解析は入れていない

## 検証コマンド

```sh
go test ./internal/diag ./internal/gomimport -count=1
gofmt -l .
go vet ./...
go test ./...
```
