# 決定の記録

1 件 1 ファイル。決定を覆すときは元のファイルを消さず、新しいファイルの「Supersedes」で参照する。

## ファイル名

- 新しい決定は `YYYY-MM-DD-<slug>.md`。日付は作成日で、状態や内容を更新してもファイル名は変えない。並列の worktree で同時に足しても名前が衝突しないように、連番は使わない
- 同じ日付で slug がかぶったら slug を変える
- 以前は連番（[juxtaposition-and-currying](2026-11-01-juxtaposition-and-currying.md)〜[development-order](2026-11-01-development-order.md)）だったが、日付形式に改名した
- 一覧表への行の追加は squash merge のときに衝突し得るが、行を両方残せばよい

## 状態

実装中に生まれた決定は次の順で進める。

1. `draft`: 案と背景を書いた段階。まだ spec / design には反映しない
2. `prototype`: 案を検証するための prototype を作っている段階。検証の手順と結果は `docs/sketch/` にレポートとして書き、ここからリンクする
3. `採用` / `不採用`: 検証結果を踏まえて決着した状態。`採用` なら spec.md / design.md を更新し、作業を TODO.md に足す。`不採用` でもファイルは消さない

既存の「一部採用、一部未決」「未決」は、P0 の実験で決める決定に使っている。

## 一覧

| ファイル | 決定 |
| --- | --- |
| [juxtaposition-and-currying](2026-11-01-juxtaposition-and-currying.md) | 関数適用は並置 `f x y`、関数はカリー化 |
| [labeled-required-arguments](2026-11-01-labeled-required-arguments.md) | ラベル付き必須引数を採用し、v1 では制限付き |
| [try-keyword](2026-11-01-try-keyword.md) | 早期脱出は後置 `?` ではなく前置 `try` |
| [no-offside-rule-and-gom-fmt](2026-11-01-no-offside-rule-and-gom-fmt.md) | オフサイドルールを持たず、出力が一意な `gom fmt` を持つ |
| [go-calls-as-tuples](2026-11-01-go-calls-as-tuples.md) | Go 関数は Go の書き方で呼び、型は自動で取り込む（一部を [go-interop-boundary](2026-11-01-go-interop-boundary.md) で置き換え） |
| [cute-placeholder](2026-11-01-cute-placeholder.md) | Go 呼び出しの `_` プレースホルダ（`cute` 方式） |
| [fsharp-pipes](2026-11-01-fsharp-pipes.md) | F# 流のパイプと関数合成 |
| [result-pipe-auto-wrap](2026-11-01-result-pipe-auto-wrap.md) | `\|?>` と `%w` による自動ラップ |
| [concurrency-model](2026-11-01-concurrency-model.md) | 並行処理は `go` 式・チャネル型・`select` 式だけを言語に入れる（一部を [select-receive-arms](2026-11-01-select-receive-arms.md) で置き換え） |
| [json-diagnostics](2026-11-01-json-diagnostics.md) | コンパイラのメッセージは既定で JSON |
| [gomi-interface-files](2026-11-01-gomi-interface-files.md) | `.gomi` を採用し、ライブラリでは必須、`pub` は廃止 |
| [compiler-in-go](2026-11-01-compiler-in-go.md) | コンパイラは Go で実装 |
| [inline-tests](2026-11-01-inline-tests.md) | テストは実装と同じファイルに書く |
| [language-name](2026-11-01-language-name.md) | 言語名は Gom |
| [select-receive-arms](2026-11-01-select-receive-arms.md) | `select` の受信腕はチャネルごとに 1 つの `case` |
| [value-restriction](2026-11-01-value-restriction.md) | 一般化は value restriction 付き |
| [try-boundary](2026-11-01-try-boundary.md) | `try` の脱出先は最も内側の関数 |
| [value-representation](2026-11-01-value-representation.md) | 値の表現（P0 で決定） |
| [go-interop-boundary](2026-11-01-go-interop-boundary.md) | Go 相互運用の境界 |
| [function-representation](2026-11-01-function-representation.md) | 関数値の表現（P0 で決定） |
| [core-ir-invariants](2026-11-01-core-ir-invariants.md) | Core IR の不変条件 |
| [evaluation-order](2026-11-01-evaluation-order.md) | 評価順序 |
| [generated-code-goals](2026-11-01-generated-code-goals.md) | 生成 Go コードの位置づけ |
| [development-order](2026-11-01-development-order.md) | 開発順序を P0〜P5 に変える |
