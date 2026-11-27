# wave53 lane A: callback result record propagation 検証

- 日付: 2026-11-27
- lane: [wave53 prompts](2026-11-27-wave53-prompts.md) lane A
- decision: [callback-result-record-propagation](../decisions/2026-11-27-callback-result-record-propagation.md)

## red-first

`testdata/spec/ok_record_context_pipe_callback_result.gom` を、同名 field set を持つ `Out` / `OtherOut` 付きで追加し、期待 `diagnostics: []` のまま実行した。実装前は次の 2 件で fail した。

```text
E0401 type mismatch: expected Out, got Unit
E0407 the record literal is ambiguous: 2 record types own these fields; annotate the type
```

これは wave52 lane D で登録した pipe 経由 `Result.map` lambda 内 record literal の gap を小さくした形である。

## 実装後の固定内容

- direct generic application: `wrap_lines (fn valid -> { count = 0, lines = valid }) lines` が戻り値 `Out` から callback result を推論する
- pipe: `lines |> List.traverse_result validate_line |> Result.map (fn valid -> { ... })` が `Result Out String` から callback result を推論する
- if / match: 分岐結果の裸 record literal も同じ期待型を使う
- unit test: speculative lookahead は callback result meta を bind せず、非結果位置の record literal は activation しない
- backpipe: `run_callback <| fn _ -> { ... }` と `Scope.run <| fn _ -> { ... }` でも、静的な戻り値が callback result へ届く
- boundary: `xs |> List.map (fn x -> { ... })` の結果を `Go.Slice Out` へ返す形では、producer 側の `List Out` を speculative に使い、最後の implicit boundary conversion を残す。`List Out` を返す通常形も固定する
- composite result: tuple 要素にある record literal も結果位置として辿り、`Go.Slice (Go.Any, Out)` への pipe で `Go.Any` の要素型 pin と record propagation が一緒に効く。逆方向に、producer が `Go.Slice` を返し pipe 結果の期待型が `List` の場合も境界変換前の producer 形から推論する。reverse boundary は spec fixture と unit test で固定し、通常適用が同じ retry を使わないことも併せて固定する。sibling parameter については `() |> pack "x" { ... }` が `List (Go.Any, Out)` を満たす fixture で固定する
- soft hint は pipe callee 自身が application のときだけ使い、block 内の無関係な application へ漏れない。boundary producer 形の retry も pipe 経路に限定し、通常の関数適用や関数値の戻り値の内側へは広げない
- `E0407_record_context_no_backward` は従来どおり E0407 + E0401。これは `let seed = { ... }` を後続使用から推論しない限界事例で、wave53 lane A の前向き propagation 対象外である

## 検証コマンド

```text
go test ./internal/build -run TestSpec/ok_record_context_pipe_callback_result -count=1
go test ./internal/check -run 'TestApplyParamExpectations(ActivatesCallbackResultRecord|IgnoresNonResultRecordInCallback)' -count=1
go test ./internal/check ./internal/build -count=1
```

いずれも pass。最終検証は `gofmt -l .` empty、`go vet ./...` pass、`go test ./...` pass。

## 残論点

- record 未解決に伴う二次 E0401 の一般的な抑制は入れない。backward inference が必要な `E0407_record_context_no_backward` で従来診断を維持し、診断 UX 側の残論点として記録する
- `experiments/dmmf-fit` の recommended form 更新は lane A の必須範囲に入れず、spec fixture で同一推論形を固定した
