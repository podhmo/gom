# 1 フィールド record の `~` 制約受理 — defined type の underlying 一致

- 状態: 採択（wave40 lane D の調査結果。実装は wave41 lane A）
- 日付: 2026-11-24

wave40 lane D（[prompts](../sketch/2026-11-24-wave40-prompts.md)）。
出典は implementation-gaps: `type UserId = {v: Int}` は Go で
`type UserId int64` の defined type なので `~int64` / `~int` を
満たすはずだが E0508 のまま。

## 現状と原因

### 再現

```gom
import go "github.com/podhmo/gom/testdata/goiface"
type UserId = {v: Int}
fn f (u : UserId) -> UserId = goiface.Clamp u u u   -- Clamp[T ~int]
```

→ E0508「type argument `UserId` does not satisfy the `~int`
constraint」。`Clamp u 0 100` の形では先にリテラル引数の
conform が E0401「expected a numeric type, got UserId」で落ちる
（こちらは別経路 — 後述）。

### 原因

`internal/goimport/constraint.go` の `GoSatisfies` が
`boundaryTypeOf` で型引数を go/types 型に写して判定するが、
`boundaryTypeOf` は **Gom 宣言型を写せない**（`l.named` は
`import go` した型しか持たず、宣言型は emit 側にしか材料がない）。
`bt == nil` → `GoVerdictUnproven` → checker 側 fallback
`goSatisfiesGomElems` が走るが、そこは `~[]E` / `~map[K]V` の
要素比較しか見ない — union term の `~int` は対象外で E0508。

### Go 側の正しい意味論

- `type UserId int64` は `~int`・`~int64` を**満たす**（approx
  term は underlying 一致で判定）
- ただし exact term `int` は**満たさない**（`types.Identical` では
  defined type は同名の unnamed 型と一致しない）— `Clamp[T int]`
  への `UserId` は Go でも拒否。フィールド型に丸めて判定するだけ
  では過剰受理になる
- 多フィールド record（struct emit）・sum（interface emit）は
  underlying が struct/interface なので `~int` に当たらず、
  現状の拒否は正しい — 対象は **defined type に emit される
  宣言型＝1 フィールド record** に限る

## 提案: `~` term 判定時に宣言型の emit 上 underlying を見る

`goSatisfiesGomElems` に union 項の分岐を足す: 型引数が 1 フィールド
record の宣言型なら、フィールド型を辿って boundary 表現を得て、

- `tm.Approx`（`~X` 項）: `boundaryType(field).Underlying()` と
  term の underlying が `types.Identical` なら Yes
- exact 項: 常に No（defined type は exact term を満たさない）

ネストした 1 フィールド record（`type A = {x: B}` かつ `B` が
1 フィールド record）は emit でも `type A B` → underlying は
再帰的に辿る必要があるので、フィールドが宣言型なら peel を繰り返す。

### 検討した他案

- **`boundaryTypeOf` で宣言型から go/types の `types.Named` を合成**:
  go/types で `types.NewNamed`＋`SetUnderlying` を組み立てれば
  `~` 判定・exact 判定の両方が go/types 側で自然に出る（`T` 項の
  `Identical(bt, tm.Type)` が defined-type セマンティクスをそのまま
  返す）。ただし goimport loader に gom 宣言の field 情報を届ける
  経路がない（checker → loader の依存方向しかない）。**推奨案**は
  checker 側で peel したうえで「synthetic named」相当の判定を
  自前で書く形 — 実質これと同じ結果を、依存方向を保ったまま得る
- **checker 側で targ をフィールド型に置き換えてから oracle に渡す**:
  却下。exact term を過剰受理する（上記）

## 隣接するが別のずれ（本 decision の範囲外）

- **リテラル引数の conform**: Go では `Clamp(u, 0, 100)` は untyped
  constant が `UserId` に代入可能でコンパイルが通るが、gom は
  `0` を `UserId` に conform させられず E0401。`~` 制約を通しても
  この呼び出し形は別途 literal→宣言型の代入可能性を扱う必要がある
  （implementation-gaps に追記する）
- **`comparable` 制約**: `type UserId int64` は Go でも comparable
  だが、checker 側の comparable 判定（`goReprComparable`）が宣言型を
  interface repr とみなす限り `T comparable` でも同種の拒否が起きる。
  lane E（sum の comparable panic）と同じ面 — 両者を同時に整理する
  余地がある

## 未実施

- 実装は採択後の実装 lane。判定経路・テスト面は上記のとおり —
  spec fixture（`Clamp u u u` が通ること・`Clamp[T int]` exact が
  依然 E0508 であること・ネスト record）と golden の type 引数 emit
  確認を含める
