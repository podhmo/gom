# nil の綴り — `Go.nil` ユニバーサル builtin

- 状態: 実装済み（2026-11-24 wave38 lane C）
- 日付: 2026-11-23

wave37 lane C（[prompts](../sketch/2026-11-23-wave37-prompts.md)）。出典は
open-questions: nil を書く面が `Ptr.nil` しかなく、`Go.Slice`/`Go.Map`/
Go 関数型の nil を作れない。sandbox 探索では `h.Sum nil` を `[]` で
代用していた（[sketch](../sketch/2026-11-23-sandbox-missing-features.md)）。

## 現状

- `Ptr.nil : Unit -> Ptr a` — 唯一の nil 生成面。`rt.PtrNil[T]()`
  （`func PtrNil[T any]() *T { return nil }`）への CallGo として
  emit される
- `errors.As` の lint は `Ptr.nil` リテラルを特別扱いする
  （internal/check/lint.go:294-333）

## 作れない nil

Gom 側で書けて Go 側が nil を持つ型のうち、生成面がないもの:

- `Go.Slice a` / `Go.Map k v` — nil slice/map。`Go.Slice` の面は
  read-only で nil でも get/length/fold が動き、`Go.Map` も
  get/delete/length は nil で動き `set` だけが panic（Go と同じ
  意味 — spec §8.4 の記述どおり）
- `Chan a` / `RecvChan` / `SendChan` — nil channel。select 腕の
  動的無効化（nil channel は永久にブロックする慣用句）に使う
- `Go.FuncN` — nil 関数値
- named Go 型（`types.Go`）— `http.HandlerFunc`、named slice、
  名前付き interface 型（`io.Reader` など）
- `Any` / `GoError` — nil interface / nil error

interface や pointer は `Option` で代替できる場面が多いが、Go API の
引数位置で明示的に nil を渡したい需要は残る。

## 選択肢

### 案 A: 型メンバーとしての `nil`（`Go.Slice.nil` 等）

`Ptr.nil`・`Go.Map.make` と同じ member table 系統 — `goSliceMembers`/
`goMapMembers`/`chanMembers` に `{name: "nil", goSym: ...}` を足すだけ。
届かない範囲: named Go 型（ぶら下げる module がない）、`Go.FuncN`、
`Any`。型ごとに nil の意味を散らす。

### 案 B: `Go.nil` ユニバーサル builtin（推薦）

`Go.conv`（env.go `isConv`）と同じ「compiler が持つ Go 名前空間の
builtin + check 側の専用制約」パターン:

- シグネチャ: `Go.nil : Unit -> a`（結果が rigid var — 呼び出し側の
  期待型で `a` が確定する。`let x = Go.nil` のように確定しない場合は
  通常の推論エラーで型注釈を要求する）
- 制約: 確定後の `a` が Go 側で nil を持つ型であることを check で
  検査し、違反は新しい診断コード（E05xx）で拒否。nilable 判定は
  `goRawNilable`（gofunc.go）が既に持つ: Con の
  `Ptr`/`Slice`/`Map`/`Chan`/`RecvChan`/`SendChan`/`GoError`/`Any`、
  `Go.FuncN`、および `types.Go` + `NilableUnderlying` oracle
- emit: `rt.Nil[T]()` 汎用 helper（`func Nil[T any]() T { var z T; return z }`）
  — `PtrNil` と同じ形でどの型にも均一に使える。裸の `nil` リテラルは
  `x := nil` が Go で非法なので helper 呼び出しに揃える

案 A は届かない型がある上に面が増える。案 B は面が 1 つで named Go 型
まで届き、`Go.conv` の先例がある。

### 案 C: `nil` リテラル新構文

構文を増やす割に gain がない。Gom は値の生成をモジュール/builtin の
面で揃えており、`nil` だけリテラルにする一貫性もない。不採用。

## 決定

案 B を採用。`Go.nil : Unit -> a` を Go 名前空間の builtin メンバー
として追加し、確定後の結果型に nilable 制約を課す。`Ptr.nil` は
既存のまま残す（`Ptr` モジュールの面として自然で、lint の特別扱いも
ある — `Go.nil` でも `Ptr a` の nil は書けるが旧来の綴りを潰さない）。

## 実装メモ

- `internal/resolve/scope.go`: `builtinGoMembers()` のハードコード表に
  `"nil"` を登録 — `Go` 名前空間は `builtinModuleTable` で cfg.Modules
  を見ないため、stdlib の member table 注入では届かない。シグネチャは
  `Go.conv` と同じく table 外の専用 typing が要る（`Unit -> a` の
  `a` が rigid で genericSig の表に載らない）
- `internal/check`: `isNil`（isConv 相当の識別子）を追加し、適用の
  結果型が確定した時点で nilable を検査。述語は `goRawNilable` を
  そのまま再利用しない — 同関数は `t.Name` だけで builtin の
  `Ptr`/`Chan`/`Slice`/`Map` を選ぶので、ユーザー宣言の同名型
  （Qual が自パッケージの `type Chan a` など）を誤って nilable と
  読む。`isBuiltinCon` 流の builtin 同一性（Qual == "" の builtin
  scope か `Go.` qual の型名）を条件に足した述語を新設する
  （FuncN seed の raw view 判定に特化した名前だが Con の判定自体は共通）
- `internal/lower`: `rt.Nil` への CallGo（`GoResultsOne`）を生成。
  TypeArgs に確定した `a` を置く
- `rt`: `Nil[T any]` helper を追加
- 診断コード: nil を持てない型への適用は新規 E05xx（E0507 系統の
  Go 境界制約の隣）。「nilable でない型 T には Go.nil を適用
  できない」
- eval: `rt.Nil` は eval 側でもゼロ値相当を返す必要があるか確認
  （PtrNil の扱いに揃える）
- lint の `Ptr.nil` 特別扱いを `Go.nil` の `Ptr` 適用にも広げるかは
  実装時に判断（errors.As 対象の検出精度）
- `Ptr.nil` との重複を gaps 台帳へ記録（2 面ある状態）
