# `gom test` でも `.gomi` 適合検査を走らせる

- 状態: 採用
- 日付: 2026-11-04

## Context

open-questions: 「`.gomi` 適合検査（`checkInterfaces`）を `gom check`/`build` 以外のサブコマンドでも走らせるか」（P4-8 レビュー指摘）。現状 `gom test`/`fix`/`info`/`dump` は `.gomi` 不整合のまま生成物を書く。[interface-failure-withholds-output](2026-11-05-interface-failure-withholds-output.md) は、契約を検証できないパッケージの生成物を `gom build` で書かないと決めた。

## Decision

`gom test` でも `checkInterfaces` を走らせる。`gom test` は生成物を書いて実行するので、`gom build` と同じ契約ゲートを通す（E0601/E0603 があるパッケージはテストを生成・実行しない）。

`info`（`.gomi` の下書き生成。不整合を前提に使う）と `dump`（デバッグ用の中間表現の表示）は対象外。`fix` は今回は決めない。

## Alternatives

- *build/check だけを契約ゲートとする*: `gom test` が通ったのに `gom build` が失敗する、という食い違いが起きる。
