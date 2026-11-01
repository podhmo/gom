# 決定の記録

1 件 1 ファイル。番号は追記のみで再利用しない。決定を覆すときは元のファイルを消さず、新しいファイルの「Supersedes」で参照する。

| 番号 | 決定 |
| --- | --- |
| [0001](0001-juxtaposition-and-currying.md) | 関数適用は並置 `f x y`、関数はカリー化 |
| [0002](0002-labeled-required-arguments.md) | ラベル付き必須引数を採用し、v1 では制限付き |
| [0003](0003-try-keyword.md) | 早期脱出は後置 `?` ではなく前置 `try` |
| [0004](0004-no-offside-rule-and-gom-fmt.md) | オフサイドルールを持たず、出力が一意な `gom fmt` を持つ |
| [0005](0005-go-calls-as-tuples.md) | Go 関数は Go の書き方で呼び、型は自動で取り込む（一部を 0019 で置き換え） |
| [0006](0006-cute-placeholder.md) | Go 呼び出しの `_` プレースホルダ（`cute` 方式） |
| [0007](0007-fsharp-pipes.md) | F# 流のパイプと関数合成 |
| [0008](0008-result-pipe-auto-wrap.md) | `\|?>` と `%w` による自動ラップ |
| [0009](0009-concurrency-model.md) | 並行処理は `go` 式・チャネル型・`select` 式だけを言語に入れる（一部を 0015 で置き換え） |
| [0010](0010-json-diagnostics.md) | コンパイラのメッセージは既定で JSON |
| [0011](0011-gomi-interface-files.md) | `.gomi` を採用し、ライブラリでは必須、`pub` は廃止 |
| [0012](0012-compiler-in-go.md) | コンパイラは Go で実装 |
| [0013](0013-inline-tests.md) | テストは実装と同じファイルに書く |
| [0014](0014-language-name.md) | 言語名は Gom |
| [0015](0015-select-receive-arms.md) | `select` の受信腕はチャネルごとに 1 つの `case` |
| [0016](0016-value-restriction.md) | 一般化は value restriction 付き |
| [0017](0017-try-boundary.md) | `try` の脱出先は最も内側の関数 |
| [0018](0018-value-representation.md) | 値の表現（一部未決、P0 で決定） |
| [0019](0019-go-interop-boundary.md) | Go 相互運用の境界 |
| [0020](0020-function-representation.md) | 関数値の表現（未決、P0 で決定） |
| [0021](0021-core-ir-invariants.md) | Core IR の不変条件 |
| [0022](0022-evaluation-order.md) | 評価順序 |
| [0023](0023-generated-code-goals.md) | 生成 Go コードの位置づけ |
| [0024](0024-development-order.md) | 開発順序を P0〜P5 に変える |
