# Gom 言語仕様（ドラフト）

この文書は Gom の**規範的な仕様**であり、利用者から見える振る舞いだけを書く。実装方針とその理由は [design.md](design.md)、未決事項は [open-questions.md](open-questions.md)、決定の経緯は [decisions/](decisions/) を参照。

Gom（拡張子 `.gom`）は、sum types・網羅的パターンマッチ・式指向を中核に据え、読める Go コードへトランスパイルする静的型付き言語。名前は Go＋ML の略であり、オランダ語の gom（ゴム・樹脂。日本語「ゴム」の語源）でもある。

---

## 1. 構文の基本と式指向

関数適用は ML / Haskell 流の並置 `f x y`、ブロックと制御構造は波括弧で書く。SML / OCaml の `let ... in` や `begin ... end`、Haskell の `$`・`where`・オフサイドルールは持たない。すべてが式であり、ブロック `{ ... }` の値は最後の式の値になる。

```
package shapes

import go "fmt"

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)
  | Empty

fn area (s: Shape) -> Float =
  match s {
    Circle r -> 3.14159 * r * r
    Rect w h -> w * h
    Empty -> 0.0
  }

fn main () -> Unit = {
  let shapes = [Circle 1.0, Rect 2.0 3.0]
  let total = shapes |> List.map area |> List.sum
  fmt.Println(total)
}
```

（`List.map` / `List.sum` は `stdlib/list` — コンパイラ組み込みモジュールとして実装済み — [TODO](../TODO.md) P4-8）

### 1.1 式指向の規則

1. ブロック `{ e1; e2; ...; en }` の値は `en`。改行は `;` と同じ扱い（改行の規則は 1.5 節）
2. 途中の式が `Unit` 以外を返したらコンパイルエラー。捨てるなら `ignore e` を明示する
3. `if c { a } else { b }` は式。`else` の省略は `a : Unit` の場合のみ許可
4. `let x = e` はブロック内で以降のスコープを導入する。再代入は不可（シャドーイングは可）
5. `let` の左辺は論駁不可能なパターンのみ（タプル・レコード分解、単一コンストラクタ型）。論駁可能なら `match` を使う
6. `return` / `break` / `continue` は持たない。早期脱出は `Result` と `try` 式で行う
7. ループは再帰が基本。自己末尾再帰は Go の `for` に変換することを保証する。糖衣として `for x in xs { ... } : Unit` を用意する
8. `fn` 宣言には `@tailrec` を付けられる。「この関数の自己再帰呼び出しはすべて末尾位置にある」ことをコンパイラに要求する属性で、守れない場合はエラー（E0902–E0905）になる。`let` には付けられない。検査は lowering 段階で行うので `gom build`（テストコードは `gom test`）が報告し、型・パターン検査までの `gom check`（12 節）は対象外
   - **末尾位置**とは、関数の結果を直接決める位置のこと。ブロック末尾の式、`if` / `match` / `select` の各分岐の結果、および最外の `|>` / `<|` の適用だけが該当する
   - オペランドや `let` 束縛の中、`for` の本体、`try`、`|?>` の適用、ネストした `fn` / ラムダの内側にある自己呼び出しは末尾位置ではない
   - `go { ... }` の中の自己呼び出しは例外で、新しいゴルーチンのスタックで実行されるため末尾位置として数えない（10 節）
   - `@tailrec` の関数を値として使うこと（`let g = f` や引数に渡すこと）はできない。別名経由の呼び出しは確認できないため（E0903）
   - `f -> g -> f` のような相互再帰は `@tailrec` では認めない。Go には相互再帰の末尾呼び出し最適化がないため（E0904）。パッケージレベルの `let` を経由する循環も同じく相互再帰とみなす — `let g = f` や `let (g, _) = (fn ..., ...)` のように関数値を保持する `let` を呼び出しに使う経路でも、反復ごとに本物の呼び出しが起きてスタックは増える
   - この相互再帰の検査が辿るのはコンパイラが静的に解決できる参照だけである。引数として受け取った関数値を呼び戻すような、呼び出し先を特定できない経路の再帰は `@tailrec` の保証の対象外とする
   - 変換可能な自己末尾呼び出し（末尾位置にある直接の自己呼び出し）がひとつもなければエラー（E0905）。再帰呼び出し自体がない場合、別名経由でしか呼ばれない場合、`go` の中だけで再帰する場合がこれにあたる。末尾位置にない直接呼び出しは E0902 で報告される

### 1.2 その他の構文

- 関数適用は並置 `f x y`。左結合で、どの二項演算子よりも強く結合する（`f x + g y` は `(f x) + (g y)`）
- フィールド・モジュールのアクセスは適用より強い: `f p.name` は `f (p.name)`、`List.map f xs`
- 負の数を引数に渡すときは括弧が必要: `f (-1)`
- ラムダ: `fn x -> x + 1`、複数引数は `fn a b -> a + b`。本体はできるだけ右まで伸びる
- 引数のない関数は `Unit` を取る: 定義は `fn main () -> Unit`、呼び出しは `main ()`
- タプル `(a, b)`、リスト `[1, 2, 3]`、レコード `{ name = "a", age = 3 }`、更新 `{ p with age = 4 }`
- リストの末尾要素は `..rest` で他のリストを接合できる: `[x, ..rest]` は `rest` を壊さずに先頭へ `x` を足した新しいリストを作る（コンス）
- コメント `//` と `/* */`、ドキュメントコメント `///`、文字列補間 `"hello ${name}"`

### 1.3 カリー化

Gom の関数はすべてカリー化され、`add 1` は `Int -> Int` の関数になる。部分適用のための専用構文は持たない（Go 関数の `_` プレースホルダは 8.2 節）。

### 1.4 パイプラインと関数合成

長い処理は、入れ子の括弧ではなくパイプで上から下へ流して書くのを基本にする。

| 演算子 | 意味 | 主な用途 |
| --- | --- | --- |
| `x \|> f` | `f x` | データを左から右へ流す。`x \|> f y` は `f y x` |
| `f <\| x` | `f x` | 最後の引数が長い式やラムダのとき、括弧を省く |
| `f >> g` | `fn x -> g (f x)` | 左から右への関数合成 |
| `g << f` | `fn x -> g (f x)` | 右から左への関数合成 |
| `r \|?> f` | 9.2 節 | 失敗し得る処理をつなぐ |

```
let total =
  orders
  |> List.filter is_paid
  |> List.map (fn o -> o.total)
  |> List.sum

let task = Task.spawn <| fn () -> {
  let body = try fetch url
  Ok (parse body)
}

let handle = parse >> validate >> render
```

（`List.filter` / `List.map` / `List.sum` は `stdlib/list` として、`Task.spawn` は並行処理と `Task`（P5-1）として実装済み）

- これらは二項演算子の中で最も優先順位が低く、左結合。`a + b |> f` は `f (a + b)`、`f <| a <| b` は `f a b`
- 標準ライブラリはデータを最後の引数に取るので、`|>` で自然につながる

### 1.5 改行の規則（インデントは意味を持たない）

改行の解釈はトークンだけで決まり、インデントは一切参照しない。

