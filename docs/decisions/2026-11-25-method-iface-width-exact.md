# @method の interface 適合判定を raw 幅で行う

- 状態: 採用
- 日付: 2026-11-25

## 問題

`int` / `int64` は Gom ビューではどちらも `Int` に畳まれる
（spec §8.3）。interface のメソッドシグネチャもこの写像で読む
ため、`@method` の適合判定（implementsIface）が幅を見られず、
両方向で壊れていた:

1. **間違った実装が通る**: `io.Seeker`（`Seek(int64, int)
   (int64, error)`）に対し全部 `Int` で書くと check は通るが、
   emit は `(int, int) (int, error)` — 生成 Go がコンパイル
   不一致。
2. **正しい実装が拒否される**: `(offset: Int64) (whence: Int)`
   と幅を正しく書き分けると、interface 側が `Int` に畳んだ
   シグネチャと types.Equal で不一致になり E0401。

emit 側の写像は既に幅を区別できる: method 境界では `Int` →
`int`、`Int64` → `int64`、`Int32` → `int32` と綴られる
（codegen methodType — `Int`→`int` は "interface-typical" の
選択、decisions/2026-11-21-go-iface-impl）。つまりユーザーは
正しいシグネチャを書ける。欠けているのは適合判定の幅比較だけ。

## 決定

適合判定を raw ビュー（`int` = `Go.Int`、`int64` = `Int` で
区別される側）で行う。

- goimport の InterfaceMethods は `lower.GoSig` が既に計算して
  いる raw ビュー（Params/ResTypes/Results/ResNilable）を
  InterfaceMethod に載せる。
- check 側は interface メソッドのシグネチャを raw ビューで
  再構成し、ユーザーのメソッドシグネチャを「境界綴り」に
  翻訳してから比較する:
  - `Int` → `Go.Int`（methodType が `int` と綴る対応）
  - `Int64` → `Int`（raw ビューで int64 は `Int`）
  - `List T` → `Go.Slice T`（methodType が `[]T` と綴る対応）
  - それ以外は構造を保ったまま再帰
- raw 情報を持たないソース（評価器のモック等）では従来の
  types.Equal 比較に退く。

## 却下した案

- **emit を interface に駆動する**: メソッドの emit は静的で
  複数 interface に適合しうるため、interface 側から綴りを
  決めることはできない（`io.Seeker` 自身が int64 と int を
  混ぜている）。検証はできるが適合のたびの emit 変更は不可能。
- **emit の `Int`→`int` 特殊ケースをやめ `int64` に統一し、
  `Go.Int` をユーザーに公開する**: 一貫性はあるが breaking
  change で、`int` を取る interface 向けの既存メソッドが全部
  書き換えになる。現行の綴り分け（`Int`/`Int64`/`Int32`）で
  全幅は表現できているので、今回は判定側だけ直す。

## 検証

prototype 検証: [docs/sketch/2026-11-25-method-iface-width-verify.md](../sketch/2026-11-25-method-iface-width-verify.md)。
正しい綴りが通り生成 Go が `io.Seeker` を満たすこと、間違った
綴りが E0401 で拒否されることを確認し採用。
