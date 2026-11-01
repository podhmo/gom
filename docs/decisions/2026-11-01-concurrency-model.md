# 並行処理は `go` 式・チャネル型・`select` 式だけを言語に入れる

- 状態: 採用
- 日付: 2026-11-01

## 背景

goroutine とチャネルとのやり取りの方法が未定義だった。

## 決定

- goroutine とチャネルは Go のものをそのまま使う
- 言語に入れるのは、`go { e }` 式（型は `Unit`）、チャネル型 `Chan a` / `RecvChan a` / `SendChan a`、値を返す `select` 式の 3 つ
- 受信 `Chan.recv` は、チャネルが閉じていると `None` を返す
- `select` にも網羅性チェックをかける。同じチャネルからの受信腕は合わせて `Option a` を網羅しなければならない（閉じた場合の処理漏れをコンパイルエラーにする）
- 待ち合わせ用に標準ライブラリの `Task`（`spawn` / `await`）を用意する

## 影響

- Gom の値は不変なので、チャネルで値を送ってもデータ競合は起きない。可変な `Ref` / `Array` / `Ptr` を送る場合だけ、v1 ではリンターが警告する
- 送れる値を型で制限する仕組み（Rust の `Send` 相当）は future work
- `select` の網羅性判定は `match` と同じアルゴリズムを使う（`internal/pattern` を共用）

## 追記

- `select` の受信腕でパターンを書く記法と、同一チャネルの腕をまとめる規則は [select-receive-arms](2026-11-01-select-receive-arms.md) で置き換えた
