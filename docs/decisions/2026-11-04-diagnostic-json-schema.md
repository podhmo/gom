# 診断 JSON は独自スキーマを維持する

- 状態: 採用
- 日付: 2026-11-04

## Context

open-questions: 「診断 JSON のスキーマ: 独自スキーマのままにするか、LSP の Diagnostic や SARIF に寄せるか」。spec 12 章で、JSON・text・LSP はどれも `internal/diag` の内部表現からの変換と決まっている。

## Decision

`gom` の JSON Lines 出力は現行の独自スキーマ（spec 12 章）のままにする。LSP は P4-6 で内部表現から変換する。SARIF が必要になったら別の出力形式として足す（Future work）。

現行スキーマに不足・過剰なフィールドがないかの棚卸しは、必要になったときに行う（急がない）。

## Alternatives

- *LSP Diagnostic / SARIF を JSON 出力そのものにする*: `summary` 行や `test` オブジェクトなど、CLI 固有の情報を表しにくい。変換層があるので、寄せる利点が小さい。
