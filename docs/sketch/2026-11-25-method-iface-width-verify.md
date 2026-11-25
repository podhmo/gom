# @method 境界の幅比較 — prototype 検証

2026-11-25 / wave45 lane A。decision:
[2026-11-25-method-iface-width-exact](../decisions/2026-11-25-method-iface-width-exact.md)
の prototype 検証記録。

## 再現（修正前）

`io.Seeker` = `Seek(offset int64, whence int) (ret int64, err error)`。

- 正しい綴り `fn (c: Cursor) seek (offset: Int64) (whence: Int) -> Result Int64 GoError`
  → **E0401 で拒否**（gom 視点で int/int64 が `Int` に fold され、
  interface 側シグネチャと型一致しない）。
- 間違った綴り（全部 `Int`）→ check は通るが、emit される
  `func (c Cursor) Seek(offset, whence int) (int, error)` は
  io.Seeker を満たさず生成 Go がコンパイル不一致。

## 実装

- `check.InterfaceMethod` に `RawParams`/`RawResults`（raw 視点）
  を追加。`goimport` の `InterfaceMethods` が `mapSignature` の
  `lower.GoSig` から転記する。raw を持たないソースは従来の
  `types.Equal` 相当（gom 視点比較のみ）にフォールバック。
- conformance は二段:
  1. `methodViewEqual` — gom 視点の構造等価。葉位置の
     `Int`/`Int64` は相互 interchangeable（幅は raw 側で決める）。
  2. `methodWidthOk` — raw 位置ごとの幅検査。raw `int`（`Go.Int`）
     ならユーザー側は `Int`（emit `int`）、raw `int64`（raw 視点の
     `Int`）なら `Int64`（emit `int64`）。コンテナは要素へ再帰。
     結果は `Result T GoError` → payload と raw 先頭、Tuple →
     要素ごと、素の値 → raw 先頭に対応付け。

## 検証

- 正しい綴り: `testdata/spec/ok_method_iface_width.gom` — check 通過。
  生成コードを実ビルド: `func (c Cursor) Seek(offset int64, whence int) (int64, error)`
  が出て、`var s io.Seeker = Cursor(0)` の代入テストが Go 側で通る
  （/tmp/w45a/out で `go test` ok）。
- 間違った綴り: `testdata/spec/E0401_method_iface_width.gom` —
  E0401 で拒否。
- 境界外: raw 情報を持たないソース（eval テストのスタブ等）は
  従来の挙動のまま（後方互換）。
