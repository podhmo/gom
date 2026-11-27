# wave56 lane C: E0401 provenance 拡張の調査

- 日付: 2026-11-28
- lane: C（調査のみ）
- 対応 decision: [../decisions/2026-11-28-e0401-provenance-expansion.md](../decisions/2026-11-28-e0401-provenance-expansion.md)

## 調査方法

`internal/check/expr.go`・`expr_control.go`・`expr_literal.go`・`apply.go`・`env.go`・`typenv.go` を読み、`/tmp/provfix` 以下の一時 fixture を `gom check -format json` にかけて現行出力を確認した。repo の fixture や実装は変更していない。

## 現行確認 fixture

| fixture | 入力の形 | 現行の主要診断 | labels | 観察 |
| --- | --- | --- | --- | --- |
| `record_direct.gom` | `{ x = "bad" }` / `x: Int` | `type mismatch: expected Int, got String` | なし | monomorphic field は正しい provenance がある |
| `record_branch.gom` | `{ x = if c { 1 } else { "bad" } }` | `type mismatch: expected Int, got String` | なし | 腕へ同じ expected type が流れるなら provenance は正しい |
| `record_constraint.gom` | `{ x = 1 + "bad" }` | `expected a numeric type, got String` | なし | mismatch の expected type は field 宣言由来ではない |
| `call_direct.gom` | `fn takes (x: Int)` / `takes "bad"` | `type mismatch: expected Int, got String` | なし | 意味上は parameter 注釈が正しい provenance |
| `call_branch.gom` | `takes (if c { 1 } else { "bad" })` | `type mismatch: expected Int, got String` | なし | record field と同じ分岐構造 |
| `call_constraint.gom` | `takes (1 + "bad")` | `expected a numeric type, got String` | なし | callee 注釈へ漏らすと誤誘導 |

## 実装構造から分かったこと

- `checkExprAt` は `expectedFrom` を最終 conformance 中だけ `c.expectedFrom` に入れ、途中の nested diagnostic へは自動継承しない
- `checkIf` / `checkMatch` / `checkBlock` は、同じ `want` が腕または最終式へそのまま流れる場合だけ `expectedFrom` を伝える。join のため detached にした腕や bare `Go.Any` では落とす
- record field には `fieldInfo.sp` があり、`registerForeignTypes` は import `.gomi` の field span も保持する
- record literal では field 値を `c.checkExpr(f.Value, ft)` で調べるため、provenance は現状まったく渡らない
- 呼出し側の `check.Signature` は型・arity 情報を持つが parameter span を持たない。`checkGomApply` も `c.checkExpr(a.Value, pt)` で provenance を渡さない
- generic record では declaration が `v: a` なのに mismatch は substitution 後 `Int` に対して出る。field declaration を `expected type declared here` と呼ぶと事実として偽になる

## 結論

1. record field direct/block tail/if/match 腕は、type parameter を含まない exact declaration type に限り正しい
2. record field 内の独立 constraint mismatch へは伝えない
3. 呼出し引数は有用だが parameter span ownership を別設計するまで止める
4. generic record field は複合 provenance の設計が必要なので止める

実装候補は record field のみ。これを decision として固定した。lane C では実装しない。

## 残課題

- parameter span の表現と curried/flattened/labeled/deferred replay の index mapping
- generic type annotation と field declaration を両方示す複合 label の形式
- 実装 lane で red-first fixture を作り、text/JSON・import `.gomi`・誤誘導しない constraint negative を固定する
