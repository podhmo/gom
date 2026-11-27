# wave54 lane G: checker state grouping + file split 検証

- 日付: 2026-11-27
- lane: [wave54 prompts](2026-11-27-wave54-prompts.md) lane G（実装・振る舞い不変）

## 目的

`checker` の平らな field を責務ごとの部分構造体へ分け、`internal/check/expr.go` を構文種別・責務ごとのファイルへ分割する。公開診断、spec fixture、生成コード、lint 結果は変えない。

## 実施内容

### state grouping

`checker` に次の embedded state struct を導入した。既存の `c.tyCons` / `c.metas` / `c.pendingSels` などの参照形は promoted field として維持し、移動と意味変更を混ぜない。

- `typeTables`: 宣言型・ctor・record field・foreign record visibility・method table
- `inferenceState`: `metas`、`nextMeta`、`nextVar`
- `pipeState`: pipe speculative propagation と want failure sink
- `resultTaintState`: result taint memo / in-flight set
- `deferredState`: finalize で revisit する pending selector/apply/assign/iter/interface/division/generic/conversion/nil/literal queue

`methods` は deferred queue ではなく宣言型の lookup なので `typeTables` 側へ置いた。

### file split

`expr.go` は 4,774 行から 510 行へ減らし、次へ分割した。

| file | 行数 | 内容 |
| --- | ---: | --- |
| `expr.go` | 510 | dispatch、代入、conformance、ident/string interpolation |
| `expr_member.go` | 574 | member selection、field/method resolution、deferred revisit |
| `expr_conv.go` | 587 | ascription、Go conversion、slice/list conversion |
| `expr_litrange.go` | 309 | numeric literal range の deferred judgment |
| `expr_operator.go` | 862 | unary/binary、arithmetic、constant folding |
| `expr_pipe.go` | 360 | pipe callee/operand/call、`|?>` |
| `expr_lambda.go` | 202 | lambda checking と contextual parameter/result helpers |
| `expr_control.go` | 459 | if/match/block/let/try |
| `expr_goexpr.go` | 59 | `Go.*` expression |
| `expr_iter.go` | 325 | for/iterable inference |
| `expr_select_expr.go` | 168 | select/receive concurrency expression |
| `expr_literal.go` | 461 | list/tuple/record literal、interface revisit |

分割は declaration 単位の移動で、package と import は `goimports` / `gofmt` で整えた。spec fixture、golden、lint の期待ファイルは変更していない。

## 振る舞い不変の確認

- `gofmt -l .`: 出力なし
- `go vet ./...`: success
- `go test ./...`: success

## 残課題

- `check.go` 自体は 2,223 行あり、declaration registration / generalization / scope check は今回の file split 対象外のまま。今回は prompts どおり `expr.go` の分割に限定した
- lint 固有の独立 state は現状ほとんどなく、lint は typed AST・`typeTables`・`diags` を読む形だったため、新しい `lintState` は作らなかった
