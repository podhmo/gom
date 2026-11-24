# `any` 引数 passthrough — 検証（wave44 lane A）

decisions/2026-11-24-goany-arg-passthrough の検証記録。

## 再現

```gom
fn wrap_err (e: GoError) -> GoError = fmt.Errorf("ctx: %w", e)
```

旧 emit: `tmp1 := any(e); fmt.Errorf("ctx: %w", tmp1)` →
`go vet` が `fmt.Errorf format %w has arg tmp1 of wrong type any`
を報告。新 emit: `fmt.Errorf("ctx: %w", e)`（tmp すら立たない）→
vet 通過。

## 変更

- `internal/lower/expr.go`
  - 引数 loop (`args` の atom 化) で `Ascribe{Conv: true}` が
    `Go.Any` 宛かつ中身が `goNativeArg` のとき Ascribe を剥がし
    `as.X` を atom 化（ChanDir の pass-through と同じ形）。
  - `convGoArg` に `isGoAny(pt) && goNativeArg(at)` → return a の
    early return（checker の Ascribe を通らない経路 — partial
    apply の hole 充填など — 用）。
  - `goNativeArg`: `*types.Go`、`*types.Func`、`Con{Qual:"Go"}`、
    `Con{Qual:"", Name: GoError|Ptr|Chan|RecvChan|SendChan|Scope}`。

## 却下した実装

- `goConvKindOf` / `ascribe` 側で identity を返す: declared
  `Go.Any` 位置（`let fun : Go.Any = fn ...`）の宣言型まで消えて
  `Go.try_as` が「is not an interface」で壊れる（c052 で検出）。
- codegen の `ConvBoxAny` emit で Go-native を裸出力: Conv ノードの
  `Typ` は `any` のまま残せるが、emitted 一時変数が `tmp := e`
  （`:=` で `error` 推論）になり、束縛経由では宣言型が伝わらず
  上と同じ失敗を起こす。arg 位置だけを狙うには lower が正。

## 回帰確認

- `testdata/golden/go_any.gom` に `wraperr`（`fmt.Errorf("wrap:
  %w", e)`）の pin を追加 — 生成は `fmt.Errorf("wrap: %w", e)`。
- 既存 golden の差分は Go-native 引数の `any(x)` 削除のみ
  （`fmt.Println(tmp2)` など）。`Int`/`String`/`42` の box は維持。
- TestEvalMatchesCompiled（`Go.try_as` を含む）・全テスト緑。
- `go vet` on the generated module — `%w` 警告なし。
