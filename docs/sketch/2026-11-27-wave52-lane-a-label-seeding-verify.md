# wave52 lane A: label-erased contextual seeding 検証

- 日付: 2026-11-27
- lane: wave52 lane A
- decision: [labeled lambda の record seeding / structural record](../decisions/2026-11-27-labeled-lambda-record-seeding.md)

## red-first

`testdata/spec/ok_record_context_labeled_callback.gom` を期待診断 0 件で追加し、実装前に実行した。`positional_labeled` は次の 3 件で red だった。

- E0401: `type mismatch: expected Acc, got Unit`（positional lambda 本体）
- E0407: `{ acc with count = acc.count + 1 }` が `Acc` / `Other` の同名 field set で ambiguous
- E0401: `Unit has no field or method count`

これは wave51 lane E の probe と同じ失敗形である。labeled callback 要求が最終 conformance では positional lambda を受け入れるにもかかわらず、推論では label 付き文脈を positional parameter へ使えていなかった。

## 実装した規則

- 完全に positional な lambda では、その lambda 自身が消費する expected parameter 位置の label だけを inference 用に落とし、位置順に対応させる
- 対応位置が record 型なら、これまでどおり注釈なし parameter をその record 型で seed する
- 残余 parameter の label は落とさない。残りは returned function の contract であり、`deep_label_pipe` の形が壊れないことを確認した
- explicit labeled lambda 向けには文脈型の label を残す。最終 conformance の unlabeled→labeled introduction も変えない
- speculative propagation の clone/rollback は変えない。label mismatch を推論上の不一致にせず、callee 側の label を authoritative にしながら parameter 型だけを merge する

## fixture / test

- positive fixture: `ok_record_context_labeled_callback` — `Acc` / `Other` の同名 field setを持つ環境で、labeled callback の positional lambda と explicit labeled lambda の両方が通る
- positive fixture 化: `ok_record_context_labeled_generic` — 旧 `E0407_record_context_labeled_generic` は、generic record field 内の explicit labeled lambda が新規則で正しく型付くようになったため empty diagnostics へ更新した
- unit test: `TestApplyParamExpectationsDoesNotBindInferenceState` を labeled callback 形へ強め、speculative trial が元の inference metaを bind せず clone/rollback することを固定した
- unit test: labeled context propagation、open leaf reification、label merge の parameter order を `apply_expectations_test.go` で固定した
- regression: `TestEvalMatchesCompiled/deep_label_pipe` で residual の label が残ることを確認した

## 検証コマンド

実装後に次を実行し、すべて成功した。

```sh
go test ./internal/build -run 'TestSpec/(ok_record_context_labeled_callback|ok_record_context_labeled_generic)|TestEvalMatchesCompiled/deep_label_pipe' -count=1
go test ./internal/check -run 'TestApplyParamExpectations|TestMergeContextualType' -count=1
go test ./...
```

あわせて `gofmt -l .`（空）と `go vet ./...` も成功した。

## 残課題

- structural record / anonymous record 型は導入しない（decision どおり）
- labeled callback の record seeding は positional lambda の推論を改善するが、named record のない ad hoc accumulator を新たに作れるようにはしない
