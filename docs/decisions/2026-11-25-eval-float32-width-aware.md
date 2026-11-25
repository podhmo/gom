# eval の float 値を幅正規化する（float32 対応）

- 状態: 採用
- 日付: 2026-11-25

## 問題

eval の `VFloat` は float64 オンリーで、compiled の float32 比較と
分岐していた: `type SmallRatio float32` の位置で `16777217.0` は
compiled では float32 に丸まり `16777216.0` と等しいが、eval は
f64 のまま比較して異なると判定する（#226 で escalate、wave45 lane B
として採択）。

## 決定

整数幅の設計（decisions/2026-11-05-eval-int-width-aware-ops）を
float に拡張する:

- **canonical 不変条件**: 幅 32 の位置の `VFloat` は float32 像の
  float64 値（`float64(float32(f))`）を保持する。
- リテラルは構築時に丸める（`litValueTyped`）— パターンリテラルも
  同じ経路なので match の比較は正規化済み値どうしで一致する。
- 算術は被演算子の幅で実行する: 幅 32 では被演算子を float32 に
  落として（canonical なのでロスなし）float32 精度で計算し、
  Go の生成コードと一致する。
- 幅の判定は型を剥がして行う: 宣言型の1フィールド record は
  codegen の defined type チェーンと同様にフィールドへ剥がし、
  行き着いた named Go 型の underlying は oracle（loader の
  `GoUnderlyingKind`）に聞く。gom には float32 の builtin が
  存在しない（goimport は predeclared float32 を未公開）ので、
  幅 32 は named Go 型経由でのみ発生する。
- oracle は Evaluator のフック（`SetFloatWidthOracle`）として
  注入する。eval はテスト oracle なので配線は harness 側の
  責務 — 未注入なら従来どおり全て幅 64。

## 検証

[docs/sketch/2026-11-25-eval-float32-width-verify.md](../sketch/2026-11-25-eval-float32-width-verify.md)
