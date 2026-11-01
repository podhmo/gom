# `let x = <int literal>` が Go の `int` で落ちる可能性

- 状態: 未解決
- 発見: 2026-11-02, P1-8 レビュー修正中に観察（再現未確定、fixture は経路を回避）

## 観察

`let x = 1`（Gom `Int` = `int64` のはず）を lowering → codegen した場合、生成コードが `x := 1` になり、Go の推論で `int` に落ちる可能性がある。型付き AST 上は `Int` でも、リテラルの emit が宣言型ではなく Go のデフォルトに従うなら、後続の `x` の使用（例: `int64` を期待する関数への適用）で型不一致になる。

実際に壊れるかは未検証 — 現行 fixture は bare int-literal bind を避けて書かれている。codegen が `Int` リテラルを常に `int64(...)` 相当で包むか、`let` の宣言型を使って明示するかは要確認。

## 対応方向

1. `internal/codegen` で `Int` 型のリテラルを `int64(1)` のように Conv する（`Conv` ノードが既に num 変換を持つ — lit emit 時に宣言型を見て包む）。
2. `internal/lower` 側で untyped-相当のリテラルに `Conv` を挿す（lower が確定する場所としては自然）。
3. 再現確認のために fixture に `let x = 1; return x` を追加して生成コードを確認する（まずこれ）。

## 影響範囲

`internal/lower`（Conv 挿入位置）、`internal/codegen`（lit emit、dest の宣言型の利用）、fixture goldens。
