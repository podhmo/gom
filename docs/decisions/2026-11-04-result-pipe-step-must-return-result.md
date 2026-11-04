# `|?>` の右辺は `Result` を返す関数に限る

- 状態: 採用
- 日付: 2026-11-04

## Context

open-questions: 「`|?>` の右辺に `Result` を返さない関数も置けるようにするか（今は `Result.map` か `Ok` で包む必要がある）」。

実測:

| 書き方 | 結果 |
|---|---|
| `s \|> strings.TrimSpace \|?> strconv.Atoi` | 通る |
| `s \|> strings.Trim(_, " ") \|?> strconv.Atoi` | 通る |
| `s \|?> strconv.Atoi \|> dbl` | E0401（`expected Int, got Result Int GoError`） |
| `s \|?> strconv.Atoi \|> Result.map dbl` | 通る |

spec 9.2 の 7 項（最初の値が `Result` でなければ `Ok` で包む）があるので、最初の `|?>` より前の失敗しないステップは `|>` で並べられる。困るのは `|?>` より後ろの失敗しないステップだけで、そこでは流れている値が `Result` なので `|> Result.map f` と書けばよい。

## Decision

現状維持。`r |?> f` の `f` は `a -> Result b e` に限る（spec 9.2 の 1 項）。`Result` を返さない関数は、最初の `|?>` より前なら `|>` で、後ろなら `|> Result.map f` でつなぐ。`|?>` の後ろで値が `Result` に変わる以上、`Result.map` を書くのは自明で、Go 側でも同じく包む必要があるのは自然。

## Alternatives

- *戻り値の型を見て、`Result` 以外なら `map` として扱う*: 戻り値が型変数の多相関数（`id` など）で bind か map かが決まらない。誤ってエラー型の違う `Result` を返したステップが `Ok (Result …)` として黙って通る。
- *Go の関数に限って自動で `map` として扱う*: 曖昧さはないが、`|> Result.map f` で済む位置にしか効かない。
