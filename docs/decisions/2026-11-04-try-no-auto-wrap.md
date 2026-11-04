# `try` は `GoError` を自動ラップしない

- 状態: 採用
- 日付: 2026-11-04

## Context

open-questions: 「`try` でも `|?>` と同じく `GoError` を自動ラップするか（今は `try` は包まない）」。[result-pipe-auto-wrap](2026-11-01-result-pipe-auto-wrap.md) と [try-keyword](2026-11-01-try-keyword.md) が保留にしていた。

実測（`check_port` は `n <= 0` で `port must be > 0` を返す Gom 関数）:

| 入力 | `try strconv.Atoi(...)` → `try check_port n` | `\|?> strconv.Atoi \|?> check_port` |
|---|---|---|
| `"x"` | `strconv.Atoi: parsing "x": invalid syntax` | `strconv.Atoi: strconv.Atoi: parsing "x": invalid syntax` |
| `"0"` | `port must be > 0` | `check_port: port must be > 0` |

## Decision

`try` は包まない（現状維持）。`try e` は `match e { Err x -> Err x を返して脱出, Ok v -> v }` と同じ意味のままにする。

- `try` は素の早期脱出、`|?>` は Go 流に文脈を付けるパイプ、と役割を分ける
- `try` で包むと、`strconv` のように関数名をもともと含むエラーで、関数名の二重付けが `try` のたびに起きる
- `try r`（変数）や `try (f x |> g)` のステップ名を新たに決める必要がない
- 文脈が欲しい場合の文言の指定方法は、Future work の「`|?>` の自動ラップの文言を設定可能にする」で扱う

## Consequences

`|?>` で書いた処理を `try` のブロックに書き直すと、`Error()` の文字列が変わる（spec 9.2 はこの文字列を公開される振る舞いとしている）。書き換えは意味を保つが、文字列は保たない。