1. `( )` と `[ ]` の内側では、改行はただの空白
2. `{ }` ブロックの内側では、改行が式の区切りになる。ただし行末のトークンが式を終われないもの（二項演算子、`<|`、`=`、`->`、`,`、`(`、`~label:` など）なら次の行へ続く
3. 次の行の先頭が二項演算子（`|>`、`|?>`、`>>`、`+`、`&&` など）か `else` なら、前の行へ続く
4. 長い処理はパイプで行を分けるのが基本。パイプで書けない長い関数適用は括弧で囲めば複数行に分けられる

```
let total =
  shapes
  |> List.map area
  |> List.sum

let page =
  (render_page
    header
    body
    footer)
```

（`List.map` / `List.sum` は `stdlib/list` として実装済み — P4-8）

### 1.6 波括弧の曖昧さ

`if` の条件と `match` の対象の位置では、`{` が式を終わらせる（Rust と同じ規則）。その位置にレコードリテラルを置くときは括弧で囲む。

### 1.7 公式フォーマッタ `gom fmt`

`gom fmt` の出力は構文木とコメントだけで決まり、元の改行位置やインデントは引き継がない。設定項目は持たない。

- 行幅 100 文字に収まる式は 1 行にまとめ、超える式だけを決まった位置で折り返す
- インデントは 2 スペース。`match` の `->` や `=` の縦揃えはしない。行末コメントだけは連続する行で揃える
- 元のソースから引き継ぐのは、コメントの位置と、ブロック内の空行（連続する空行は 1 行にまとめる）だけ
- トップレベル定義の間は空行 1 行。`import` は並べ替えて 1 つにまとめる
- 冗長な括弧は外し、必要な括弧は付け直す
- 保証: `fmt(fmt(x)) = fmt(x)`（冪等性）、および `parse(fmt(x))` が `parse(x)` と同じ構文木になること

---

## 2. ラベル付き必須引数

引数名に `~` を付けると、呼び出し側でラベルの明示が必須になる。省略可能引数（OCaml の `?x`）は持たない。

```
fn transfer (from: Account) (~to: Account) (~amount: Money) -> Result Unit TxError = ...

transfer alice ~to: bob ~amount: 100
transfer alice ~amount: 100 ~to: bob    // ラベル付き同士は順不同
let to = bob
transfer alice ~to ~amount: 100         // パンニング: ~to は ~to: to の略
transfer alice bob 100                  // エラー
```

1. 位置引数は宣言順に渡す。ラベル付き引数は位置引数の前後どこに書いてもよく、互いに順不同
2. ラベルの後ろに置けるのは単純式 1 つ（識別子・リテラル・括弧式）。適用や演算は `~amount: (base * 2)` のように括弧で囲む
3. 位置引数をラベル付きの仮引数に充てることはできない。ラベルの欠落・重複・未知のラベルはすべてコンパイルエラー
4. 評価順は呼び出し側に書いた順（左から右）
5. 部分適用はラベル付きでも書ける。`transfer alice ~to: bob` の型は `~amount: Money -> Result Unit TxError`。ラベルを先に渡せば位置引数だけが残る: `List.map (transfer ~to: bob ~amount: 1) accounts`
6. sum type のバリアントでも同じ記法を使える: 定義は `| Transfer (~to: Account) (~amount: Money)`、構築は `Transfer ~to: bob ~amount: 100`
7. **パンニング**: 渡す値がラベルと同名の変数なら、`~to: to` を `~to` とだけ書ける。値を書かないラベルは、必ず同名の変数を参照する（その変数がなければ「未定義の変数 `to`」のエラー）。式や別名の変数を渡すときは `~to: x` と書く

### 2.1 型の扱い

- ラベルは関数型の一部: `Account -> ~to: Account -> ~amount: Money -> Result Unit TxError`。型の同値は、連続するラベル付き引数の並びをラベル名順に並べ替えてから比較する（連続していないラベル付き引数どうしは入れ替えない）
- ラベル付きの呼び出しは、その時点で関数の型が既知であることを要求する。型変数のままなら注釈を求めるエラーにする
- ラベル付き関数とラベルなし関数の暗黙変換はしない

これらの制限の緩和と、省略可能引数・デフォルト値は future work とする。

---

## 3. 型システム

Hindley–Milner（let 多相）を基礎とする。型はすべて名前的（nominal）で、構造的部分型は持たない。型の適用も並置で書く（`List Int`、`Result Unit TxError`、`Option (List Int)`）。型変数は小文字。

### 3.1 組み込み型

| Gom の型 | 説明 |
| --- | --- |
| `Int` / `Float` / `Bool` | 64 ビット整数 / 64 ビット浮動小数点 / 真偽値。`Int32` 等のサイズ付き型も提供 |
| `String` / `Char` / `Unit` | 文字列 / Unicode コードポイント / 単位型（値は `()`） |
| `(a, b)` | タプル |
| `List a` | 不変リスト |
| `Option a` / `Result a e` | 標準ライブラリで定義された通常の sum type |
| `a -> b` | 関数 |
| `Ref a` / `Array a` | 可変参照 / 可変配列（限定的な可変性） |
| `Chan a` / `RecvChan a` / `SendChan a` | チャネル（10 章） |
| `GoError` | Go の `error` |

A `type` declaration in a file may reuse a builtin name. In that file the unqualified name then denotes only the local declaration — the builtin is unreachable (shadowing applies per name, file-wide). Inside a file declaring `type Result`, `Result` is that ADT; constructs that specifically require the builtin `Result a e` — `try`, `|?>`, calls to Go functions whose signature contains `Result`, and a `test` body typed `Result Unit e` — report E0415. `List`, `Option`, `Int`, `Unit` and the other builtin names behave the same way and are compiled and evaluated as ordinary local ADTs. Qualified names such as `pkg.Result` or `Go.Int` are never shadowed.

### 3.2 sum types

```
type Option a =
  | Some a
  | None

type Expr =
  | Num Int
  | Add Expr Expr
  | Neg Expr
  | Let (name: String) (value: Expr) (body: Expr)
```

バリアントの引数は関数の引数と同じ書き方をする。型だけなら位置引数、`(name: T)` なら名前付き（パターンとフィールド参照で名前を使える）、`(~name: T)` ならラベル付き必須引数。コンストラクタはカリー化された関数でもあり、`List.map Num xs` のように渡せる。再帰型は許可する。

名前付きペイロードへのフィールド参照 `s.f` は、その名前を宣言するバリアントが sum type 内にちょうど 1 つのときに限り型検査を通り、そのバリアントを値が保持していることを実行時にアサートする。別のバリアントの値に対する読み取りは契約違反として panic する（8.6 節） — `s : Shape` に対する `s.r` は `s` が `Circle` 値ならペイロードを返し、`Rect` や `Empty` の値なら panic する。位置や同名の別フィールドへの読み替えは行わない。

