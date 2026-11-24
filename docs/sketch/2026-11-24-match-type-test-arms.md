# 調査: `Go.Any` への `match` 型テスト腕（wave39 lane E）

日付: 2026-11-24

## 目的

wave38 lane F の decision（[go-any-extraction](../decisions/2026-11-24-go-any-extraction.md)）
が棄却した第3案 — `Go.Any` scrutinee に型テスト腕を許す `match` 拡張 —
の設計空間を調べ、decision 草案へ落とす。単一取り出しは採択済みの
`Go.try_as` が担うので、ここで問うのは**多型ディスパッチ**（1 形で複数の
動的型へ分岐する）の提供形。

## 現状の制約

- `match` の網羅検査は Maranget の有用性アルゴリズム
  （internal/pattern、design.md 4.2）で、ctor 集合は型から閉じて
  得られる。`Go.Any` は開いた型 — 任意の Go 型が動的に入りうるので
  閉じた ctor 集合が存在せず、構造的網羅は原理的に不可能。
- 腕はパターン集合として扱われ順序を持たない（E0302 が到達不能を
  静的に指す）。Go の `switch x.(type)` は first-match で、
  subtype 包含による腕の食い合い（`case fmt.Stringer` が
  `case int` を隠す等）を動的意味論として許す。Gom `match` に同じ
  判定を持ち込むには Go の型包含 oracle が新規に要る。
- scrutinee の型が `Go.Any`（`interface{}` 相当）以外の named Go
  interface 型（`fmt.Stringer` など）も同じ開放性を持つ。
- `Go.Any` の値は nil を持ちうる。type switch では `case nil:` が
  受け皿になる。
- 現行 grammar: match 腕は `pat -> expr`（guard 付き可）。パターンに
  型アスクリプション形はない（式の `: T` 注釈はある）。
- checkMatch（internal/check/expr.go:2990）は腕パターンを scrutinee
  型に照合するだけで、型テストは今はどこにも存在しない。

## 設計空間

### 腕の形

- **A: 型アスクリプション様パターン `p : T`** — `match x {
  n: Int -> f n; s: String -> g s; _ -> h }`。既存の腕形の拡張で
  新しい式形を足さない。`x : Go.Any` のときだけ合法とすれば、
  sum scrutinee との意味衝突がない（`Int n` 形は ctor パターンと
  見分けがつかないので、名前が先の形が安全）。bound 変数は `T` で
  束縛される — Go の `case T:` が `v` を `T` に絞るのと同じ。
- **B: 専用式（`typeswitch` 的な別形）** — 多型ディスパッチ専用の
  新構文。`match` の意味論（網羅・順序非依存）と Go type switch の
  意味論（非網羅・順序依存）の混線を避けられるが、ユーザーが覚える
  構文が増える。機能差を表面に出したいならこれが正直。
- **C: 現状維持** — `match (Go.try_as x : Option Int) { Some n -> ..
  ; None -> match (Go.try_as x : Option String) ... }` の入れ子で
  書ける。腕数に比例して try_as が深くネストし、default 腕の位置が
  不自然になる。糖衣として見劣りするがゼロコスト。

### 網羅性の意味論

- 開いた型に対して `match` が式として値を返す以上、**`_` 腕を
  必須化**が自然（Go type switch の暗黙 default は `()` を返す訳に
  いかないので）。違反は E0301 の亜種か専用コード。
- 型テスト腕同士の重なりは 2 段構えが釣り合う:
  - **逐語の同一型の重複**（`n: Int` と `m: Int`）は構文的に検出して
    E0302 相当で拒否。
  - **subtype 包含による隠蔽**（interface ターゲット）を静的に
    指すには go/types の包含判定が要る — pattern 行列に乗らない
    新しい検査。実装時に入れるか、到達不能はユーザー責任として
    warning なしで許すかは decision に残す論点。
- nil: `_` が受け皿になるが、明示腕が欲しければ `case nil`
  相当の綴りが別途要る。最小形では `_` に寄せる。

### emit / eval

- lower → codegen は Go の `switch v := x.(type) { case T: ...;
  default: ... }` にほぼ一対一 — Maranget の decision tree を通さない
  専用経路。bound 変数は Go が `v` を絞るのと同じ仕組みに載る。
- eval は腕順に `Go.try_as` と同じ内部断言を試す逐語の実装。 ただし `inhabits` は型名比較（`error` 特別扱い）なので、named Go interface 腕（`fmt.Stringer` 等）は implements 判定の持ち込みが要る — codegen の Go case 一致と食い違う（decision 実装メモに記録）。
- 混在ルール: scrutinee が `Go.Any` 系のとき通常パターン腕は不可、
  sum scrutinee に型テスト腕は不可 — 交差する設計を切らない。

## まとめ

推奨は **案 A**（`p : T` 型テスト腕、`Go.Any` 系 scrutinee 限定、
`_` 腕必須、first-match 意味論、emit は Go type switch 直写し）。
残る実装時論点は subtype 包含への到達不能検査の有無と nil 腕の
綴り。decision: [2026-11-24-match-type-test-arms](../decisions/2026-11-24-match-type-test-arms.md)。
