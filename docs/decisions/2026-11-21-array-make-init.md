# `Array.make` は初期値を必須引数にする

- 状態: 採用（実装・検証した — `Array.make n init` が `[]A` に n 個の init コピーを emit し、`Array (Option T)` も `None` で初期化できる。stdlib/list の利用と testdata/spec/ok_array_ops を機械移行済み。spec.md §8.3 へ反映済み）
- 日付: 2026-11-21
- 問い: [implementation-gaps](../implementation-gaps.md)「`Array.make` の ADT/`Option` 要素ゼロ値」

## 問題

`Array.make n` は「n 個のゼロ値」を spec としていたが、生成される `[]Option[T]` 等のゼロ値は Go の `nil` であって `None` ではなく、`Array.get` が "not a declared variant" で panic する。ADT 要素型には正当なゼロ値が存在しない（linkcheck で実発生 — `Array (Option Outcome)` の timeout 経路、`--timeout 1ms` で再現）。

## 選択肢

- **(a) 初期値引数を必須にする(採用)**: `Array.make n init`。全要素型で一貫し、「ゼロ値が存在するか」を型で考えなくてよい。破壊的変更だが v1 前で利用者は stdlib 内部・examples・テスト fixture のみ — 機械移行で足りる。
- **(b) `Option` だけ `None` を生成**: 部分修正。生成側に ADT の分岐（`Option` は `None`、他の ADT 要素は診断拒否）が要り、`make` の意味が要素型で変わる。将来 ADT 要素を許す日が来ても (a) の API 形のほうが素直。**不採用**。
- **(c) 現状維持 + spec 明記 + lint**: 「ADT 要素不可」の制約を警告で運用する。panic するコードが書けるまま残るより、書けない API にするほうが安全。**不採用**。

## 実装

`rt.ArrayMake[A](n int64, init A) []A` が n 個の `init` コピーを返す（負の長さは Go と同じく panic）。`Array.make` のシグネチャは `Int -> a -> Array a`。`Array.set` で上書きする構築イディオムは `Array.make n <既定値>` と書く。
