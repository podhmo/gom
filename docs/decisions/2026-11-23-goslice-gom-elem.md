# `Go.Slice`/`Go.Map` 要素に Gom 宣言型 — `~` 制約の Gom 側一致

- 状態: draft
- 日付: 2026-11-23

wave37 lane D（[prompts](../sketch/2026-11-23-wave37-prompts.md)）。出典は
open-questions: record が `[]MyRec` として `slices.SortFunc` 等に
渡せない。`List T`→`[]T` の境界コピーの意味論と一体の設計。

## 現状と原因

- `Go.Slice Rec`（`Rec` は Gom の record/sum 宣言型）自体は型式として
  綴れるし、`fn f (rs: Go.Slice Rec)` のシグネチャも通る
- 拒否されるのは generic Go 関数への**型引数**:
  `slices.Reverse[S ~[]E, E any]` に `List Rec`（S へ `Go.Slice Rec`
  が境界物質化する）や `Go.Slice Rec` を渡すと E0508 —
  `Go.Slice Rec` が `~[]E` を満たさない、と報告される
- 原因は `internal/goimport/constraint.go` の `GoSatisfies`: `~[]E` の
  要素判定を `types.Identical(s.Elem(), el)` — go/types の同一性 —
  で行うが、`boundaryTypeOf`（boundary.go:80）は Gom 宣言型を
  **写せない**。record/sum は import された型ではなく emit される型
  で、go/types 側に対応オブジェクトを構成する材料（フィールド並び
  ・パッケージ名）を loader が持たない → `bt == nil` → 常に拒否
- したがって `Rec` だけでなく、Gom 宣言型を要素に持つ
  `Go.Slice`/`Go.Map`/`Chan`/`List` の `~` 制約チェックは全て落ちる

## 選択肢

### 案 A: check 側で要素の Gom 一致を見る（推薦）

`~` 制約で oracle が要素を写せないとき、対応する型引数どうしを
Gom 側の `types.Equal` で比べる:

- `GoConstraintSlice`（`~[]E`）: 型引数が `Go.Slice X` で
  `types.Equal(X, targs[E])` なら受理。`List X` も同じ規則（List の
  トップレベル boundaryType は既に匿名 `[]T` で、`~` を満たす読みを
  維持したまま要素だけ Gom 側で比較する）
- `GoConstraintMap`（`~map[K]V`）も同様に K/V を `types.Equal` で比較
- 正しさの根拠: 1 つの生成パッケージでは等しい Gom 型は同じ Go 型に
  emit される。`slices.Reverse[[]Rec, Rec](rs)` の `Rec` は同じ宣言の
  同じ emit 名で、`types.Identical` に相当する事実を Gom 側で検査
  できる。named Go 型の要素は従来どおり oracle の `types.Identical`
  に任せる（そちらは写せる）
- `cmp` 引数側の `func(a,b E) int` は `Go.fn2` アダプタで既に通る
  （Go.FuncN 系統は別経路）

### 案 B: loader に Gom 宣言型の synthetic named を渡す

`listType`（boundary.go:206）が `list.List[A]` を合成しているのと同じ
手で、Gom 宣言型ごとに `types.NewNamed` を合成する — ただし loader に
宣言のフィールド並び（record → struct underlying）や識別子（decl の
symbol → 安定した同一名オブジェクト）を供給する新しい oracle 配線が
要る。正確だが境界面が 1 つ増える。案 A で同じ正しさが check 内の
局所判断で得られる。

### 案 C: `~` 制約では Gom 宣言要素を無条件に受理

正しさの保証がない — `Go.Slice Rec` に `E := Other` の組合せを通すと
emit される `[]Rec` / `Other` は Go 的に不整合。不可。

## 決定

案 A を採用。`GoSatisfies`（または check 側のフォールバック）で、
要素型が loader に写せない Gom 宣言型のとき対応型引数を `types.Equal`
で比較する。`types.Equal` も false なら従来どおり拒否（E0508）。

- `slices.Reverse rs` / `slices.SortFunc rs (Go.fn2 f)` の `List Rec`
  経路: S への境界物質化 `Go.Slice Rec` が `~[]E` を通るようになる。
  境界コピーの意味論（callee が破壊的変更しても Gom 側に届かない —
  W0501 で警告済み）は変わらない
- `slices.IsSorted`（`E cmp.Ordered`）は引き続き E0508 — record は
  Ordered を満たさない、正しい拒否

## 関連（延期バグへの材料）

旧候補 A（`Go.conv` の `List`→named slice 拒否）と同じ境界面。
`Go.conv list : B`（`type B []byte`）は境界コピー `[]byte` のあと
`B(copy)` キャストで実現可能だが、現行は E0507。本決定の
「Gom 宣言要素は Gom 側で同一性を見る」拡張があっても named slice
変換は別論点（変換自体の導線）なので、あのバグの処遇は別 lane で。

## 実装メモ

- `internal/goimport/constraint.go` `GoSatisfies`: `el == nil` のとき
  フォールバック呼び出し（checker 側で型引数どうしを `types.Equal`
  比較するか、`GoSatisfies` が「unproven」を区別して返す形状にする —
  現行は nil → false の一方向なので、unmappable と不適合の区別が要る）
- または `internal/check` 側で、oracle が false を返した場合に限り
  Gom 宣言要素かつ `types.Equal` 成立のケースを受理する
- spec §8.5 の `~` 制約記述に「Gom 宣言要素は Gom 側の等価で判定」
  の一節を同期する
- テスト: `Go.Slice Rec`/`List Rec` の `slices.Reverse`/`SortFunc`
  （ok 系）、`Go.Slice Rec` に `E := Int`（拒否 E0508）、
  `Go.Map` の `~map[K]V`、named Go 型要素の既存経路の非退行