レコード型は自分自身を値として内包できない（E0413）。フィールドはインライン配置されるので、`type Point = {x: Point}` や相互に含め合うレコードは無限の大きさを持つ。タプルや `Result`/`Task` のペイロードも同じくインラインで、それらを通る再帰もエラーになる。再帰が許されるのは間接参照を介するときだけで、関数型のフィールド（`{f: () -> Point}`）、sum type のバリアント、`List` / `Ref` / `Ptr` / `Array` / `Chan` / `Go.Slice` / `Go.Map` などのポインタ表現を通る再帰は合法。

### 3.3 型推論の範囲

- **value restriction**: トップレベルの定義は、右辺が関数（`fn` 定義・ラムダ）か構文的な値（リテラル、コンストラクタの適用、それらからなるタプル・リスト・レコード）のときだけ一般化する。`let r = Ref.new []` のような関数適用の結果は一般化しない（可変参照による型安全性の破れを防ぐ）
- ローカルの `let` は一般化しない。これは実装上の一時的な制限ではなく v1 の型規則とする（将来ローカル多相を入れる場合は relaxed value restriction を検討する）
- `let` の型注釈が新しい型変数を導入できるのは、一般化されるトップレベルの `let` だけ（その変数は束縛自体の型パラメータになる）。`fn` や型定義のシグネチャが宣言した変数は内側の注釈からも参照できるが、それ以外で型変数を書いた注釈は未束縛変数としてエラーになる
- 再帰呼び出しは型引数を成長させてはならない（E0416）。`fn wdepth (w: W a)` の本体で `wdepth` を `W (List a)` で呼ぶような、呼び出しのたびに型が大きくなる再帰（自己・相互を問わない）を許すと、生成される Go のジェネリクスは無限のインスタンシエーションを要求し、Go コンパイラが instantiation cycle として拒否する。型変数の入れ替えだけの再帰（`swap` が `swap2` を呼び `swap2` が `swap` を呼ぶ等）は合法。ADT のフィールドが `W (List a)` のような型で再帰すること自体は許可される — 制限されるのは関数呼び出しの型引数だけ
- 公開関数のシグネチャは `.gomi` に必ず書く（7 章）
- 算術演算子は組み込み数値型にのみオーバーロードし、型検査の最後で具体型に解決する。未解決なら注釈を求めるエラー
- 数値演算は演算子ごとに機械型で 1 回ずつ評価する。`Float` のリテラルは書かれた値に最も近い `float64`（IEEE 754 binary64）を表し、各演算子は被演算子の `float64` に IEEE 演算を適用した結果を返す — `869.32 * 38.29` は `33286.262800000004` になる。整数演算は 64 ビット（`Int32` は 32 ビット）の 2 の補数 wrap-around で、オーバーフローはエラーではなく値が回る。定数畳み込みは言語の意味に含めないので、`let a = 869.32` のあとの `a * 38.29` とリテラルのままの `869.32 * 38.29` は同じ値を返し、`0.1 + 0.2 == 0.3` は `false` になる
- `/` / `%` の右辺が定数のゼロのとき、その除算はコンパイルエラー（E0414）。ここで「定数」は数値リテラル・単項の `-`・それらからなる算術式・型の明示（ascription）で構成される式で、`let` 束縛の変数や関数呼び出しは含まない。判定は Go と同じ — 整数では被除数が定数でなくても `x / 0` はエラー（Go がその定数式を受理しない。実行時 panic しか起き得ないため）。`Float` では `x / 0.0` のように被除数が定数でなければ IEEE の除算として合法（`+Inf` / `-Inf` / `NaN`）だが、`10.0 / 0.0` のように式全体が定数ならエラーになる
- `==` / `!=` / `assert_eq` は関数を含まない型にのみ使える構造的等価（同じ判定を共有し、違反は E0409）。型変数に対する `==` は v1 では使えない。判定は名前付き型の宣言を展開する — レコードのフィールド・ADT のペイロード・エイリアス・型引数で具現化したジェネリクスを辿るので、`type Box = {f: Int -> Int}` の `x == x` は関数名を書かなくてもエラーになる。等価の読みは次の通り（`internal/eval` の `eqValue` と生成コードは同じ答えを出す）:
  - スカラー・`Ref`・`Ptr`・チャネルは Go の `==` と同じ（`Float` は IEEE なので NaN は自分自身とも等しくない。`Ref`/`Ptr`/チャネルは同一性）
  - `List` / タプル / レコードは要素・フィールドごとの等価。空リストと nil は等しい
  - ADT は同じバリアントかつペイロードが等しいときに等しい。`Result` は `Ok`/`Err` のタグと中身
  - `GoError` はエラーメッセージ文字列の等価
  - 名前付き Go 型・`Task`・`Go.Any` のように Gom が表現を握らない型は、ランタイムの反射比較を使う（表現の中に上の読みと食い違う答えが残りうる — 実装ノートは `rt.Equal` と design.md 4.9）
- `show` / `compare` は v1 では型ごとの関数で提供する

---

## 4. パターンマッチと網羅性

`match` は網羅的でなければコンパイルエラー、到達不能な腕（冗長な腕）もエラーとする。エラーには具体的な反例を表示する。

```
fn eval (e: Expr) -> Int =
  match e {
    Num n -> n
    Add (Num 0) r -> eval r
    Add l r -> eval l + eval r
    Neg (Neg x) -> eval x
    Neg x if is_zero x -> 0
    // Neg x の一般ケースと Let が欠けている
  }
```

```
error[E0301]: non-exhaustive match on `Expr`
  --> eval.gom:2:3
   |  patterns not covered:
   |    Neg (Num _)
   |    Neg (Add _ _)
   |    Neg (Let _ _ _)
   |    Let _ _ _
   = note: arms with guards do not count toward exhaustiveness
```

（上は `--format text` の表示。既定の出力形式は 12 章の JSON）

### 4.1 パターンの種類

| パターン | 例 | 備考 |
| --- | --- | --- |
| ワイルドカード / 変数 | `_`, `x` | |
| リテラル | `0`, `"ok"`, `'a'`, `true` | 無限型のリテラルは `_` が必須 |
| コンストラクタ | `Some x`, `Add (Num 0) r` | ネストしたパターンは括弧で囲む |
| 名前付きフィールド | `Let { name = n, .. }` | `..` で残りを省略 |
| タプル / レコード | `(a, b)`, `{ name, age = 0 }` | レコードは単一コンストラクタとして扱う |
| リスト | `[]`, `[x]`, `[x, ..rest]` | Nil / Cons 相当の 2 コンストラクタとして扱う |
| or パターン | `Circle _ \| Empty` | 全選択肢で同じ変数集合・同じ型を束縛すること |
| as 束縛 | `Add _ _ as whole` | |
| ガード | `p if cond` | 網羅性判定ではその腕を無視する |

ガード付きの腕は、網羅性判定でも冗長性判定でも「その値を覆う」とは見なさない。たとえば `Neg x if is_zero x` の後の `Neg x` は冗長ではない。

同じ網羅性判定を、`let` の左辺（論駁不可能性）と関数引数パターンにも使う。

網羅性が保証するのは、Gom の中で作られた値がいずれかの腕に当たることである。Go から渡された不正な値（ADT 型の `nil` など）は契約違反として扱い、`match` の時点で型名を含むメッセージで panic する（8.6 節）。

