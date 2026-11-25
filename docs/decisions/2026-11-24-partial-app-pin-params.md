# 部分適用の残余パラメータも want に向けて pin する

- 状態: 採用
- 日付: 2026-11-24

## 決定

`checkApply` の want-pin（`Go.Any` / defined type を result のメタへ
反映する仕組み）を残余パラメータ位置にも拡げる — ただし供給済み
引数のスロット集合が静的に決まる（`fillUsed` が受理する）場合に
限る。

- `let g : Name -> String = pair "x"`（`pair : (b) -> (b) -> String`）
  は `b := Name` に pin され、供給リテラル `"x"` は `Name("x")` に
  materialize する。従来は positional-slice の近似 residual
  （`params[len(args):]`）が正しい slot 対応を持たない可能性がある
  ため param 位置の pin は禁止されていて、`?b := String` が先に
  commit して E0401 だった。
- `applyResidual`（宣言されたカリー分割を保った残余型）を使い、
  residual の param メタは result 位置と同じ規則（`containsGoAny`
  または defined underlying を持つ want）で pin する。制約付き
  メタ（num/ord/flo/intg/eq）は従来どおりスキップ。
- spread・`..`・Hole・未解決/重複ラベル・余剰 positional など
  静的に fill set が決まらない呼び出しは従来の positional-slice
  residual のままに退く。「param pin なし」は宣言型への pin の
  話 — `containsGoAny` 節による `Go.Any` への pin は本 decision
  以前から param 位置でも無条件に働いており、そこは変えない
  （literal 供給が `any` へ流れる既存経路）。
- 残余と want の矢印数が合わない場合（ret が未解決の generic
  var で want 側の矢印が多い等）、pin は共通 prefix の param を
  対にして残りを ret 同士に回す。以前は長さ一致のときだけ pin
  していて、`apply f "x"`（`f: a` 未解決）が `?b := String`
  に倒れて E0401 になっていた。

## 却下した案

- スカラーリテラルの meta bind を数値リテラルの pendingLit 相当に
  遅延する: arg の AST を bind 後に書き換える経路が要り、pipe 上の
  全 conform 経路に波及する。want 側を先に pin する方が局所的で、
  受理されるプログラムの集合は同じ。
- fill の決まらない場合でも pin する: positional-slice residual は
  実際の未供給 slot とずれることがあり、誤った slot への pin は
  供給引数を誤った型に retype する。

## 検証

- `testdata/spec/ok_scalar_lit_partial_pin.gom`
- [検証スケッチ](../sketch/2026-11-24-partial-app-pin-verify.md)
