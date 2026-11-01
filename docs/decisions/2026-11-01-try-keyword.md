# 早期脱出は前置の `try`

- 状態: 採用
- 日付: 2026-11-01

## 背景

当初は Rust 風の後置 `?` を予定していた。並置の適用（[juxtaposition-and-currying](2026-11-01-juxtaposition-and-currying.md)）を採用した結果、`f x?` がどこに掛かるのか読みにくくなった。

## 決定

`try e` で、`e` が `Err` なら即座に外側の関数から `Err` を返す。

## 影響

- 表面言語に `return` はないが、生成コードでは `return` を使う
- `try` で `GoError` を自動ラップするかは未決（`|?>` は自動ラップする。[result-pipe-auto-wrap](2026-11-01-result-pipe-auto-wrap.md)）