---

## 5. モジュールと可視性

1 ディレクトリ = 1 Gom パッケージ = 1 Go パッケージ。Go の `go.mod` とビルドツールをそのまま使う。

- 公開されるのは `.gomi` に書いた定義だけ（`pub` キーワードは持たない）。Go 側では先頭大文字の識別子になる（`.gomi` の `fn area` → `Area`）
- 非公開定義は Go 側で先頭小文字。Go の予約語と衝突する名前は `_` 接尾辞を付けてマングルする
- 同じディレクトリの `.gom` ファイルは 1 つのトップレベルスコープを共有する: 型・コンストラクタ・関数・`let` はファイルの順序に関係なく相互に見える。別ファイルの同名トップレベル宣言は E0202、`package` 句の不一致は E0605（後の句に位置付く）になる。`import` 宣言はファイルローカルのまま
- `import "path"` は Gom パッケージを読み込む（Go パッケージは `import go "path"`）。公開面はパッケージの `.gomi` で決まり、import path は生成される Go パッケージのパスと一致する。v1 ではメインモジュール内のパッケージだけが解決できる（ワークスペースの兄弟モジュールや依存モジュール内の Gom パッケージは解決しない）
- `package main` の `fn main () -> Unit` は Go のエントリポイント `func main()` になり、`gom build` の成果物はそのまま `go build` できる。使える引数を取る `main`（`(u: Unit)` など）や `Unit` 以外を返す `main` は特別扱いせず、通常の関数として生成される（Go と同じく `go build` が入口としては拒否する形になる）

### 5.1 パッケージの初期化順

トップレベルの `let` 束縛はパッケージ変数となり、そのパッケージが
使われる前に 1 度だけ評価される。順序は Go のパッケージ初期化と同じ
依存解析で決まる。

- 変数 x が変数 y に依存するのは、x の初期化式が y を参照するとき、
  または初期化式が参照する関数（`fn` 宣言・関数リテラル）の本体が
  推移的に y を参照するとき。関数から関数への参照だけでは依存を
  生じない。
- 初期化は依存の残っていない変数から進む。初期化できる変数が複数
  あるときは宣言順に選ぶ — 宣言順はファイル内では出現順、ファイルを
  またぐときはファイル名の辞書順である。import したパッケージは
  それを参照する側の初期化より先に初期化される。
- 依存関係が循環して初期化順を定められない変数が残ったら、その
  プログラムは不適格で、変数ごとに E0907 として報告される。

---

## 6. エラー処理と `try`

例外は持たない。失敗は `Result` で表す。Go の panic は伝播させ、`recover` は提供しない。

`try e` は、`e` が `Err x` なら `Err x` を返して脱出し、`Ok v` なら `v` になる。

1. 脱出先は、`try` を囲む最も内側の関数（`fn` 定義またはラムダ）。`test` ブロックも関数境界として扱う
2. その関数の戻り値型は `Result _ e` でなければならず、`e` は `try` した値のエラー型と一致しなければならない
3. `go { ... }` のブロックは関数境界ではない。その中で `try` を使うには、ラムダで包むか `Task.spawn` を使う
4. `try` は `GoError` を自動ラップしない（`|?>` との違い。[decisions/try-no-auto-wrap](decisions/2026-11-04-try-no-auto-wrap.md)）

---

## 7. インターフェースファイル `.gomi`

パッケージの公開 API は、OCaml の `.mli` に相当するインターフェースファイル `.gomi` に書く。依存する側は相手の `.gomi` だけを読んでコンパイルする。

```
// shapes/shapes.gomi
package shapes

/// 図形。コンストラクタを公開するので、利用側で match できる
type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)
  | Empty

/// 抽象型。中身は利用側から見えない
type Canvas

fn area (s: Shape) -> Float
fn new_canvas (~width: Int) (~height: Int) -> Canvas
fn draw (c: Canvas) (s: Shape) -> Canvas
let unit_circle : Shape
```

1. 1 パッケージに `.gomi` は 1 つ（`<パッケージ名>.gomi`）。書けるのは型定義・関数シグネチャ・定数の型と `///` のドキュメントコメントだけで、式は書けない
2. 関数は `.gom` と同じ引数リストの形で書く（引数の数とラベルがインターフェースの一部になる）
3. sum type はコンストラクタまで書けば透明、名前だけ書けば抽象。利用側で `match` できるのは透明な型だけ
4. ライブラリパッケージ（`package main` 以外）は `.gomi` が必須で、ないとエラー
5. `gom info` は、`.gomi` がまだないパッケージについて、全トップレベル定義のシグネチャを型推論の結果から書き出した下書きを作る
6. 実装の型はインターフェースと一致しなければならない（v1 では型変数の名前の違いだけを許す）
7. 依存パッケージの再コンパイルは、インターフェース（コメントと書式を除く）が変わったときだけ起きる。実装だけの変更では起きない
8. 実装とインターフェースの適合（4・6）は `gom check`・`gom build`・`gom test` が検査する。不適合のパッケージは生成物を書かない（`gom test` ではテストも実行しない。[decisions/gomi-conformance-in-gom-test](decisions/2026-11-04-gomi-conformance-in-gom-test.md)）

---

## 8. Go 相互運用

### 8.1 Go を呼ぶ

Go の関数は Go と同じ書き方で呼ぶ: `strconv.Atoi(s)`、`fmt.Println(a, b)`。n 引数の Go 関数は、Gom からは n 要素のタプルを 1 つ受け取る関数に見える。`f(a, b)` は並置の適用 `f (a, b)` そのものである。Go 関数はカリー化しない。`import go` すると、コンパイラがパッケージの型を自動で読み込むので宣言は不要。

```
import go "strconv"
import go "strings"
import go "bytes"
import go "fmt"

fn parse_sum (a: String) (b: String) -> Result Int GoError = {
  let x = try strconv.Atoi(a)        // (int, error) → Result Int GoError
  let y = try strconv.Atoi(b)
  Ok (x + y)
}

fn report (xs: List String) -> Unit = {
  let buf = bytes.NewBufferString(strings.Join(xs, ", "))
  ignore (buf.WriteString("!"))     // メソッド呼び出しも Go と同じ
  ignore (fmt.Println(buf.String(), List.length xs))
}
```

（`List.length` は `stdlib/list` として実装済み（P4-8）。`strings.Join` への `List String` 引数は `[]T` と `List T` の境界コピー（P3-3）の実装待ち）

