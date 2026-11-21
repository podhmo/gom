# 1 フィールド record を Go の defined type として emit する

- 状態: 採用
- 日付: 2026-11-21

`type UserId = { value: Int }` のような 1 フィールド record は、これまで
`type UserId struct { Value int64 }` に emit していた。これを Go の
defined type `type UserId int64` に変え、Gom の単一フィールド record を
newtype の綴りとして使えるようにする（wave29 lane A、採択 (a)）。

## 生成規則

- 宣言: `type X F`（F はフィールドの Go 型）
- 構築 `{ value = v }`: `X(v)` — lower が `core.Conv{ConvNamed}` に下ろす
- 読み取り `x.value`: `F(x)` — `core.FieldGet{Unwrap: true}`
- 更新 `{ r with value = v }`: `X(v)` — 唯一のフィールドを置き換えるので
  copy-then-set と同じ意味を conversion で書く（lower が `core.Conv` に下ろす）
- `==` / `show` の record レシピの被演算子射影: `F(v)` —
  `EqField` / `ShowField` の `Unwrap` にフィールド型を載せる

## 例外

唯一のフィールドの型が裸の型パラメータのとき（`type Box a = { item: a }`）
は struct にフォールバックする — Go は defined type の underlying に型
パラメータそのものを許さない（`type Box[A any] A` はコンパイルエラー）。
フィールド型が型パラメータを「含む」合成型（`type Eq a = { lt: a -> a -> Bool }`
→ `type Eq[A any] func(A) func(A) bool`）は defined type のまま。

判定は宣言側（フィールドの宣言型が `*types.Var` か）で行い、
`check.DefinedField` に集約する。lower はそれを `l.typeD`（ローカル宣言）と
`TypeOracle.DefinedField`（`.gomi` 由来のインポート宣言）で解決する —
instantiate 済みフィールド型では判断できない（`Box Int` のフィールドは
`Int` に見えるが宣言は `a`）ので、オラクルの答えは宣言型ベースにする。

## 付随する変更

- discard 位置の `F(x)` / `X(v)` は Go の文として conversion を許さないため
  `_ =` 経由にする（`emitsConversion` が `core.Conv` と `Unwrap` FieldGet を
  拾う — 既存の named-Go-type Lit と同じ扱い）
- `.gomi` の宣言テキストは変わらない（Gom 側の言語表現は同一で、Go 表現の
  選択だけが変わる）
- `stdlib/cmp` の `Eq` / `Ord`（1 フィールド・関数型 record）は defined type
  になる — `Eq[A any] func(A) func(A) bool`。`FloatOrd.Lt x` は
  `(func(float64) func(float64) bool)(FloatOrd)(x)` に展開される

## 検証

- `testdata/golden/record_one_field.gom`: 構築・読み取り・`with`・`==`・
  record パターン分解・多相 Box（param フォールバックは poly_adts の既存
  `type Box a`）を網羅
- `go test ./...` 全緑（generated infer fuzz 含む — conversion-in-stmt の
  罠は fuzz seed 44 で捕捉して `emitsConversion` で解消）
