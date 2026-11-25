# eval float32 幅対応 — prototype 検証

2026-11-25 / wave45 lane B。decision:
[2026-11-25-eval-float32-width-aware](../decisions/2026-11-25-eval-float32-width-aware.md)
の prototype 検証記録。

## 検証

- `float32_width_literal_rounding`（internal/build/compare_test.go の
  compareCases）: `SmallRatio` の位置の `16777217.0` と `16777216.0`
  が eval・compiled の両方で等しい（"same"）。
- `float32_width_arithmetic`: `16777216.0 + 1.0` が幅 32 の加算で
  吸収される（"absorbed"）— float32 精度の実行が compiled と一致。
- 負の証明 `TestFloat32WidthNegativeControl`
  （internal/build/float32_width_test.go）: oracle 未注入では両
  ケースとも発散する — ケースが幅機構を実際に踏むことの確認。
- oracle 配線: diff harness（evalDiff）と compare harness
  （evalAnswerFiles）に注入。loader はクエリ時にパッケージを
  preload する（cold だと interned named table に載らない）。

## 残る制約

- oracle 未注入の eval（eval_test.go 直利用など）は従来どおり
  幅 64 — eval はテスト oracle で、配線は harness の責務。
- ホスト境界から入る float32 値は reflect 経由で既に exact なので
  追加の丸めは不要。