1. 0 引数の Go 関数は `()` を受け取る（`time.Now()`）。1 引数なら `strconv.Itoa(n)` と `strconv.Itoa n` は同じ意味
2. 値として渡せる（`List.map strconv.Itoa xs`）
3. メソッド呼び出しは `x.M(args)`。Go の型の値にだけ使え、Gom の型はメソッドを持たない。`x.M(1).N(2)` のような連鎖は Go と同じく左結合で `(x.M(1)).N(2)` と読む（括弧つき引数に続く `.` は呼び出し結果への選択子になる。[decisions/post-arg-selector-binding](decisions/2026-11-05-post-arg-selector-binding.md)）
4. 可変長引数は個数自由に渡せる（`fmt.Println(a, b, c)`）。リストを展開するときは `fmt.Println(..xs)`。`..e` は Go と同じく可変長引数の全体をそのリストで渡すので、固定引数（`fmt.Sprintf("%s", ..xs)`）との併用はできるが、同じ可変長パラメータに入る引数とは併用できない（`fmt.Println("tag:", ..xs)` は E0505）
5. 数値型の暗黙変換は、Go の `int` / `int64` と Gom の `Int`、`float64` と `Float` のように情報が落ちないものに限る。それ以外は 8.4 節の `Go.conv` で明示する。Go の型なし定数（`math.Pi` など）は、期待される Gom の型になる
6. Go のジェネリック関数（`slices.Contains(xs, x)`）は型引数を推論して呼ぶ
7. `any` 型の引数には任意の Gom 値を渡せる。暗黙のボックス化はこの位置に限る
8. Go の interface 型の引数には、Go の代入規則（`go/types` の assignability）を満たす Go 由来の値を渡せる（`io.Copy(w, r)` に `*os.File` を渡すなど）。型注釈付きの束縛・戻り値・Gom 関数の引数でも、期待型が Go interface のときは同じ代入規則を使う。コンテナの要素型や関数型の同一性は変えない。Gom の型が Go の interface を実装することは v1 ではできない

### 8.2 `_` プレースホルダ

Go 呼び出しの引数の 1 つを `_` にすると、そこを受け取る 1 引数関数になる（Scheme の `cute`、Scala の `_` に相当）。

```
strings.Repeat(_, 3)             // fn s -> strings.Repeat(s, 3)
strconv.ParseInt(_, 10, 64)
```

- `_` は 1 つだけ置ける。Go 関数の呼び出しにだけ使える
- `_` 以外の引数は、関数を作る時点で 1 回だけ評価する（`cute` と同じ。呼び出しのたびに評価し直さない）

### 8.3 型の対応

| Go 側の型 | Gom 側の型 |
| --- | --- |
| `func(a A, b B) R` | `(A, B) -> R`（タプルを取る関数） |
| 戻り値 `(T, error)` | `Result T GoError` |
| 戻り値 `(T, bool)` | タプル `(T, Bool)`。`Option T` にするのは、標準のバインディングか `extern` で comma-ok と宣言した関数だけ（`os.LookupEnv` など） |
| 戻り値 `(A, B)`（上記以外の多値） | タプル `(A, B)` |
| 戻り値 `error` のみ | `Result Unit GoError` |
| `*T` | `Ptr T`（nil の参照は Go と同じく panic。操作は `Ptr` モジュール） |
| `int` / `int64` / `int32` / `float64` | `Int` / `Int` / `Int32` / `Float` |
| named type（`type UserID int64` など） | 別の名前的な型 `pkg.UserID`。基底型との変換は `Go.conv` で明示 |
| `[]T` | `List T`。境界では両方向ともコピーする（8.5 節） |
| `map[K]V` | 不透明型 `Go.Map K V`（操作は `Go.Map` の型メンバー群） |
| `chan T` / `<-chan T` / `chan<- T` | `Chan T` / `RecvChan T` / `SendChan T` |
| interface（`io.Reader`、`fmt.Stringer` など） | 名前的な型 `io.Reader` など。Go 由来の値を代入規則に従って渡せる |
| `error` | `GoError` |

v1 で対応していない型（符号なし整数 `uint8`–`uint64`/`byte`、`float32`、複素数、配列 `[N]T`、`unsafe.Pointer`、ジェネリック型・関数、メソッドを持つ無名 interface、関数型の引数・戻り値）をシグネチャに含むシンボルは公開されず、参照すると理由付きの診断になる（8.5 節）。ジェネリック関数については 8.5 節の記述と食い違いがある（[problem](problems/2026-11-04-generic-go-fns-hidden.md)）。

Go の名前付き struct 型の値またはそのポインタから、exported field を `value.Field` で読み取れる。Go と同じ promoted field と曖昧性の規則を使い、unexported field は参照できない。field の型は上の対応に従い、未対応の型は参照位置で理由付き E0501 になる。読み取りはソースの評価順に行い、nil ポインタ経由の読み取りは Go と同じく panic する。field への代入と Go struct のコンポジットリテラルはまだ定めない。

`Ptr T` の操作はコンパイラ組み込みの `Ptr` モジュールが持つ（[decisions/ptr-ops-surface](decisions/2026-11-05-ptr-ops-surface.md)）: `Ptr.get : Ptr a -> a`（`*p` — nil で panic）、`Ptr.set : Ptr a -> a -> Unit`（`*p = v` — nil で panic）、`Ptr.is_nil : Ptr a -> Bool`、`Ptr.new : Unit -> Ptr a`（`new(T)`。`errors.As` の target や decode 先のような out-pointer API 用）、`Ptr.make : a -> Ptr a`（`&v` — 新しいコピーへのポインタ）、`Ptr.nil : Unit -> Ptr a`（`tls.Dial` の `*tls.Config` のような任意の `*T` パラメータへ nil を渡す）、`Ptr.to_option : Ptr a -> Option a`（nil なら `None`、さもなくば `Some *p` — 安全な読み出し）。`Ptr` の `==`/`!=` は Go のポインタの `==` と同じ同一性比較（3.3 節の等価規則）。nil との比較は `Ptr.is_nil` / `Ptr.to_option` で行う。

`Go.Map K V` の操作は `Go.Map` の型メンバーとして解決する（[decisions/go-map-ops](decisions/2026-11-05-go-map-ops.md)）: `Go.Map.make : Unit -> Go.Map k v`、`Go.Map.get : Go.Map k v -> k -> Option v`（missing key は `None`）、`Go.Map.set : Go.Map k v -> k -> v -> Unit`、`Go.Map.delete : Go.Map k v -> k -> Unit`、`Go.Map.length : Go.Map k v -> Int`、`Go.Map.to_list : Go.Map k v -> List (k, v)`（順序不定のスナップショット。v1 の反復面）。`k` は Go の `comparable` 制約を持ち、`List` など比較できない表現のキーは E0508 で拒否される。nil マップの意味は Go のまま — get/delete/length は nil でも動き、set は panic する。

### 8.4 extern（上書き専用）

自動の対応付けで不都合なときだけ書く。

```
extern go "flag" {
  fn Lookup(name: String) -> Option (Ptr Flag)   // nil を返し得るので Option に
}

extern go "io" {
  // ラベル付きのカリー化版として公開する
  fn Copy (~dst: Writer) (~src: Reader) -> Result Int64 GoError
}
```

`extern` では型だけでなく変換規則も指定できる（`(T, bool)` を comma-ok として `Option` にする、`*T` を `Option (Ptr T)` にする、など）。

extern のエントリは `fn` の関数シグネチャだけを持ち、ブロックが名指す Go パッケージのエクスポートされた**関数**メンバーを上書きする。型・変数・定数・メソッドの上書きはできない。宣言は生の Go シグネチャと照合され、合わない場合は E0509 で報告される:

