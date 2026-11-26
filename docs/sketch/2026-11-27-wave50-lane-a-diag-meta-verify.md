# wave50 lane A: 診断コード meta-test 検証

## 実施

- live code の coverage を phase 別に分類する meta-test を追加した。source-language diagnostic は `testdata/spec`、driver/interface diagnostic は CLI test または spec fixture、evaluator diagnostic は unit test を要求する
- fixture を source から直接作りにくい code は例外一覧で明示した。E0204/E0503 は production emitter がない reserved 扱い、E0502/E0605/E0606/E0704/E0901 は unit test 側の coverage とした
- `gom explain` example を executable / fragment に分類し、executable と判定した例は `package main` を足した完全なファイルとして実際にその診断を出すことを CLI test で検査する
- 不足していた source-level fixture として E0105 invalid byte string と E0602 `.gomi` value body を追加した
- W0601 は title/explanation どおり retired として `Retired: true` を付け、`docs/error-codes.md` を再生成した
- E0204/E0503 の「live code table にあるが emitter がない」ずれは implementation-gaps に登録した

## 結果

- `go test ./internal/diag -run 'TestLiveDiagnosticCoverageClassification|TestReservedDiagnosticsHaveNoEmitter' -count=1`: pass
- `go test ./cmd/gom -run 'TestExplainExamplesAreClassified|TestExecutableExplainExamplesProduceTheirDiagnostic' -count=1`: pass
- `go test ./internal/build -run TestSpec -count=1`: pass
- `go test ./...`: pass
- `go vet ./...` / `gofmt -l .` / `scripts/check-sumtype.sh`: pass

## 分類上の発見

- E0204 unknown attribute は現状 parser が E0101 を出し、AST が attribute を保持できるまで E0204 は emitter を持たない
- E0503 implicit numeric conversion も production emitter を持たない。どちらも explain/error-codes の説明と実装の間の未決定 gap なので、lane A では semantic を変えず reserved と明示した
- E0704 destructuring select receive arm は parser が先に E0101 を出すため、通常 source fixture ではなく checker recovery の unit coverage とした
- explain example の多くは周囲の型・関数宣言を省略した illustrative fragment であり、すべてを無条件に実行対象にはしない。実行可能と判定した 22 code だけを実実行 coverage に固定した

## 境界

- fragment example をすべて self-contained な例へ書き換える作業は lane A に入れていない
- E0204/E0503 を実装側へ寄せるか、文書・code table 側へ寄せるかは未決定のまま残す
- `docs/error-codes.md` と code table の一致は既存 `TestErrorCodesDocInSync` に任せ、この lane では重複実装しない
