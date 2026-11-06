# Verification: stdlib `.gomi` type members

検証対象: [decision: stdlib-gomi-type-members](../decisions/2026-11-08-stdlib-gomi-type-members.md)
日付: 2026-11-08

## 何を確かめたか

同じ `.gomi` の type member が二つの経路で同一に解決されること:

- gomimport 経路 — `import "example.com/m/shapes"` で
  `shapes.Shape` / `shapes.Shape.Circle` が `p.Members` +
  `CtorsOf` + `TypeDecls` 経由で解決する。
- stdlib 経路 — builtin module table (`Config.Modules`) に
  `KindType`/`KindCtor` が入り、`pkg.Type.Ctor` が wrapped
  `Loader` の `CtorSource` seam 経由で解決する。

## 手順

1. `internal/stdlib` の `env` に gomimport と同形のテーブル
   （`tdecls` / `byCon` / `ctorFields` / `typeCtors`）を追加し、
   `MemberType` 分岐で `loadType` を呼ぶようにした。`load` を
   `loadPackage`（1 インタフェース分）に分け、`sync.OnceValues`
   化したままテストから合成パッケージを駆動できるようにした。
2. `Apply` を `env.apply` に分解し、`Sigs`（memberSigs =
   MapSource + TypeDecls）、`Loader`（ctorLoader wrap）、
   `Lower.Types`（typeOracle chain）を配線。
3. gomimport 側は unexported の `requalify` 等を stdlib 内に
   mirror（types.Type は 5 ケース、TypeBody は 4 ケースで固定）。
4. `TestLoadTypeMembers` — 同一 .gomi を両経路で読み、member
   kind・ctor テーブル・Signature（`types.Equal` で
   `Con{Qual: example.com/m/shapes}`）・`TypeDecls`・
   `ctorFields` の一致を assert。
5. `TestTypeMemberParity` — 同一プログラムを
   `List.Shape.Circle`（module 経路）と `shapes.Shape.Circle`
   （import 経路）で `build.Compile` し、生成 Go が同じ
   `shapes.Shape_Circle{` / `case shapes.Shape_*` を含むことを
   assert。

## 結果

- `go test ./internal/stdlib/` — 新規 2 テスト含め緑。
- `go test ./...` — 全 29 パッケージ緑、`ctorLoader` wrap /
  `Lower.Types` chain による既存呼び出しへの影響なし。

## 判明した制約

- モジュール名は resolve の builtin scope の固定集合
  （`List`/`Option`/`Result`/`Ref`/`Array`/`Chan`/`Task`/`Ptr`）
  で、`Config.Modules` は member table の注入のみ — 新しい
  モジュール名は stdlib 経路では作れない。これは意図通り
  （stdlib の module 名は spec 固定）。