- パラメータ数は一致し、各パラメータ型は 8.3 節の対応表の組（同一型、`Int`/`Int64` ↔ `int`/`int64`、`List` ↔ `Go.Slice` の境界）のどれかでなければならない。
- 引数を取らない Go 関数は `fn F () -> T`（あるいは `(u: Unit)`）と宣言する。`()` は Unit パターンであり Go の引数として数えない。束縛されたシグネチャは自動導出の場合と同じく引数なしの形になり、呼び出しは `pkg.F ()` と書く。
- 結果の宣言は Go の結果の形が許すものだけ:
  - `()` → `Unit`
  - `T` → `T`（対応する型）、`*T` → `Option (Ptr T)`（`Option` はポインタのまま保持する）
  - `error` → `Result Unit GoError`
  - `(T, error)` → `Result T GoError`
  - `(T, bool)` → `Option T` または `(T, Bool)`
  - その他の `(A, B)` → `(A, B)` のタプル
- 宣言に型変数は書けない（Go の関数は多相ではない）。ジェネリックな Go 関数の上書きも現時点ではできない。

宣言した型が Gom 側で見える型になり、生成される呼び出しは生の Go シグネチャのまま `pkg.F(args)` で、結果は宣言した変換規則で物質化される — `(T, bool)` の `Option` は `v, ok := f(...)` の後 `option.FromGo(v, ok)`、`*T` の `Option` は `v := f(...)` の後 `option.FromGo(v, v != nil)`。

明示的な変換は組み込みの `Go.conv` で書く: `Go.conv user_id : Int`。変換できるかどうかは Go の変換規則で検査する。`Go.Slice T` と `List T` の間の `Go.conv` は、8.5 節の境界変換と同じくコピーする（[decisions/goconv-list-slice-copies](decisions/2026-11-04-goconv-list-slice-copies.md)）。

### 8.5 境界でのデータの扱い

- `List T` と Go の `[]T` は、境界を越えるときに両方向ともコピーする。Gom から渡したスライスを Go が書き換えても、Go から受け取ったスライスを Go が後で書き換えても、Gom の `List` は変わらない
- コピーを避けたいときは、Go が所有する可変の値として `Go.Slice T` を使う（`extern` で宣言する）。`List` を `Go.Slice` パラメータへ渡してもやはりコピーする — no-copy は `Go.Slice` から `Go.Slice` への引き渡しだけ
- このコピーのため、`sort.Strings xs` のような in-place 変更を契約に持つ Go 呼び出しはコンパイルできても Gom の値に効かない。結果が `Unit` の Go 呼び出しがコピーされる引数を `[]T` パラメータへ渡すとき、リンターは W0501 を警告する（[decisions/go-mutation-warning](decisions/2026-11-05-go-mutation-warning.md)）。コピーされるのは `List` 引数（`..xs` スプレッドと `Go.Slice` 宣言パラメータへの `List` を含む）と、`List` 位置へ物質化される `Go.Slice` 引数（チャネル送信の `[]T` パラメータなど）。素の `Go.Slice` が `[]T` スロットへそのまま渡る経路 — `Go.Slice` 宣言パラメータへの `Go.Slice` 引数、`..gs` スプレッド、`...[]T` の要素位置 — はバッキングを共有するので警告しない。読み取り専用の消費者は値を返すので（`strings.Join` など）警告されない。`signal.Notify` のように副作用を持つが slice を書き換えない Unit 関数も警告対象になり得る — 警告の文言は条件形で、事実としてコピーが起きることだけを述べる
- Gom が表現できない型（`unsafe.Pointer`、制約付きの型パラメータを持つジェネリック関数など）を含むシンボルは公開しない。`import go` 自体は失敗せず、そのシンボルを参照したときに理由付きの診断を出す
- Go のジェネリック関数は、制約が `any` か `comparable` のものだけを呼べる（v1）

### 8.6 nil と不正な値

- Go から受け取った Gom の ADT 型の値が `nil` などの不正な値だった場合は契約違反とし、`match` の時点で型名を含むメッセージで panic する
- 名前付きペイロードのフィールド参照 `s.f` も同じ族である — 値がそのフィールドを宣言するバリアントを保持していなければ契約違反として panic する（3.2 節）。これは Gom が作った正当なバリアント値にも起きる（値は正しくとも、期待するバリアントではない）
- Go の interface 型やポインタ型の値は `nil` になり得る。メソッド呼び出しや参照で Go と同じく panic する（`*T` の既定は open-questions.md）。`Ptr T` の nil 判定は `Ptr.is_nil`、安全な読み出しは `Ptr.to_option`、nil の供給は `Ptr.nil` で行う（8.3 節）

### 8.7 Go から Gom を呼ぶ

生成コードは普通の Go パッケージなので直接 import できる。生成コードは「読めて、デバッグでき、Go から自然に使える成果物」であり、人が直接書き換えて保守するソースではない。sum type は sealed interface として公開され、Go 側では type switch で分解する（網羅性は Go 側では保証されない）。ただし `Result a e` は例外で、非公開タグ付きの struct として公開し、`Ok` / `Err` のコンストラクタ関数と `Tag()` / `Unwrap()` で扱う。

---

## 9. 失敗し得る処理のパイプ `|?>`

### 9.1 動機

Go の標準ライブラリの多くは `(T, error)` を返す。これらを毎回 `match` や `try` で受けていては Go のパッケージを素直に使えないため、専用のパイプ演算子を持つ。

### 9.2 意味

```
fn load_config (path: String) -> Result Config GoError =
  Ok path
  |?> os.ReadFile
  |?> parse_toml
  |?> validate
  |?> fn c -> Ok { c with source = path }

fn parse_port (s: String) -> Result Int64 GoError =
  Ok (strings.TrimSpace(s))
  |?> strconv.ParseInt(_, 10, 64)
  |?> check_port_range
```

（この例は現状コンパイルできない: `os.ReadFile` の `[]byte` 戻り値は v1 で対応していない型（8.3 節）で、戻り値の `Int64` も 8.3 節の対応表（`int64` → `Int`）とずれている — [implementation-gaps](implementation-gaps.md) 参照）

1. `r |?> f` は `r : Result a e` と `f : a -> Result b e` を取り、`Result b e` を返す。`Ok v` なら `f v`、`Err e` ならそれ以降のステップを飛ばして `Err` のまま流す
2. 優先順位と結合は `|>` と同じ。1 つの流れの中で `|>` と混ぜられる
3. **自動ラップ**: エラー型が `GoError` のとき、失敗したステップで `fmt.Errorf("<ステップ名>: %w", err)` を自動で付ける。`errors.Is` / `errors.As` で元のエラーを判定できる
4. ステップ名は、右辺の式の先頭にある名前をソースに書かれたとおりに使う（`os.ReadFile`、`parse_with opts` なら `parse_with`、`strconv.ParseInt(_, 10, 64)` なら `strconv.ParseInt`）。`let f = parse_toml` のあと `|?> f` と書けば名前は `f` になる。関数値の由来は追跡しない。右辺がラムダなどで名前がなければソース位置（`config.gom:14`）を使う
5. 包むのはエラーが起きたステップでの 1 回だけ。後続のステップは `Err` を素通しする
6. エラー型が `GoError` 以外のときは包まない
7. 最初の値が `Result` でなければ `Ok` で包んで始める。`path |> os.ReadFile |?> ...` と書くと、最初の Go 呼び出しの失敗は包まずに流れる
8. `Result` を返さない関数は右辺に置けない。最初の `|?>` より前なら `|>` で、後ろなら `|> Result.map f` でつなぐ: `s |> strings.TrimSpace |?> strconv.Atoi |> Result.map double`（[decisions/result-pipe-step-must-return-result](decisions/2026-11-04-result-pipe-step-must-return-result.md)）

