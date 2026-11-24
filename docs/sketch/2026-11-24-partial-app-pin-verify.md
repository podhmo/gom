# partial-app 残余パラメータ pin の検証

wave44 lane B。`fn pair (x: b) (y: b) -> String = "p"` に対する
`let g : Name -> String = pair "x"`（`type Name = { n: String }`）が
E0401 だった件の修正検証。

## 変更

`checkApply` の want-pin を残余パラメータ位置に拡大:

- `fillUsed` が供給引数のスロット集合を静的に模倣する（label 解決・
  重複・spread・Hole/`..` は失敗扱い）。成功したら残余型は
  `params[len(args):]` の近似ではなく `applyResidual`（宣言カリー
  分割を保つ真の残余）。
- pinParams が立つとき residual の param メタは `!inParam` と同じ
  規則で defined underlying want に pin される。

## 結果

- 再現: `gom check` で E0401「expected Name, got String」（修正前）
  → 修正後は ok。emit は `tmp1 := Name("x")` / `Pair[Name](tmp1, tmp2)`
  — リテラルが宣言型に materialize する。
- `pair ~y: "z"`（`~x:`/`~y:` 宣言版）は `~x: Name -> String` の
  residual — 位置型 `Name -> String` の want とはそもそも非互換
  （labeled param は labeled want が要る）で、lane の対象外。
- spec fixture: `testdata/spec/ok_scalar_lit_partial_pin.gom`（位置 fill）。
- 全チェック: gofmt / vet / check-sumtype / `go test ./...` 緑。
