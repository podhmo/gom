# sum 型の `comparable` 制約 — interface 表現の実行時 panic 面

- 状態: draft（wave40 lane E の調査結果。採択時に実装 lane が生じる）
- 日付: 2026-11-24

wave40 lane E（[prompts](../sketch/2026-11-24-wave40-prompts.md)）。
wave38 lane D で `slices.Contains` 系の `~[]E` 制約が Gom 宣言型の
要素を受け入れるようになった副作用として、sum 型（sealed
interface 表現）を要素に持つ `Go.Slice <ADT>` への汎用 Go 呼び出しが
受理される — 実行時に `==` が panic しうる面を調査した。

## 現状と原因

### 再現

```gom
import go "slices"
type Shape = | Circle (r: Float) | Points (pts: List Int)

fn has (xs: Go.Slice Shape) (needle: Shape) -> Bool =
    slices.Contains xs needle
```

→ check 通過。emit は `slices.Contains([]Shape, needle)` — Go でも
コンパイルが通る（`Shape` は interface 型なので `comparable` を
満たす）。実行時、`Shape_Points` 同士の `==` が評価された時点で
`panic: runtime error: comparing uncomparable type
ex.Shape_Points`（payload の `list.List[int64]` は named slice =
non-comparable）。Go 仕様では dynamic type が一致する interface
値同士の比較で、その型が non-comparable なら panic する。

### 原因

`internal/check/unify.go` の `cmpWalk` — `comparable` 制約を生成
コードの表現で判定する。「interface 表現は常に comparable」という
Go の静的規則をそのまま写している:

- `kindSum` → 常に `true`（payload を見ない）
- `Option` → 常に `true`（`option.Option[T]` は interface）
- `GoError`・`Go.Any` → `true`（真の interface — 実装は未知）
- `*types.Go`（named interface など）→ `ComparableSource` oracle
  （`types.Comparable` — Go と同じく interface に `true`）

対照的に struct 表現（record・`Result`・`Task`・`Tuple`）は
フィールド要素まで見るため Go と完全に一致する — struct 型自体が
non-comparable なら Go 側でも `comparable` 制約を満たさず、生成
コードはコンパイルに落ちる（panic ではなく拒否で済む）。

つまり panic 面は **gom が emit する interface 表現のうち payload
が静的に列挙できるもの**（sum と Option）に限定される。`GoError`・
`Go.Any`・named Go interface は payload 集合が checker に見えず、
手書き Go と同じ残留リスクとして残る。

## 選択肢

### A. goReprComparable を narrow する（payload まで見る）— 推薦

checker が見える interface 表現について payload 判定に変える:

- `kindSum` → 全 variant の全フィールドが comparable のときのみ
  `true`（record の規則を variant 単位で畳み込む）
- `Option` → 要素型 `T` が comparable のときのみ `true`
  （`Option_Some[T]` struct と同じ判定）

- 効果: panic になる呼び出しが全部 check で落ちる（E0508 系）。
  全 payload が comparable な sum/Option は従来どおり受理。
- 代償: Go より厳しい。non-comparable payload を持つ ADT への
  `slices.Contains` などは書けなくなる — 回避は既存と同じ
  （要素ごとの `==` を避ける自前ループや `match` 分解）。
- `GoError`・`Go.Any`・named Go interface は変えない — 実装集合が
  見えないので Go と同じ残留リスクを受け入れる（spec に明記）。

### B. Go semantics のまま（panic リスクを文書化のみ）

受理のままにして、spec / guide に「interface 表現の要素比較は
non-comparable payload で panic しうる」と明記する。

- 利点: Go と完全 parity、`slices.Contains` 等が型集合を問わず
  書ける。コード変更ゼロ。
- 欠点: panic が gom 固有の ADT 表現（sealed interface + struct
  variant）から生じるため「Go の既知の振る舞い」とは言いにくい —
  checker が payload を見えているのに見なかったことにする設計。

### C. interface 表現を全て拒否

`kindSum`・`Option`・`GoError`・`Go.Any`・named interface を一律
`false` にする。

- 欠点: 全 payload comparable の安全なケース（`Option Int` の
  `slices.Contains` 等）まで死ぬ — 棄却。

## 推奨

**A**。panic は gom 自身の emit 選択（interface 表現）から生じ、
checker は payload を完全に列挙できる — 静的に潰せる panic を実行
時に残す理由がない。opaque な interface（`GoError`/`Go.Any`/named
Go interface）だけは実装集合が見えないため Go と同じ admission の
まま残し、その残留リスクは spec 8.5 に明記する。

### A の実装メモ

- `cmpWalk` の `kindSum` で `ti`（typeInfo）の variant 定義から
  payload 型を取り、各フィールドを `w.has` で畳む — record の
  `kindRecord` 分岐を variant 全体に一般化する形。再帰 sum は
  既存の `seen`/`bound` ガードで打ち切る。
- `Option` は現状の `"Option"` builtin 分岐で `t.Args[0]` を判定
  すればよい。
- 生成側は変更不要（check だけの話）。eval は Go と同じく interface
  比較なので eval/emit の意味差も生じない。
- spec 8.5 の `comparable` 節に「Gom 宣言型の interface 表現は
  payload が静的に comparable と分かるときのみ制約を満たす」旨と、
  opaque interface の残留リスクを追記。
- spec fixture: payload-comparable sum は受理、List payload を持つ
  sum / `Option (List Int)` は E0508。

## 付随して見つかった gap

- `Go.FuncN` 型引数は `cmpWalk` の `t.Qual == "Go"` default 分岐で
  `true` を返すが、`func(...)` 型は Go で non-comparable —
  `Go.Slice (Go.Func1 Int Int)` 等の引数を取る `comparable` 呼び出しを
  受理すると生成コードが `go build` に落ちる。小さいので台帳行として
  implementation-gaps に登録（修正は `default` 分岐で `FuncN` を
  `false` に振り分けるだけ）。