例: `validate` が `port must be > 0` で失敗すると、結果は `validate: port must be > 0`。

自動ラップは `errors.Is` / `errors.As` の結果を変えないが、`Error()` の文字列は変える。この文字列も公開される振る舞いの一部として扱い、規則を変えるときは決定記録を残す。

ラップの文言の設定方法は future work（open-questions.md）。

---

## 10. 並行処理

goroutine とチャネルは Go のものをそのまま使う。言語に入れるのは `go` 式、チャネル型、`select` 式の 3 つだけ。

```
import go "context"
import go "time"

type Stop =
  | Closed
  | Cancelled
  | Timeout

fn worker (jobs: RecvChan Job) (results: SendChan Output) -> Unit =
  for job in jobs {
    Chan.send results (process job)
  }

fn run (input: List Job) -> List Output = {
  let n = List.length input
  let jobs = Chan.make n
  let results = Chan.make n
  for _ in List.range 0 4 {
    go { worker (Chan.recvOnly jobs) (Chan.sendOnly results) }
  }
  for job in input { Chan.send jobs job }
  Chan.close jobs
  List.map (fn _ -> Chan.recv results |> Option.get) input
}

fn next_job (ctx: context.Context) (jobs: RecvChan Job) -> Result Job Stop =
  select {
    r <- jobs -> match r {
      Some job -> Ok job
      None -> Err Closed
    }
    _ <- ctx.Done() -> Err Cancelled
    _ <- time.After(time.Second) -> Err Timeout
  }
```

（`Option.get` / `List.*` は `stdlib/{option,list}`、`Chan` / `Task` は並行処理（P5-1）として実装済み。`ctx.Done()` の `<-chan struct{}` は `RecvChan Unit` として受け取れる）

1. `go { e }` は `e` を新しい goroutine で評価する式で、型は `Unit`。`e` が `Unit` 以外ならエラー
2. チャネル型は `Chan a`（双方向）、`RecvChan a`、`SendChan a`。方向の制限は `Chan.recvOnly` / `Chan.sendOnly` で明示的に変換する。Go 関数に渡すときだけは Go の代入規則に合わせて暗黙に変換する
3. 操作: `Chan.make n`（`n = 0` でバッファなし）、`Chan.send ch v : Unit`、`Chan.recv ch : Option a`（閉じていれば `None`）、`Chan.close ch`。`for x in ch { ... }` はチャネルが閉じるまで受信する
4. `select` は式で、すべての腕が同じ型を返す。腕は受信 `x <- ch -> e`、送信 `send ch v -> e`、`default -> e` の 3 種類。各腕は Go の `select` の 1 つの `case` にそのまま対応する。複数の腕が同時に準備できていれば、Go と同じく一様ランダムに 1 つ選ぶ（優先度付きの書き方はない。[decisions/select-arm-choice](decisions/2026-11-04-select-arm-choice.md)）
5. 受信腕の左辺は変数か `_` だけ（論駁不可能なパターンのみ）。`x` の型は `Option a` で、閉じたチャネルからの受信は `None`。値による分岐は腕の本体の `match` で書くので、閉じた場合の処理漏れは通常の `match` の網羅性検査で見つかる
6. 評価順は Go と同じ: `select` に入った時点で、すべての腕のチャネル式と送信値を、ソースに書かれた順に 1 回ずつ評価する。選ばれなかった腕の式も評価される
7. 同じチャネルを複数の腕に書いてもよい（Go と同じく、別々の `case` になる）
8. `time.After(d)` や `ctx.Done()` は `RecvChan` なので、そのまま `select` の腕に書ける
9. 待ち合わせには標準ライブラリの `Task`（`Task.spawn : (Unit -> a) -> Task a`、`Task.await : Task a -> a`）か、Go の `sync` パッケージを使う。`Task` 内の panic は `await` 側で再び panic する
10. `Task` を使わない goroutine で捕捉されなかった panic は、プログラム全体を停止する

**送ってよい値と捕捉してよい値**: 不変な Gom の値はそのまま送れる。リンターは次の場合に警告する: 要素の型が可変な値（`Ref a`・`Array a`・`Ptr T`・`Go.Slice a`・`Go.Map k v`）を含むチャネルを作るとき、および `go { ... }` がそれらの型の変数を捕捉するとき。

---

## 11. テスト

テストは実装と同じ `.gom` ファイルに書く。

```
@test
fn unit_square () -> Shape = Rect 1.0 1.0

test "rect area" {
  assert_eq (area (Rect 2.0 3.0)) 6.0
}

test "parse then sum" {
  let n = try parse_sum "1" "2"
  assert_eq n 3
  Ok ()
}
```

1. `test "名前" { ... }` はトップレベルの宣言。ブロックの型は `Unit` か `Result Unit e`。`Err` を返すと失敗
2. テストは `.gomi` で公開していない定義も呼べる
3. `@test` を付けた定義はテスト用のスコープに入り、テストと他の `@test` 定義からしか参照できない。名前空間はパッケージと共有するので、通常の定義と同じ名前は付けられない
4. アサーションは `assert cond` と `assert_eq actual expected`。`assert_eq` は `==` が使える型（関数を含まない型）にだけ使える。失敗すると両方の値を表示する（Go 由来の値は `fmt` の `%v` で表示する）。どちらも `test` ブロックの中でだけ使える（外で呼ぶ・値として参照するのは E0803）
5. テストと `@test` 定義はインターフェースに含まれない
6. `gom build` でもテストの型検査とパターン検査は行う。テストのコードは `gom test` のときだけ出力される

---

## 12. コンパイラのメッセージ形式

コンパイラが出すメッセージ（エラー・警告・注記・ビルド結果の集計）は、既定で JSON Lines（1 行に 1 つの JSON オブジェクト）として標準出力に書く。人間向けの表示は `--format text` で切り替える。生成される `.go` ファイルには関係しない。

