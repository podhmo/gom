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
  ・パッケージ名）を loader が持たない。要素が写せないときは
  `Go.Slice` 全体も `nil` を返す（boundary.go:137-140）ので、
  `bt == nil` で `GoSatisfies` が早期リターンしてしまい「要素だけ
  フォールバック」の形では届かない — **引数全体が unmappable の
  場合を先に構造判定へ分岐**する必要がある
- したがって `Rec` だけでなく、Gom 宣言型を要素に持つ
  `Go.Slice`/`Go.Map`/`Chan`/`List` の `~` 制約チェックは全て落ちる
- なお `~chan E` はそもそも `structuralConstraint`
  （mapping.go:359-379）が `~[]E`/`~map[K]V` しか認識せず、
  signature の読み込み時点で関数自体が使えない — 本 lane の対象外

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
- `cmp` 引数側 `func(a,b E) int` は `Go.func2`（Gom fn → Go 関数値
  の外向き変換）が担う — しかし `goFuncRawView`（gofunc.go:553）
  は record 等の Gom-only 型を raw view に写せないため、
  **宣言型を要素に持つ SortFunc の cmp 渡しは別の緩和が要る**。
  `~` 制約を通しても cmp が作れなければ SortFunc は完結しない —
  実装 lane は `goFuncRawView` の宣言型対応（または cmp 用の
  named-struct raw view）も同時に扱うべき

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

案 A を採用。型引数全体が unmappable（Gom 宣言要素を含む
`Go.Slice`/`Go.Map`/`List`）のとき、構造を Gom 側で分解して対応する
型引数どうしを `types.Equal` で比較する。`types.Equal` も false なら
従来どおり拒否（E0508）。

- `slices.Reverse rs` / `slices.SortFunc rs (Go.func2 f)` の `List Rec`
  経路: S への境界物質化 `Go.Slice Rec` が `~[]E` を通るようになる。
  境界コピーの意味論（callee が破壊的変更しても Gom 側に届かない —
  W0501 で警告済み）は変わらない。cmp 側は `Go.func2` の raw view が
  record を写せないため依然 E0510 系 — 上記の通り別の緩和が必要で、
  実装 lane に含める
- `slices.IsSorted`（`E cmp.Ordered`）: **複数フィールドの** record
  は struct emit で Ordered を満たさず正しく拒否される。ただし
  1 フィールド record（`type UserId = {v: Int}` → `type UserId int64`）
  は underlying 経由で `~int64` の Ordered を Go 的には満たすので、
  拒否側の例には struct emit される多フィールド record を使う
  （defined type 経由の受理可否は boundaryTypeOf が写せないため
  依然拒否になる — その差は gaps 台帳へ）

## 関連（延期バグへの材料）

旧候補 A（`Go.conv` の `List`→named slice 拒否）と同じ境界面。
`Go.conv list : B`（`type B []byte`）は境界コピー `[]byte` のあと
`B(copy)` キャストで実現可能だが、現行は E0507。本決定の
「Gom 宣言要素は Gom 側で同一性を見る」拡張があっても named slice
変換は別論点（変換自体の導線）なので、あのバグの処遇は別 lane で。

## 実装メモ

- `internal/goimport/constraint.go` `GoSatisfies`: `bt == nil`（引数
  全体が unmappable）のとき早期リターンせず「unproven」を区別して
  返す形状にする（現行は nil → false の一方向）。check 側で
  unproven かつ型引数が `Go.Slice`/`Go.Map`/`List` 構造なら要素を
  `types.Equal` で比較して受理/拒否する
- `internal/check/gofunc.go` `goFuncRawView`: `Go.func2` 等の外向き
  raw view に Gom 宣言型（emit される named struct / defined type）
  を通す緩和 — cmp コールバックが SortFunc で完結するために必要。
  emit 名の安定（同一パッケージで同名 emit）に依存するので、
  named 型の場合は emit 名と decl 名の対応を oracle 経由で確認する
  形が安全かは実装時に詰める
- `docs/implementation-gaps.md` へ record-slice 受理差分の項目追加
  （決定待ちでも台帳必須 — AGENTS.md）
- spec §8.5 の `~` 制約記述に「Gom 宣言要素は Gom 側の等価で判定」
  の一節を同期する
- テスト: `Go.Slice Rec`/`List Rec` の `slices.Reverse`/`SortFunc`
  （ok 系）、`Go.Slice Rec` に `E := Int`（拒否 E0508）、
  `Go.Map` の `~map[K]V`、named Go 型要素の既存経路の非退行