```json
{
  "kind": "diagnostic",
  "code": "E0301",
  "severity": "error",
  "message": "non-exhaustive match on `Expr`",
  "span": {
    "file": "eval.gom",
    "start": { "line": 2, "col": 3, "offset": 41 },
    "end": { "line": 9, "col": 4, "offset": 228 }
  },
  "data": {
    "scrutinee_type": "Expr",
    "missing_patterns": ["Neg (Num _)", "Neg (Add _ _)", "Neg (Let _ _ _)", "Let _ _ _"]
  },
  "notes": ["arms with guards do not count toward exhaustiveness"],
  "fixes": [
    {
      "title": "add the missing arms",
      "applicability": "placeholder",
      "edits": [
        {
          "file": "eval.gom",
          "start": { "line": 9, "col": 3, "offset": 226 },
          "end": { "line": 9, "col": 3, "offset": 226 },
          "text": "  Neg (Num _) -> todo ()\n    Neg (Add _ _) -> todo ()\n    Neg (Let _ _ _) -> todo ()\n    Let _ _ _ -> todo ()\n  "
        }
      ]
    }
  ]
}
```

（実際の出力は 1 行。ここでは整形している）

```json
{"kind": "summary", "ok": false, "errors": 1, "warnings": 0, "files": ["eval.gom"], "outputs": []}
```

1. 1 行に 1 オブジェクト。JSON・text・LSP は、どれもコンパイラ内部の診断データからの変換であり、同じ内容を持つ。`kind` で種類を区別し、最後の行は必ず `summary`。終了コードは成功 0、診断エラーあり 1、コンパイラの内部エラー 2
2. `code` は安定した識別子。意味を変えず、廃止しても番号を再利用しない
3. `message` は 1 行の英語。詳細は `data` に構造化して入れる
4. 位置は 0 始まりの UTF-8 バイトオフセットを正とし、1 始まりの行・列（列は Unicode コードポイント単位）を出力時に計算して添える。LSP など別の座標系（UTF-16）が必要な出力では、オフセットから変換する
5. `fixes` の `applicability` は `safe`（そのまま適用してよい）、`placeholder`（`todo ()` など実行すると panic する穴を含む）、`unsafe`（意味が変わり得る）の 3 段階。`gom fix` は `safe` だけを自動で適用する
6. 生成した Go コードに対する `go build` / `go vet` / `go test` のエラーも `.gom` の位置に戻し、`"source": "go"` を付けて同じ形式で出す
7. 出力の JSON Schema は `gom schema` で取り出せる。互換性のない変更をしたらスキーマのバージョンを上げる
8. `summary` の `ok` は error 診断がなく失敗したテストもないときだけ `true`。`gom test` の summary にはテストの集計として `"tests": {"run": N, "failed": N, "skipped": N}` が付く（他のコマンドでは `tests` 自体がない）

| コマンド | 出力するメッセージの `kind` |
| --- | --- |
| `gom build` | `diagnostic`、`summary`（生成した `.go` ファイルの一覧を含む） |
| `gom check` | `diagnostic`、`summary`（Go を生成せず、型検査とパターン検査だけ行う） |
| `gom test` | `diagnostic`、`test`（テストごとの成否・所要時間・失敗時の値と位置）、`summary`（`tests` の集計を含む） |
| `gom fmt --check` | `unformatted`（整形が必要なファイルと差分）、`summary` |
| `gom fix` | `applied`（適用した編集）、`summary` |
| `gom explain <code>` | `explanation`（エラーコードの説明・例・修正例） |
| `gom schema` | メッセージ形式の JSON Schema |

`applied` オブジェクトは適用した fix 1 件につき 1 つ。`code` は fix が答えた診断のコード、`title` は `fixes` にあった fix のタイトル、`edits` は診断の fix edit と同じ形（`file`・`start`・`end`・`text`）で、編集前のソースの座標系で解決される。

```json
{"kind": "applied", "code": "E0402", "title": "discard the value with `ignore`", "edits": [{"file": "a.gom", "start": {"line": 7, "col": 3, "offset": 93}, "end": {"line": 7, "col": 3, "offset": 93}, "text": "ignore ("}, {"file": "a.gom", "start": {"line": 7, "col": 16, "offset": 106}, "end": {"line": 7, "col": 16, "offset": 106}, "text": ")"}]}
```

`explanation` オブジェクトはエラーコード表のエントリをそのまま運ぶ。`code`・`severity`・`title`・`phase`・`explanation` は必須、`spec`・`example` は表が持つときだけ、`retired` は廃止コードのときだけ `true` で付く。引数なしの `gom explain` は全コードの `explanation` を出す（`--format text` ではコードとタイトルの一覧）。

```json
{"kind": "explanation", "code": "E0301", "severity": "error", "title": "non-exhaustive match", "phase": "pattern", "spec": "4", "explanation": "...", "example": "..."}
```

`test` オブジェクトは `gom test` が `go test -json` から変換した 1 テストの結果で、`diagnostic` の後、`summary` の前に出る。`name` は `test "name"` の名前（Gom が生成していないテストは Go の関数名）、`func` は生成された Go のテスト関数名（`go test -run` のセレクタに使える）、`ok` は成否、`elapsed` は `go test` が報告した秒数。`skipped` はスキップ時だけ `true` で付く（スキップは `ok: true`）。`span` はテスト宣言の名前の位置で、Gom が生成していないテストでは付かない。`output` はそのテストが出した出力 — 失敗時は `assert_eq` の両方の値と、生成 `//line` 経由で `.gom` に戻した位置を含む。panic や goroutine dump が出たとき `output` は 2 部構成になる — 先にフレームを `.gom` の位置に戻したトレース（`.gom` に解決できないフレームは生の `file:line` のまま残る）、続いて生の Go スタックトレースをそのまま出す。`gom test -q` / `--quiet` は生のトレース側を抑制する（`.gom` 位置付きのトレースは残る）。`init` や `TestMain` のようなパッケージレベルの panic を運ぶ E0001 diagnostic の `output` も同じ 2 部構成にする。

```json
{"kind": "test", "name": "rect area", "func": "TestRectArea", "ok": false, "elapsed": 0.001, "span": {"file": "area.gom", "start": {"line": 15, "col": 6, "offset": 232}, "end": {"line": 15, "col": 16, "offset": 242}}, "output": "    area.gom:16: gom: assertion failed:\n          actual:   Rect ~w: 1 ~h: 1\n          expected: Rect ~w: 2 ~h: 2\n"}
```

`gom test` が実行するのは `go test` のパッケージ粒度である — 入力ファイルのあるディレクトリのパッケージ全体のテストが走るので、同じパッケージの他の `.gom` 由来や手書きのテストも結果に含まれる。それらは `span` のない `test` オブジェクトとして `func` と同名の `name` で報告される。コンパイルにエラーがあるとき `go test` は実行しない（`tests` の集計はすべて 0）。Go のツールチェーン側の失敗（テストバイナリがコンパイルできない、`go test` が起動できない）は `"source": "go"` の `diagnostic` として報告する。

`gom test` の引数列のうち最初の `--` 以降は `go test` にそのまま渡す（`gom test -- -race`、`gom test -- -run Bump -count=1` など）。`-json` は gom が結果の取得に使うフラグなので、これを打ち消す指定（`-json=false`）は結果ストリームを壊し、Go ツール側の失敗として `"source": "go"` の E0001 で報告される。未知・不正なフラグも gom 側の使用エラーではなく同じ経路で報告される（[decisions/gom-test-go-test-flags](decisions/2026-11-05-gom-test-go-test-flags.md)）。
