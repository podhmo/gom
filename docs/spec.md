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
2. 途中の式が `Unit` 以外を返したらコンパイルエラー。捨てるなら `ignore e` を明示する。ただし捨てた値の型が関数型のときは W0901 で警告する（部分適用の握り潰しは「残りの呼び出しを忘れた」可能性が高いため — 4.1 節）
3. `if c { a } else { b }` は式。`else` の省略は `a : Unit` の場合のみ許可
4. `let x = e` はブロック内で以降のスコープを導入する。再代入は不可（シャドーイングは可）
5. `let` の左辺は論駁不可能なパターンのみ（タプル・レコード分解、単一コンストラクタ型）。論駁可能なら `match` を使う
6. `return` / `break` / `continue` は持たない。早期脱出は `Result` と `try` 式で行う
7. ループは再帰が基本。自己末尾再帰は Go の `for` に変換することを保証する。`match` の分岐結果が `[h, ..f 引数]` 形（自身の呼び出し結果を cons する蓄積再帰）で、呼び出し結果がそのまま `..rest` にだけ使われるものも同じく `for` に変換する（検出できる形状はそれ以外にも広げない — 当てはまらない形は通常の再帰としてコンパイルされるだけで、診断は出ない）糖衣として `for x in xs { ... } : Unit` を用意する。反復対象は `List a`、`View a`、`Go.Slice a`（いずれも `range []T` と同じく添字順で、nil は 0 回反復）、受信可能なチャネル（10 章）で、パターンはそれぞれの要素型を束縛する
8. `fn` 宣言には `@tailrec` を付けられる。「この関数の自己再帰呼び出しはすべて末尾位置にある」ことをコンパイラに要求する属性で、守れない場合はエラー（E0902–E0905）になる。`let` には付けられない。検査は lowering 段階で行うので `gom build`（テストコードは `gom test`）が報告し、型・パターン検査までの `gom check`（12 節）は対象外
   - **末尾位置**とは、関数の結果を直接決める位置のこと。ブロック末尾の式、`if` / `match` / `select` の各分岐の結果、および最外の `|>` / `<|` の適用だけが該当する
   - オペランドや `let` 束縛の中、`for` の本体、`try`、`|?>` の適用、ネストした `fn` / ラムダの内側にある自己呼び出しは末尾位置ではない
   - `go { ... }` の中の自己呼び出しは例外で、新しいゴルーチンのスタックで実行されるため末尾位置として数えない（10 節）
   - `@tailrec` の関数を値として使うこと（`let g = f` や引数に渡すこと）はできない。別名経由の呼び出しは確認できないため（E0903）
   - `f -> g -> f` のような相互再帰は `@tailrec` では認めない。Go には相互再帰の末尾呼び出し最適化がないため（E0904）。パッケージレベルの `let` を経由する循環も同じく相互再帰とみなす — `let g = f` や `let (g, _) = (fn ..., ...)` のように関数値を保持する `let` を呼び出しに使う経路でも、反復ごとに本物の呼び出しが起きてスタックは増える
   - この相互再帰の検査が辿るのはコンパイラが静的に解決できる参照だけである。引数として受け取った関数値を呼び戻すような、呼び出し先を特定できない経路の再帰は `@tailrec` の保証の対象外とする
   - cons 蓄積形の自己呼び出し（`match` の分岐結果が `[h, ..f 引数]` で lower がループ化するもの。7 項目）は末尾位置ではないが、スタックを増やさない変換対象として末尾呼び出しと同じく数える
   - 変換可能な自己末尾呼び出し（末尾位置にある直接の自己呼び出し）がひとつもなければエラー（E0905）。再帰呼び出し自体がない場合、別名経由でしか呼ばれない場合、`go` の中だけで再帰する場合がこれにあたる。末尾位置にない直接呼び出しは E0902 で報告される
9. `fn` 宣言には `@extern` を付けられる。「この関数の本体は、生成される同じ Go パッケージに手書きの Go 関数としてある」ことを宣言する属性（[decisions/cons-buffer-array-extern-impl](decisions/2026-11-18-cons-buffer-array-extern-impl.md)）。宣言はシグネチャだけを持ち `= body` は書けない — 全パラメータと結果型に型注釈が必要になる。解決・型検査・`.gomi` との適合は通常の `fn` と同じで、呼び出しの出力形も変わらない。違いは宣言自身がコードを生成しないことだけで、実装は同じパッケージの手書き Go ファイルが同名（生成名 — `map` → `Map`）の関数として担う。名前・シグネチャのずれは Gom 側ではなく Go の `go build` / `go test` が報告する。`let` や `.gomi` には付けられない

### 1.2 その他の構文

- 関数適用は並置 `f x y`。左結合で、どの二項演算子よりも強く結合する（`f x + g y` は `(f x) + (g y)`）
- フィールド・モジュールのアクセスは適用より強い: `f p.name` は `f (p.name)`、`List.map f xs`
- 負の数を引数に渡すときは括弧が必要: `f (-1)`
- ラムダ: `fn x -> x + 1`、複数引数は `fn a b -> a + b`。パラメータは `fn` 宣言と同じ `(x: T)` の形で型注釈できる: `fn (x: Int) -> x + 1`、`fn (x: Int) (y: Int) -> x + y`。パラメータにラベルを付けることもできる — 呼び出し側の `~x:` pun と対称の裸の形 `fn ~x -> x` と、`fn` 宣言と同じ注釈つき `(~x: T)` の形 `fn (~x: Int) -> x`（[decision](decisions/2026-11-23-labeled-arg-relaxation.md) 案 D）。ラベルつきラムダの型は `~x: T -> R` で、expected 側のラベルと名前対応で受ける — ラベルのない expected への逆方向 introduction はしない（2.1 節）。本体はできるだけ右まで伸びる
- 引数のない関数は `Unit` を取る: 定義は `fn main () -> Unit`、呼び出しは `main ()`
- タプル `(a, b)`、リスト `[1, 2, 3]`、レコード `{ name = "a", age = 3 }`、更新 `{ p with age = 4 }`
  - フィールド名を裸で書く pun が使える: `{x, y}` は `{x = x, y = y}` と同じ意味で、同名の変数をフィールド値に取る。`{ p with x }` の更新でも同様。`{x}` 単独はブロック式（x を返す）のままなので、1 フィールドの pun は末尾に `,` を置いて `{x,}` と書く。パターン側の pun と同じ規則（8 章・パターン一覧）
- リストの末尾要素は `..rest` で他のリストを接合できる: `[x, ..rest]` は `rest` を壊さずに先頭へ `x` を足した新しいリストを作る（コンス）
- コメント `//` と `/* */`、ドキュメントコメント `///`、文字列補間 `"hello ${name}"`
- 二項演算子は優先順位の高い順に `* / % &&& &&^ <<< >>>`、`+ - ||| ^^^`、`== != < <= > >=`、`&&`、`||`、パイプ系（`|>` `<|` `|?>` `>>` `<<`、1.4 節）。`&&&`・`&&^`・`|||`・`^^^`・`<<<`・`>>>` はビット演算子で Go の `&` `&^` `|` `^` `<<` `>>` に対応する（三重字の綴りは `|` や `<<`/`>>` の他用途との衝突を避けるため — [decision](decisions/2026-11-23-bit-operators.md)）。被演算子は `%` と同じく組み込み整数型と underlying が整数種の named Go 型（3.3 節）。単項のビット反転は持たない — 符号つき型では `x ^^^ -1` が `^x` と同値、unsigned の named 型では `y &&^ x` でビットを落とす

### 1.3 カリー化

Gom の関数はすべてカリー化され、`add 1` は `Int -> Int` の関数になる。部分適用のための専用構文は持たない（Go 関数の `_` プレースホルダは 8.2 節）。

宣言パラメータが 2 本以上の矢印を持つ関数型（`fn g (f: A -> B -> C) -> ...` の `f`）は flat 呼出規約で渡る。この場合 `f a` の部分適用は呼出全体を残余引数の到着まで遅延する — `todo` 等の作用は flat arity を満たす適用の時点で発火し、residual が一度も適用されなければ作用は走らない。生成コードと eval の両方で同じタイミング（[decision](decisions/2026-11-18-flat-fn-params.md)、[decision](decisions/2026-11-19-flat-partial-eval.md)）。

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

### 1.8 予約語

次の 22 語は予約語であり、変数名・関数名・型名・パラメータ名・import 別名など名前を束ねる位置では使えない:

```
as default else extern false fn for go if ignore import in let
match package select send test true try type with
```

束ねる位置に予約語を書くと E0106（"X is a reserved word and cannot be used as a name"）を報告する — パーサはその単語を名前とみなして回復するので、続くトークンの二次エラーをできるだけ出さない。ただし `let` / `fn` / `type` / `import` / `extern` / `test` / `@` のように宣言そのものを始める語は例外で、名前として取り込むと続く本物の宣言を飲み込んでしまうため、従来どおり E0101 の回復に任せる（E0106 は出ない）。`.` のあとのメンバー名も例外で、`Chan.send` のようなエクスポート名のためにあらゆる予約語が使える（grammar.ebnf 2.2、member-name）。`assert` / `todo` / `not` / `show` は予約語ではなく普通の関数（§11）。

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
6. **ラベルはカリー化された鎖の全体を指す**: `fn f (~a: Int) -> ~b: Int -> Int` への `f ~b: 3` は `fn ~a -> f ~a ~b: 3` と同じ意味（戻り値関数のパラメータを先に束縛する部分適用）で、`f ~b: 3 ~a: 1` は `f ~a: 1 ~b: 3` と同じ `f(1)(3)` を適用する。位置引数はこの規則の対象外で、常に自分のレベルの未束縛パラメータを宣言順に埋める（[decision](decisions/2026-11-06-label-args-into-returned-functions.md)）
7. sum type のバリアントでも同じ記法を使える: 定義は `| Transfer (~to: Account) (~amount: Money)`、構築は `Transfer ~to: bob ~amount: 100`
8. **パンニング**: 渡す値がラベルと同名の変数なら、`~to: to` を `~to` とだけ書ける。値を書かないラベルは、必ず同名の変数を参照する（その変数がなければ「未定義の変数 `to`」のエラー）。式や別名の変数を渡すときは `~to: x` と書く
9. **record の spread**: `f ~..opts` と書くと、record の各フィールドが名前でラベル付きパラメータに対応づく（`move 0 ~..opts` は `move 0 ~dx: opts.dx ~dy: opts.dy` と同じ）。`~..` の値は単純式 1 つ — `~dx:` の値と同じ規則で、複合式は `~..(mk_opts ())` と括る。record 以外の値は E0418。対応する `~name` パラメータを持たないフィールドは E0406、同じラベルが二度埋まる（別の `~dx:` との衝突を含む）と E0405。位置引数や個別の `~label:` との混在は可 — 位置引数は位置パラメータだけを、フィールドはラベル付きパラメータだけを埋める（[decision](decisions/2026-11-21-record-spread-arg.md)）。record は一度だけ評価され、各フィールドの読み出しはその結果から行われる

### 2.1 型の扱い

- ラベルは関数型の一部: `Account -> ~to: Account -> ~amount: Money -> Result Unit TxError`。型の同値は、連続するラベル付き引数の並びを名前対応かつ宣言順で比較する — `~a: Int -> ~b: Int -> R` と `~b: Int -> ~a: Int -> R` は別の型（生成 Go は位置呼び出しで、並びが違うと引数が入れ替わる）。呼び出し側の `~x:` 記述順は引数規則どおり順不同のまま
- ラベル付きの呼び出しは、その時点で関数の型が既知であることを要求する。型変数のままなら注釈を求めるエラーにする
- ラベル付き関数とラベルなし関数の暗黙変換はしない。ただし expected 型がラベル付きの位置で produced 側の同じ flat 位置にラベルのないパラメータがあるときは、expected のラベルを位置対応で貸す一方向の introduction を許す（`Int -> Int` は `~x: Int -> Int` を要求する位置に置ける）。逆方向（labeled → unlabeled）と、途中にラベル付きが混ざる produced 型への introduction は変換しない（[decision](decisions/2026-11-23-labeled-arg-relaxation.md)）

省略可能引数・デフォルト値は future work とする。

---

## 3. 型システム

Hindley–Milner（let 多相）を基礎とする。型はすべて名前的（nominal）で、構造的部分型は持たない。型の適用も並置で書く（`List Int`、`Result Unit TxError`、`Option (List Int)`）。型変数は小文字。

### 3.1 組み込み型

| Gom の型 | 説明 |
| --- | --- |
| `Int` / `Float` / `Bool` | 64 ビット整数 / 64 ビット浮動小数点 / 真偽値。`Int32` 等のサイズ付き型も提供 |
| `String` / `Char` / `Unit` | 文字列 / Unicode コードポイント / 単位型（値は `()`） |
| `Byte` | Go の `byte`（`uint8`）。`List Byte` が `[]byte` に対応するバイト列 I/O 用の型（8.3 節）。リテラルと四則・比較演算子はなく、`==`/`!=` と `Go.conv` による数値型との変換だけ使える |
| `(a, b)` | タプル |
| `List a` | 不変リスト。不変なのはリスト自体で、要素の可変性は要素型の契約に従う（`List (Ref Int)` の要素は可変のまま。境界では 8.5 節） |
| `Option a` / `Result a e` | 標準ライブラリで定義された通常の sum type |
| `a -> b` | 関数 |
| `Ref a` / `Array a` | 可変参照 / 可変配列（限定的な可変性） |
| `Chan a` / `RecvChan a` / `SendChan a` | チャネル（10 章） |
| `Task a` | 待ち合わせ可能な goroutine の結果（10 章） |
| `Ptr a` | Go の `*T`（8.3 節） |
| `View a` | `List` 由来の読み取り専用部分ビュー（8.3 節、不変バッキングの共有） |
| `Go.Slice a` / `Go.Map k v` / `Go.Any` | Go の `[]T` / `map[K]V` / `any` の書ける綴り（8.3 節） |
| `Go.FuncN` | Go の func 型（8.8 節） |
| `GoError` | Go の `error` |

A `type` declaration in a file may reuse a builtin name. In that file the unqualified name then denotes only the local declaration — the builtin is unreachable (shadowing applies per name, file-wide). Inside a file declaring `type Result`, `Result` is that ADT; constructs that specifically require the builtin `Result a e` — `try`, `|?>`, calls to Go functions whose signature contains `Result`, and a `test` body typed `Result Unit e` — report E0415. `List`, `Option`, `Int`, `Unit` and the other builtin names behave the same way and are compiled and evaluated as ordinary local ADTs. Qualified names such as `pkg.Result` or `Go.Slice` are never shadowed.

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

`type alias X a ... = T` は既存の型に別名を付ける宣言で、新しい型を導入しない — Go の `type X[A] = T` に対応し、alias はあらゆる位置で `T` に完全展開される（`type alias UserId = Int` の値は `int64` そのもの。`type alias Wrap a = List a` のような型パラメータも書ける）。`alias` は `type` の直後だけで効く文脈依存キーワードで、予約語ではない — `let alias = ...` は従来どおり普通の名前として書ける。`type X = T`（`alias` なし）は従来どおり新しい型の宣言 — record 本体か sum バリアント列で、`type A = Int` はコンストラクタ `Int` を持つ単一バリアント sum になる（透明な別名が意図なら `type alias A = Int` と書く）。

record の各フィールドは、型のあとに Go の struct tag を verbatim の raw string として書ける — `type User = { name: String `json:"name"` }`。タグはそのまま生成される struct フィールドに付くので、`encoding/json` など Go の reflection ベースのライブラリにそのまま効く（フィールド自体は従来どおり PascalCase に export される）。`key:"value"` の形になっていないタグは E0515。型パラメータを持つ record や Go emit が defined type になる 1 フィールド record（8.7 節）でも同じ位置に書ける（後者はフィールド名が Go 側に出ないので、タグは struct フォールバック時にのみ意味を持つ）（[decision](decisions/2026-11-24-record-field-struct-tag.md)）。

レコード型は自分自身を値として内包できない（E0413）。フィールドはインライン配置されるので、`type Point = {x: Point}` や相互に含め合うレコードは無限の大きさを持つ。タプルや `Result`/`Task` のペイロードも同じくインラインで、それらを通る再帰もエラーになる。再帰が許されるのは間接参照を介するときだけで、関数型のフィールド（`{f: () -> Point}`）、sum type のバリアント、`List` / `Ref` / `Ptr` / `Array` / `Chan` / `Go.Slice` / `Go.Map` などのポインタ表現を通る再帰は合法。

### 3.3 型推論の範囲

- **value restriction**: トップレベルの定義は、右辺が関数（`fn` 定義・ラムダ）か構文的な値（リテラル、コンストラクタの適用、それらからなるタプル・リスト・レコード）のときだけ一般化する。`let r = Ref.new []` のような関数適用の結果は一般化しない（可変参照による型安全性の破れを防ぐ）
- ローカルの `let` は一般化しない。これは実装上の一時的な制限ではなく v1 の型規則とする（将来ローカル多相を入れる場合は relaxed value restriction を検討する）
- `let` の型注釈が新しい型変数を導入できるのは、一般化されるトップレベルの `let` だけ（その変数は束縛自体の型パラメータになる）。`fn` や型定義のシグネチャが宣言した変数は内側の注釈からも参照できるが、それ以外で型変数を書いた注釈は未束縛変数としてエラーになる
- 再帰呼び出しは型引数を成長させてはならない（E0416）。`fn wdepth (w: W a)` の本体で `wdepth` を `W (List a)` で呼ぶような、呼び出しのたびに型が大きくなる再帰（自己・相互を問わない）を許すと、生成される Go のジェネリクスは無限のインスタンシエーションを要求し、Go コンパイラが instantiation cycle として拒否する。型変数の入れ替えだけの再帰（`swap` が `swap2` を呼び `swap2` が `swap` を呼ぶ等）は合法。ADT のフィールドが `W (List a)` のような型で再帰すること自体は許可される — 制限されるのは関数呼び出しの型引数だけ
- 公開関数のシグネチャは `.gomi` に必ず書く（7 章）
- 算術演算子は組み込み数値型と、underlying が数値の基本型の named Go 型（`time.Duration` など）にオーバーロードし、型検査の最後で具体型に解決する。named Go 型の算術・比較は Go 自身の規則と同型 — 両被演算子が同じ named 型なら演算でき、結果もその型。型なしリテラルは被演算子の named 型へ conform する（`30 * time.Second` は `time.Duration`）。named 型と組み込み数値型の混在（`d + intVar`）は Go と同じく型不一致（E0401）。%（剰余）とビット演算子（`&&&`・`&&^`・`|||`・`^^^`・`<<<`・`>>>`）は underlying が整数種の named 型に限り、`<`/`>` は整数・浮動小数点種に限る。未解決なら注釈を求めるエラー
- 数値演算は演算子ごとに機械型で 1 回ずつ評価する。`Float` のリテラルは書かれた値に最も近い `float64`（IEEE 754 binary64）を表し、各演算子は被演算子の `float64` に IEEE 演算を適用した結果を返す — `869.32 * 38.29` は `33286.262800000004` になる。整数演算は 64 ビット（`Int32` は 32 ビット）の 2 の補数 wrap-around で、オーバーフローはエラーではなく値が回る。定数畳み込みは言語の意味に含めないので、`let a = 869.32` のあとの `a * 38.29` とリテラルのままの `869.32 * 38.29` は同じ値を返し、`0.1 + 0.2 == 0.3` は `false` になる
- 数値リテラルは自分の位置の静的型で表現できなければならない（E0417）— `Int`/`Int64` は int64 の、`Int32`/`Char` は int32 の、`Float` のリテラルは float64 で表現できる値に限る。判定の単位は式全体ではなくリテラルで、単項 `-` の直接の被演算子は符号込みで判定する（`-9223372036854775808` は `Int64` として合法）。`Go.conv` の直接の被演算子は変換先の型で判定する（`Go.conv 3000000000 : Int32` はエラー）— float リテラルの整数変換先への被演算子は Go の定数変換規則が効き、整数値でなければならない（`Go.conv 1.5 : Int32` はエラー、`Go.conv 2.0 : Int32` は合法で整数として範囲判定される）。named Go 型の変換先でも同じ — その underlying 基本型の幅と符号で判定する（`os.FileMode` は `uint32` なので `Go.conv 5000000000 : os.FileMode` はエラー、`time.Duration` は `int64` なので `Go.conv 1.5 : time.Duration` はエラー）。underlying が `float32`/`float64` ならその精度で表現可能かで判定し、underlying が数値の基本型でない（interface など）named target ではリテラル自身の型で判定する
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
| 型テスト | `s : String`, `_ : GoError` | `Go.Any` / named Go interface を対象とする `match` のみ。後述 |
| ガード | `p if cond` | 網羅性判定ではその腕を無視する |

ガード付きの腕は、網羅性判定でも冗長性判定でも「その値を覆う」とは見なさない。たとえば `Neg x if is_zero x` の後の `Neg x` は冗長ではない。

`Go.Any` または named Go interface 型の対象への `match` では、型テスト腕 `p : T` / `_ : T` が使える — Go の型スイッチ `case T:` に対応し、最初に当たった腕を実行する（型スイッチと同じく、上から順に評価され、型テスト腕は書かれた順に意味を持つ）。`p : T` は断言した型 `T` で値を束縛し、`_ : T` は束縛せず型だけで分岐する。`T` は具象型（型パラメータ・メタ変数を含まない）に限る。named Go interface を目標にした場合は名前一致ではなくメソッド集合で適合を判定する（`x : fmt.Stringer` は `String() string` を実装する任意の値に当たる）。型テストを含む `match` では:

- すべての腕が型テスト腕または末尾の catch-all（`_` または変数）でなければならない — 通常パターンとの混在は E0307
- 末尾の catch-all は必須（ない場合 E0308 — Go の型スイッチに相当する `default` が必要; `gom:allow` で緩和可能だが、どの腕にも当たらない値は網羅性違反の panic になる）
- `any` または空 interface を目標にした型テスト腕も catch-all として数え、末尾に限る
- 同じ Go 表現の目標を重複して書けない（`Char` と `Int32` は同じ `case` になるため E0309）
- ガード（`if cond`）は使えない（E0307 — 型スイッチは fallthrough しないのでガードの意味を持てない）
- 対象が `Go.Any` / named Go interface 以外のときは E0306

網羅性判定（4 節）や冗長性判定は型テスト `match` には適用しない — 開いた型集合への上から順の一致なので、Maranget 系の反例生成はしない。生成は Go の型スイッチ `switch v := x.(type)` で、catch-all は `default` に落ちる（束縛する catch-all は default 内で元の型のまま bind される）。

`_` で値を捨てる形 — `let` のワイルドカード左辺、`match` 腕パターン内の任意の位置の `_`、`ignore e`（1.1 節）— で、捨てた部分値の型が関数型のとき W0901 で警告する。`fmt.Println` 等へ関数値を*渡す*形は捨てではなく対象外。確信犯は束縛して名前を付けるか `//gom:allow W0901` で沈黙させられる（12 節）。

同じ網羅性判定を、`let` の左辺（論駁不可能性）と関数引数パターンにも使う。

網羅性が保証するのは、Gom の中で作られた値がいずれかの腕に当たることである。Go から渡された不正な値（ADT 型の `nil` など）は契約違反として扱い、`match` の時点で型名を含むメッセージで panic する（8.6 節）。

---

## 5. モジュールと可視性

1 ディレクトリ = 1 Gom パッケージ = 1 Go パッケージ。Go の `go.mod` とビルドツールをそのまま使う。

- 公開されるのは `.gomi` に書いた定義だけ（`pub` キーワードは持たない）。Go 側では先頭大文字の識別子になる（`.gomi` の `fn area` → `Area`）
- 非公開定義は Go 側で先頭小文字。Go の予約語と衝突する名前は `_` 接尾辞を付けてマングルする
- 異なる宣言が同じ Go 名を要求したときは衝突が報告される: 型名・バリアント構造体（`T_Ctor`）は verbatim で両方 emit されるためエラー E0608 — 生成 Go はコンパイル不可なので出力を書かない（ファイルをまたぐ衝突ペアは各ファイルの自分側の宣言に位置付く）。値宣言は（emit base, Gom 名）の正準順で後側が数値 suffix に押し出され警告 W0602（テスト関数はファイル内の宣言順で dedup され、非テスト宣言に base を取られたときも同じ警告）。組み込み名の同名 dedup（同名テストのファイルタグ suffix、`gomEqN` 等の内部名）は警告しない。生成されるパッケージスコープ名の一覧は `gom dump -phase=emit` で確認できる（[decisions/emit-name-collision-check](decisions/2026-11-23-emit-name-collision-check.md)・[verbatim-dup-error](decisions/2026-11-23-verbatim-dup-error.md)）
- 同じディレクトリの `.gom` ファイルは 1 つのトップレベルスコープを共有する: 型・コンストラクタ・関数・`let` はファイルの順序に関係なく相互に見える。別ファイルの同名トップレベル宣言は E0202、`package` 句の不一致は E0605（後の句に位置付く）になる。`import` 宣言はファイルローカルのまま
- `import "path"` は Gom パッケージを読み込む（Go パッケージは `import go "path"`）。公開面はパッケージの `.gomi` で決まり、import path は生成される Go パッケージのパスと一致する。メインモジュール内のパッケージはモジュールパス + 相対パスで解決し、それ以外のパッケージは import するファイルのモジュールコンテキストで `go list -m` が報告するディレクトリを見る — 依存モジュール・`replace`・ワークスペースの兄弟に `.gomi` があれば Gom パッケージとして読み込める（ない場合、またはモジュールグラフに無い場合は E0203）。vendoring 下では `vendor/<import path>` を優先して見る（`go list -m` は vendor モードで build list を返せない）。build list にあってまだ module cache に無いモジュールは `go mod download` で取得する。ツールチェーン同梱の Gom stdlib は短名で解決する: `import "list"` / `"option"` / `"result"` / `"cmp"` は `github.com/podhmo/gom/stdlib/{list,option,result,cmp}` を指す（束縛名は末尾要素の `list` など、生成される Go import は完全パス）。`Result` / `Option` / `List` の各モジュールは import なしで使える組み込みモジュールでもあり、型どうしの層変換は変換 *元* のモジュールに載る — `Result.to_option : Result a e -> Option a`（`Ok x → Some x`、`Err → None`）、`Option.to_list : Option a -> List a`（`Some x → [x]`、`None → []`）。実装は層の上側のパッケージが持つ（[decisions/stdlib-conv-placement](decisions/2026-11-18-stdlib-conv-placement.md)）
- import で束縛されるモジュール名はパスの末尾の要素（`import "example.com/t/lib"` は `lib`）。`import alias "path"` で別名を付けられる。Go 側にも同じ形がある（`import go f "fmt"`）。別名はファイルローカルのモジュール名を置き換えるだけで、公開面や他ファイルには出ない。生成される Go の import は常に束縛名を明示の別名として持つ（`import "example.com/m/lib"` は `lib "example.com/m/lib"` と生成される）— パス末尾が相手の `package` 句と食い違っても（`lib/` なのに `package other`、あるいは `math/rand/v2` なのに `package rand`）、別名は束縛名を指すので `lib.X` はそのまま参照できる（[decisions/generated-import-bound-name-alias](decisions/2026-11-06-generated-import-bound-name-alias.md)）
- `package main` の `fn main () -> Unit` は Go のエントリポイント `func main()` になり、`gom build` の成果物はそのまま `go build` できる。それ以外の `main`/`init` という名前のトップレベル宣言（引数を取る `main`、`Unit` 以外を返す `main`、パッケージ main 以外の `main`、あらゆる `init`）は `_` 接尾辞にマングルされる（`main_`/`init_`）— Go は `init`/`main` を特別扱いするため、退避しないとビルド不可か、意味のない `init` が import 時に走ってしまう（[decisions/reserved-name-escape](decisions/2026-11-19-reserved-name-escape.md)）

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

### 6.1 後片付けのスコープ `Scope`

Go の `defer` に当たる後片付けは、組み込みの `Scope` モジュールで書く（[decisions/scoped-cleanup](decisions/2026-11-21-scoped-cleanup.md)）。`Scope.run` がスコープを開き、本体のラムダが受け取るスコープハンドル `s : Scope` に後片付けを登録する。

| メンバー | 型 |
| --- | --- |
| `Scope.run` | `(Scope -> a) -> a` |
| `Scope.run_result` | `(Scope -> Result a GoError) -> Result a GoError` |
| `Scope.defer` | `Scope -> (Unit -> Unit) -> Unit` |
| `Scope.defer_err` | `Scope -> (Unit -> Result Unit GoError) -> Unit` |
| `Scope.on_err` | `Scope -> (Unit -> Unit) -> Unit` |

```gom
fn guarded (held: Ref Int) (need: Bool) -> Int =
  Scope.run <| fn s -> {
    if need {
      Ref.set held 1
      Scope.defer s (fn () -> Ref.set held 0)
    }
    Ref.get held
  }
```

1. 登録した後片付けは、本体のラムダが終わったとき（`try` による脱出と panic を含む）に登録の逆順で走る。登録は `if` や `match` の腕の中に書いてよく、その腕を抜けても本体の終わりまで生きる
2. `try` の脱出先は本体のラムダなので、`Scope.run_result` の中の `try` はスコープを閉じてから `Err` になる
3. `Scope.defer_err` の後片付けが `Err` を返したとき、本体が `Ok` ならスコープの結果はその `Err` になる。複数失敗したら実行順に `errors.Join` でまとめる。本体が `Err` なら本体のエラーを優先し、後片付けのエラーは捨てる。`Scope.run` では `defer_err` のエラーを捨てる
4. `Scope.on_err` の後片付けは、本体が `Err` を返したときと panic したときだけ走る。後片付けの `Err` では走らない
5. 後片付けの 1 つが panic しても、残りの後片付けはすべて走り、そのあと panic が伝播する
6. スコープが閉じたあとに登録すると panic する
7. `s`（とそれを束ねた別名・それを捕まえたクロージャ）をスコープの外へ持ち出すことは型エラー（E0419、[decisions/scope-escape-check](decisions/2026-11-22-scope-escape-check.md)）。型の位置では `Scope` はパラメータの葉の型か関数型の引数側の中にのみ書ける — 結果型・フィールド・コンテナ要素は禁止。使い方では、引数を保持しない呼び出し（`Scope` 操作・非脱出パラメータ・即時適用ラムダ）の中では使えるが、返り値にする・`Task.spawn` や `go {}`・Go の関数へ渡す・可変セルやコンストラクタへ格納するのはエラー。Go の関数は常に「逃げる」扱いで、非脱出パラメータはパッケージ内の推論が `.gomi` の `//gom:noescape` 注釈に書き出される。ラムダを束ねた `let` のパラメータにも同じ推論が効く。`let f = g` の別名束縛は `g` が証明した非脱出パラメータをそのまま引き継ぐ。どちらも `let` メンバのまま `.gomi` に出るときは `//gom:noescape` 行がメンバ名に対して書き出され、import 側にも同じ判定が効く。引数が保持されなくても呼出しの結果がその引数の生産物を運びうるとき（結果型が関数を含む脱出位置の呼出し）、引数ラムダの本体が返す値にも同じ脱出規則がかかる — ローカル宣言の callee ではそのパラメータの生産物が結果に載る場合に限り、callee が呼ぶ関数を介した運搬も遡って判定し（相互再帰は保守的）、外部の callee では保守的に検査する。パラメータの生産物が結果に載らない位置は `.gomi` の `//gom:result-notaint <name> <i>` 注釈に書き出され、import 側にも同じ判定が効く — 注釈のない位置は載る保守扱いで、未検査の外部 callee と同じ判定になる
8. `run_result` / `defer_err` が合流するのは `GoError` だけ。後片付けの失敗をドメイン独自のエラー型 `E` で扱いたいときは、`run_result` の結果を `|> Result.map_err f` で橋渡しする。後片付け自体が `E` を返す場合は、`defer_err` のコールバック内で `E -> GoError` に変換して登録すれば `Close` の合流に乗る。取得と登録を一度に書く糖衣はユーザー側の helper 関数として書ける — `s` は引数を保持しない呼び出しに渡せるので:

   ```gom
   fn closing (s: Scope) (open: Unit -> r) (close: (r -> Unit)) -> r = {
     let x = open ()
     Scope.defer s (fn () -> close x)
     x
   }
   ```

   スコープ値をエラー型で型パラメータ化する版・組み込みの取得+登録糖衣は future work（[decisions/scope-cleanup-extension](decisions/2026-11-23-scope-cleanup-extension.md) 案 D）。

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

1. 1 パッケージに `.gomi` は 1 つ（`<パッケージ名>.gomi`）。書けるのは `import` 宣言・型定義・関数シグネチャ・定数の型と `///` のドキュメントコメントだけで、式は書けない。シグネチャで他パッケージの型や Go の型を指すには `.gom` と同じ形の `import`（Gom パッケージ、`import go`、別名付きのいずれも）を書く — その名前はこの `.gomi` のシグネチャを読むためだけのスコープで、公開面にも実装側の `.gom` にも出ない（公開される型は完全な import path の型として記録されるので、利用側は `.gomi` の import 名を引き継がない）。`.gom`（実装側）の import を通る循環は `.gom` 同士と同じく経路付きの E0606 になる。`.gomi` 同士だけの循環も読み込み中の再入として検出され、同じく経路付きの E0606 になる（順序づけ不能なまま読み込みが止まるため）。ただし `.gomi` の辺だけが逆向きに依存する場合 — 例えば `a.gom` が `b` を import し `b.gomi` が `a` を import — は実装側の import グラフを循環させないので循環とは扱わない。公開した定数は Gom 側では不変の束縛だが、生成される Go では package 変数（`var`）として見える — Go 側からの書き換えは 8.7 節の契約が禁じる。定数の型は型変数を含んでよい（`let empty : List a`）— 一般化された値を公開する宣言で、実装側の `.gom` が一般化に至る `let`（`let empty : List a = []`）と同じ niladic のジェネリック関数（`func Empty[T]() T`）として emit される。利用側の `p.empty` という参照は値として型付き、その呼び出し `p.Empty[T]()` に写る
2. 関数は `.gom` と同じ引数リストの形で書く（引数の数とラベルがインターフェースの一部になる）
3. sum type はコンストラクタまで書けば透明、名前だけ書けば抽象。利用側で `match` できるのは透明な型だけ
4. ライブラリパッケージ（`package main` 以外）は `.gomi` が必須で、ないとエラー
5. `gom info` は、`.gomi` がまだないパッケージについて、全トップレベル定義のシグネチャを型推論の結果から書き出した下書きを作る
6. 実装の型はインターフェースと一致しなければならない（v1 では型変数の名前の違いだけを許す）。`fn` 項目の実装側は本体を持つ定義でも `@extern fn` のシグネチャ宣言（1 章 9 項）でもよい — 適合検査はどちらにも同じく行う
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

（`List.length` は `stdlib/list` として実装済み（P4-8）。`strings.Join` への `List String` 引数は 8.5 節の `[]T` ↔ `List` 境界コピーを通る）

1. 0 引数の Go 関数は `()` を受け取る（`time.Now()`）。1 引数なら `strconv.Itoa(n)` と `strconv.Itoa n` は同じ意味。値として束ねた 0 引数関数の型は `-> T`（Go シグネチャそのままの形であって `Unit -> T` ではない）になり、`let f = e.Error` のあとの `f ()` は Go の `f()` 呼び出しになる — `()` は引数ではなく呼び出しの印で、型は適用結果の `T` になる。`Unit -> T` の Gom 関数の `f ()` はこれと別物で、本物の Unit 引数を渡す普通の適用になる（生成される Go は `f(struct{}{})` — `()` が呼び出しの印になるのは `-> T` の値に対してだけ）
2. 値として渡せる（`List.map strconv.Itoa xs`）
3. メソッド呼び出しは `x.M(args)`。Go の型の値にだけ使え、Gom の型はメソッドを持たない。`x.M(1).N(2)` のような連鎖は Go と同じく左結合で `(x.M(1)).N(2)` と読む（括弧つき引数に続く `.` は呼び出し結果への選択子になる。[decisions/post-arg-selector-binding](decisions/2026-11-05-post-arg-selector-binding.md)）
4. 可変長引数は個数自由に渡せる（`fmt.Println(a, b, c)`）。リストを展開するときは `fmt.Println(..xs)`。`..e` は Go と同じく可変長引数の全体をそのリストで渡すので、固定引数（`fmt.Sprintf("%s", ..xs)`）との併用はできるが、同じ可変長パラメータに入る引数とは併用できない（`fmt.Println("tag:", ..xs)` は E0505）
5. 数値型の暗黙変換は、Go の `int` / `int64` と Gom の `Int`、`float64` と `Float` のように情報が落ちないものに限る。それ以外は 8.4 節の `Go.conv` で明示する。Go の型なし定数（`math.Pi` など）は、期待される Gom の型になる
6. Go のジェネリック関数は、制約が `any`、`comparable`、型パラメータを項に持つ構造制約（`~[]E`、`~map[K]V`）、または型パラメータを含まない union（`~int | ~string` など）のものが呼べる。union 制約は `interface{ cmp.Ordered }` のように名前付き union interface を包んだ綴りも同じ型集合として扱う。型引数は宣言順に推論される。`S ~[]E` の `s S` は `Go.Slice E` パラメータとして読む — `List` 引数は境界のコピーを通り、制約だけに現れる `E` はその要素から推論される（`slices.Contains`、`maps.Equal`）。制約の判定は出力する Go の型で行う — `Int` の repr は `int64` なので `~int64` は満たすが `~int` は満たさない。union の項が slice / map を指すとき（`~[]byte`、`[]byte | string`）`List` は型引数になれない — 出力が named な `list.List[T]` で項に属さず、素通しは callee の書き込みが backing に届く。それ以外の制約 — メソッドを持つ interface、近似項・union 項の交叉、型パラメータを含む union — を持つ関数は公開されない（8.5 節）
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
| 戻り値 `(T, error)` | `Result T GoError`。`T` がメソッドを持つ interface 型の場合（`(error, error)`、`(net.Error, error)`、ユーザ定義 interface）は `Result (Option T) GoError` — Ok 側のペイロードも nil 許容で、nil interface は `Ok None` として読む（メソッド呼び出しが nil で panic するので、素のまま `Ok` に入れると観測できない穴になるため）。`any` や named な空 interface は verbatim の `Result T GoError` のまま — nil は値として素通しできる |
| 戻り値 `(T, bool)` | タプル `(T, Bool)`。`Option T` にするのは、標準のバインディングか `extern` で comma-ok と宣言した関数だけ（`os.LookupEnv` など） |
| 戻り値 `(A, B)`（上記以外の多値） | タプル `(A, B)` |
| 戻り値 `(T, U, ..., error)`（3 個以上・末尾が `error`） | `Result (T, U, ...) GoError` — ペイロードはタプルに畳む（[decisions/nary-go-results](decisions/2026-11-18-nary-go-results.md)）。ペイロードのタプル内でメソッドを持つ interface 型の要素は `Option` に包まれ、その位置の nil は `None` と読む（`(T, error)` の規則の要素ごとの適用） |
| 戻り値 3 個以上（末尾が `error` でないもの） | タプル `(A, B, ...)` — 8 個まで。9 個以上は公開されない |
| 戻り値 `error` のみ | `Result Unit GoError`（既定の型由来ビュー）。`extern` 宣言か標準バインディングで `GoError`（nil を返さない契約 — 違反は境界で panic）か `Option GoError`（nil 許容のエラー値）にもなる（8.4 節） |
| `*T` | `Ptr T`（nil の参照は Go と同じく panic。操作は `Ptr` モジュール） |
| `int` / `int64` / `int32`（`rune` もここ） / `float64` | `Int` / `Int` / `Int32` / `Float` |
| `byte` / `uint8` | `Byte` |
| named type（`type UserID int64` など） | 別の名前的な型 `pkg.UserID`。基底型との変換は `Go.conv` で明示。underlying が数値の基本型なら 3.3 節の named 算術・比較が使える |
| `[]T` | `List T`。Gom 側の境界変換位置では両方向ともコピーする（8.5 節） |
| `map[K]V` | 不透明型 `Go.Map K V`（操作は `Go.Map` の型メンバー群） |
| `chan T` / `<-chan T` / `chan<- T` | `Chan T` / `RecvChan T` / `SendChan T` |
| interface（`io.Reader`、`fmt.Stringer` など） | 名前的な型 `io.Reader` など。Go 由来の値を代入規則に従って渡せる |
| `error` | `GoError` |
| `any` / `interface{}` | `Go.Any` |

`Char` はこの対応からは現れない。Go の `rune` は `int32` の別名なので `Int32` として読む（`unicode.IsUpper` の引数は `Int32`）。`Char` は `'a'` リテラルか、Go の型なし rune 定数（既定は `Char`）、または `Go.conv` で得る — `Char` の Go 表現も `rune` なので `Go.conv 'A' : Int32`・`Go.conv x : Char` の両方向が変換できる。

`Byte` もリテラルを持たない。`byte`（`uint8`）の値は `[]byte` 由来の `List Byte` / `Go.Slice Byte` の要素として Go 境界から得るか、`Go.conv` で他の数値型から変換する（`Go.conv n : Byte` は 0–255 の範囲で判定し、`Go.conv 'a' : Byte` のような rune 変換も Go の規則に従う）。比較は `==`/`!=` のみ — `<` や四則演算は `Go.conv` で `Int` などに変換してから行う。

v1 で対応していない型（`byte`/`uint8` 以外の符号なし整数と `int8`/`int16`、`float32`、複素数、配列 `[N]T`、`unsafe.Pointer`、ジェネリック型、8.1.6 節の形でない制約（メソッドセット・交叉・型パラメータを含む union など）を持つジェネリック関数、メソッドを持つ無名 interface、8.8 節の `Go.FuncN` の形を超える func 型の引数・戻り値）をシグネチャに含むシンボルは公開されず、参照すると理由付きの診断になる（8.5 節）。戻り値が 9 つ以上ある関数も同じく公開されない — 多値のタプル対応は 8 要素までで、末尾が `error` の多値は `Result` に畳まれる（8.4 節）。

Go の名前付き struct 型の値またはそのポインタから、exported field を `value.Field` で読み取れる。Go と同じ promoted field と曖昧性の規則を使い、unexported field は参照できない。field の型は上の対応に従い、未対応の型は参照位置で理由付き E0501 になる。読み取りはソースの評価順に行い、nil ポインタ経由の読み取りは Go と同じく panic する。

`Ptr T` からの exported field への書き込みは `ptr.Field = v` と書く（T は Go の名前付き struct 型に限る）。代入の左辺はこの field selection の形だけで、変数や record のフィールドへの代入は E0511 になる — record は不変で、変数の再束縛はない。`Ptr` でない Go struct 値経由の代入はコピーの書き換えで効果が落ちるため、同じく E0511 にする。代入式は `Unit` に型付き、受取側の field の評価のあと値を評価し、値の型は読み取りと同じ対応（コピー規則を含む）で Go の field 型へ変換する。

Go struct のコンポジットリテラルによる構築は提供しない — 構築は `Ptr.new ()` からの field への代入か、Go 側の factory 関数（`extern` や `import go` で見える普通の関数）で行う（[decision](decisions/2026-11-17-struct-construction.md)、[decision](decisions/2026-11-19-struct-field-write.md)）。

`Ptr T` の操作はコンパイラ組み込みの `Ptr` モジュールが持つ（[decisions/ptr-ops-surface](decisions/2026-11-05-ptr-ops-surface.md)）: `Ptr.get : Ptr a -> a`（`*p` — nil で panic）、`Ptr.set : Ptr a -> a -> Unit`（`*p = v` — nil で panic）、`Ptr.is_nil : Ptr a -> Bool`、`Ptr.new : Unit -> Ptr a`（`new(T)`。`errors.As` の target や decode 先のような out-pointer API 用）、`Ptr.make : a -> Ptr a`（`&v` — 新しいコピーへのポインタ）、`Ptr.nil : Unit -> Ptr a`（`tls.Dial` の `*tls.Config` のような任意の `*T` パラメータへ nil を渡す）、`Ptr.to_option : Ptr a -> Option a`（nil なら `None`、さもなくば `Some *p` — 安全な読み出し）。`Ptr` の `==`/`!=` は Go のポインタの `==` と同じ同一性比較（3.3 節の等価規則）。nil との比較は `Ptr.is_nil` / `Ptr.to_option` で行う。

`Ref a` の操作はコンパイラ組み込みの `Ref` モジュールが持つ（[decisions/ref-members](decisions/2026-11-19-ref-members.md)）: `Ref.new : a -> Ref a`（初期値を持つセルの確保 — OCaml の `ref x` 相当）、`Ref.get : Ref a -> a`（読み出し）、`Ref.set : Ref a -> a -> Unit`（書き込み）。出力される Go は `*T` — `Ptr a` と同じ表現だが、nil を持たない常に初期化済みの可変セルとして別の型で、`is_nil`/`nil`/`to_option` の面はない。`Ref` の `==`/`!=` は `Ptr` と同じく同一性比較（3.3 節）。`Ref` は Gom 内部の可変セルで、Go 境界の `*T` の読みは `Ptr a` が担う — `Ref a` を `*T` のパラメータに渡す変換はなく、逆向きにも `Ptr` は `Ref` にならない。

`Go.Map K V` の操作は `Go.Map` の型メンバーとして解決する（[decisions/go-map-ops](decisions/2026-11-05-go-map-ops.md)）: `Go.Map.make : Unit -> Go.Map k v`、`Go.Map.get : Go.Map k v -> k -> Option v`（missing key は `None`）、`Go.Map.set : Go.Map k v -> k -> v -> Unit`、`Go.Map.delete : Go.Map k v -> k -> Unit`、`Go.Map.length : Go.Map k v -> Int`、`Go.Map.to_list : Go.Map k v -> List (k, v)`（順序不定のスナップショット。v1 の反復面）。`k` は Go の `comparable` 制約を持ち、`List` など比較できない表現のキーは E0508 で拒否される。nil マップの意味は Go のまま — get/delete/length は nil でも動き、set は panic する。

`Go.Slice T` の操作も `Go.Slice` の型メンバーとして解決する（[decisions/go-slice-ops](decisions/2026-11-05-go-slice-ops.md)）。すべて読み取り専用 — Gom 側から Go が所有するスライスを書き換える操作はない: `Go.Slice.length : Go.Slice a -> Int`（nil では 0）、`Go.Slice.get : Go.Slice a -> Int -> Option a`（範囲外・負の添字は `None`）、`Go.Slice.slice : Go.Slice a -> Int -> Int -> Go.Slice a`（`s[lo:hi]` — バッキングを共有する部分ビュー。範囲外・逆転した境界は Go と同じく panic）、`Go.Slice.fold : Go.Slice a -> (b -> a -> b) -> b -> b`（左畳み込み。nil は acc のまま）、`Go.Slice.to_list : Go.Slice a -> List a`（スナップショット — コピーするのはこのメンバーだけ）。`Go.Slice` は `for x in s { ... }` の反復対象にもなる — Go の `range` と同じく添字順に要素を読み、nil は 0 回反復する（[decisions/for-over-go-slice](decisions/2026-11-05-for-over-go-slice.md)）。

`Array T` の操作はコンパイラ組み込みの `Array` モジュールが持つ（[decisions/cons-buffer-array-extern-impl](decisions/2026-11-18-cons-buffer-array-extern-impl.md)）。`Array a` は可変の構築バッファで、出力される Go は `[]T` — cons を積み上げる `List` 構築が O(n²) になるのに対し、添字への書き込みで線形の組み立てができる: `Array.make : Int -> a -> Array a`（`Array.make n init` — n 個の `init` のコピー。ADT 要素に正当なゼロ値は存在しない（Go の nil は宣言されたバリアントでない）ので初期値は暗黙に選べない — 必須引数にした（[decisions/array-make-init](decisions/2026-11-21-array-make-init.md)）。負の長さは Go と同じく panic）、`Array.length : Array a -> Int`、`Array.get : Array a -> Int -> a`（範囲外は panic）、`Array.set : Array a -> Int -> a -> Unit`（同）、`Array.from_list : List a -> Array a`（コピー — 配列への書き込みは元のリストへ届かない）、`Array.to_list : Array a -> List a`（コピーなしの凍結 — リスト化したあと同じ配列へ書いてはいけない。違反は 8.6/8.7 節の契約違反であり、診断はされない）。

`View T` は `List` 由来の読み取り専用の部分ビュー型（[decisions/list-view](decisions/2026-11-18-list-view.md)、MoonBit の `StringView` に相当）。`List` のバッキングを共有するので、部分取り出し・添字アクセスがコピーなし O(1)。面は `List.view : List a -> View a`（全体のビュー）、`List.slice : List a -> Int -> Int -> View a`（`xs[lo:hi]` — 境界違反は Go と同じく panic）、`View.length : View a -> Int`、`View.get : View a -> Int -> Option a`、`View.slice : View a -> Int -> Int -> View a`、`View.fold : View a -> (b -> a -> b) -> b -> b`、`View.to_list : View a -> List a`（不変どうしの共有 — O(1)）。`View` は `for x in v { ... }` の反復対象にもなる。`View` を作れるのは `List` からだけ — Go のスライス由来の共有 View は `Go.Slice` の読み取り専用面が担うので足さない（[decisions/goconv-list-slice-copies](decisions/2026-11-04-goconv-list-slice-copies.md)）。`View` と `List`/`Go.Slice`/`Array` の間の暗黙変換はない。

`Go.Any` は Go の `any`（`interface{}`）の綴りとして採用した（[decisions/go-any-spellable](decisions/2026-11-06-go-any-spellable.md)）。型式の書けるすべての位置 — シグネチャのパラメータ・結果（`.gom`/`.gomi`/`extern`）、型注釈、`List Go.Any`・`Option Go.Any` などの型引数位置 — で認める。裸の `any` は小文字識別子なので型変数と読む（`fn f (x: any) -> any` は多相関数）— `any` の綴りは `Go.` 修飾の `Go.Any` だけ。`type Token any`（定義型 — `json.Token` がこの形）のような named な `any` は名前的な型のままで、`Go.Any` とは別の型として扱う。`type alias T = Go.Any` のエイリアスは `Go.Any` そのものに展開され、`any` に写る。want が `Go.Any` の位置（`Go.Any` パラメータへの引数、`Go.Any` の型注釈、`Go.Any` を宣言した record フィールドや sum ペイロードの構築位置、`-> Go.Any` の本体）にはどの Gom の値でもそのまま置け、境界で `any(x)` のボックスが生成される（数値は Gom の型の Go 表現へ先に物質化する — `any(int64(42))`。8.1.7 節）。型引数の位置では `Go.Any` は受理規則ではなく実引数の選択になる — Go のジェネリックは型不変なので `Option Go.Any` は `Option[any]` だけを指し、`Option Go.Any` のスロットに `Some 42` を置けば payload は `any` 要素へボックスされるが、既に `Option Int` と型付いた値は合わない（`Option[int64]` は `Option[any]` ではない）。関数型のパラメータ・結果も同じで、`Go.Any` は emit される Go 型のマーカー — `any -> Int` と `String -> Int` は `func(any) int64` と `func(string) int64` で型不変なので、型が要求される位置（注釈・パラメータ・match の各スロット）ではどちら向きにも置き換えられない（E0401）。`any -> Int` のシグネチャを持つ callee へ `String` の値を渡せるのは適用サイトの引数 conform が値スロットで働くからで、型同士の代入可能性ではない — `let g : Go.Any -> Int = fn x -> 1` のような期待位置では `x` は `any` に bind されて `func(x any) int64` が生成される。スカラの `Go.Any` スロットは受理するだけでインスタンス化を選ばない — `let x : Go.Any = None` の `None` の要素型はそこからは決まらず、注釈が要る（E0407; `Option Go.Any` という書き方が合成の答え）（[decisions/go-any-conformance-materialize](decisions/2026-11-06-go-any-conformance-materialize.md)）。逆向き — `Go.Any` から具象型への取り出し — は Go の型アサーションに相当し、`Go.conv` の変換では認めない（E0507）。取り出しの唯一の面は `Go.try_as x : Option T` — `x` は `Go.Any` に限り、目標 `T` は `Go.Any` 自体や型パラメータを含まない具象型に限る（いずれも違反は E0514）。生成は comma-ok の型アサーション `v, ok := x.(T)` を `option.FromGo` に通す形で、panic 版は `Option.get` で導出する（[decisions/go-any-extraction](decisions/2026-11-24-go-any-extraction.md)）。

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
  - `T` → `T`（対応する型）、`*T` → `Option (Ptr T)`（`Option` はポインタのまま保持する）、interface 型の結果 → `Option T`（`Option io.Reader` のような `Option <interface 型>` — nil を `None` と読む）
  - `error` → `Result Unit GoError`（既定）、`Option GoError`（nil を返し得る値）、`GoError`（nil を返さないという契約の宣言 — Go の型では表せないので境界で検査し、nil が来たら panic する）
  - `(T, error)` → `Result T GoError`。第 1 結果が nil 許容な型（`*T` または任意の interface 型 — `error`、`net.Error`、`io.Reader`、named な空 interface を含む）のときは `Result (Option T) GoError` も宣言できる — 第 1 結果の `nil` を `Ok None` と読む。第 1 結果がメソッドを持つ interface 型のとき、verbatim の `Result T GoError` 宣言（`(error, error)` への `Result GoError GoError`、`(net.Error, error)` への `Result net.Error GoError` など）は「第 2 が nil なら第 1 は真の値」という契約で、`(nil, nil)` は境界で panic する（型由来の既定は `Result (Option T) GoError` — 8.3 節）
  - `(T, bool)` → `Option T` または `(T, Bool)`
  - その他の `(A, B)` → `(A, B)` のタプル
  - 3 個以上の結果（8 個まで）: 末尾が `error` の `(T, U, ..., error)` → `Result (T, U, ...) GoError`（ペイロードはタプル）。タプルの各要素は verbatim で、nil 許容な型（`*T` または任意の interface 型）の要素は `Option T` とも宣言できる — ただしメソッドを持つ interface 要素の verbatim は宣言できない（`(T, error)` pair の never-nil 境界検査に相当するガードをタプル内には emit しないため、その要素は `Option` を綴る）。末尾が `error` でない多値は `(A, B, ...)` のタプルのみ — `Option` 要素は書けない
  - 9 つ以上の結果を持つ関数は `extern` の対象外 — 書いても E0509 で照合に落ちる（`extern` は公開面の見え方を変えるだけで、公開範囲そのものは広げない）
- 宣言に型変数は書けない（Go の関数は多相ではない）。ジェネリックな Go 関数の上書きも現時点ではできない。

宣言した型が Gom 側で見える型になり、生成される呼び出しは生の Go シグネチャのまま `pkg.F(args)` で、結果は宣言した変換規則で物質化される — `(T, bool)` の `Option` は `v, ok := f(...)` の後 `option.FromGo(v, ok)`、`*T` や interface 型への `Option` 宣言は `v := f(...)` の後 `option.FromGo(v, v != nil)`。`Result (T, U, ...) GoError` は `v0, v1, ..., err := f(...)` の後、ペイロードを `rt.TupleN` に詰めて `result.FromGo(t, err)` で物質化し、`Option` 要素はその位置で `option.FromGo(v_i, v_i != nil)` が走る。`error` への `Option GoError` 宣言も同じ `option.FromGo(v, v != nil)` 形で、`GoError` 宣言は `if v == nil { panic(...) }` の境界検査を生成する。`(T, error)` への `Result (Option T) GoError` 宣言はペイロードを `option.FromGo(v, v != nil)` で物質化し、メソッドを持つ interface 第 1 結果への verbatim `Result T GoError` 宣言（`Result GoError GoError` など）は `if err == nil && v == nil { panic(...) }`（`try`/`|?>` では `err` の脱出のあと `if v == nil { panic(...) }`）の境界検査を生成する。

コンパイラが用意する**標準バインディング**は extern と同じ規則で宣言ビューをあてがう既定の上書きである（[decisions/error-result-bindings](decisions/2026-11-05-error-result-bindings.md)）。`errors.New` と `fmt.Errorf` は `-> GoError`（エラーを値として直接組み立てられる）、`errors.Join` と `errors.Unwrap` は `-> Option GoError` と見える。明示的な `extern` は標準バインディングに優先し、生の Go シグネチャと照合される — `extern go "errors" { fn New (msg: String) -> Result Unit GoError }` は型由来の既定に戻せる。

`errors.As` のように Go の型では表せない契約（`target` は `*T` で、`T` が `error` を実装するか interface 型 — そうでなければ panic）は型付きビューでは追えない。`errors.As` は型由来の `(GoError, Go.Any) -> Bool` のまま公開され、リンターが証明できる違反に W0502 を警告する（[decisions/errors-as-target-lint](decisions/2026-11-05-errors-as-target-lint.md)）: `Ptr` でない引数、`Ptr.nil`/`Go.nil` リテラル、`Ptr E` で `E` が interface にも `error` の実装にもなり得ない具象形状（`Int` などの組み込み型、関数・タプル型、宣言された record）。`Ptr (Ptr E)` は `*E` が named 非 interface 型のメソッド集合を持ち得ない場合 — E が unnamed 表現か interface 表現（宣言 sum・`Option`・`GoError`・`Go.Any`）のとき — も警告する（[decisions/errors-as-nested-ptr-lint](decisions/2026-11-19-errors-as-nested-ptr-lint.md)）。named Go 型の `Ptr` — 意図した綴り — と `GoError`/`Go.Any` の要素、宣言された sum（表現は interface）、`Ptr (Ptr E)` で E が named Go 型・record・abstract・未解決メタの場合は警告しない。

明示的な変換は組み込みの `Go.conv` で書く: `Go.conv user_id : Int`。変換できるかどうかは Go の変換規則で検査する。1 フィールド record（`type X = {f: F}` — Go では defined type `type X F`、8.7 節）は F に剥がれてから検査される: `Go.conv u : Int`・`Go.conv n : UserId`（`type UserId = {value: Int}`）は `int64(u)`・`UserId(n)` と emit される。剥がれたオペランドは Go の名前付き型なので、通るのは名前付きオペランドでも `T(x)` が合法なペアに限る — underlying が一致するペアと、両側の underlying が数値の基本型のペア（`float64(u)`、`type D = {d: time.Duration}` に対する `int64(d)` — 名前付き Go underlying は oracle が調べる）。それ以外が F に対して許す規則 — `List`↔`Go.Slice` の境界コピー（`T(x)` キャストはバッキングを共有して `List` の不変性を破る）、2 つの名前付き channel ラッパ間の向き変更、interface への適合（defined type はメソッドを持たない — `X` は `fmt.Stringer` を実装しない） — は defined type 経由では E0507 になる。剥がれは連鎖する（`type M = {u: UserId}` は `Int` まで剥がれる）。リテラルが変換先に直接渡されるときの範囲判定も剥がれた型で行う（`type U32 = {v: Int32}` に `Go.conv 3000000000 : U32` は E0417）。複数フィールドの record・sum・フィールドが裸の型パラメータの record（`type Box a = {item: a}` — struct フォールバック）は剥がれず、他の declared 型と同じく E0507 になる。`Go.Slice T` と `List T` の間の `Go.conv` は、8.5 節の境界変換と同じくコピーする（[decisions/goconv-list-slice-copies](decisions/2026-11-04-goconv-list-slice-copies.md)）。このペアは要素型が一致するときに限る — `Go.Slice Int64` → `List Int` や `Go.Slice Int32` → `List Char` のように Go の表現では変換できても Gom の要素型が違うものは E0507 で、そのままの名前付きキャストはバッキングを共有して `List` の不変性を破るため（[decisions/goconv-slice-list-element-mismatch](decisions/2026-11-05-goconv-slice-list-element-mismatch.md)）。underlying が `[]repr(T)` の named Go スライス型（`sort.Float64Slice` など）と `List T` の間も同じ — Go の変換規則で受理されるペアなら、境界を一度コピーしてからキャストする（`B(xs.ToSlice())`・`list.FromSlice([]T(b))`）。要素の Go 表現が合わないペア（`List Int` と `sort.IntSlice` — `[]int64` と `[]int`）は E0507。

### 8.5 境界でのデータの扱い

- `List T` と Go の `[]T` は、Gom のコードの中で `[]T` と `List` の view が切り替わる位置で両方向ともコピーする。切り替わるのは、Go 呼び出しの引数と結果、Go の exported field・パッケージ変数の読み取り、`List` と `Go.Slice` が適合する型の位置（型注釈・戻り値・宣言されたパラメータ・record のフィールドなど）、`Go.conv`、`Chan.send` などの組み込みの要素位置。Gom から渡したスライスを Go が書き換えても、Go から受け取ったスライスを Go が後で書き換えても、Gom の `List` は変わらない。コピーするのはスライスの 1 段目だけ — 要素の可変性は要素型の契約に従い、`[][]T` の読み取りは内側のスライスが Go のバッキングを共有する `List (Go.Slice T)` を返す（`Ref`・`Go.Map` などの要素も同様）。この規則は Gom が Go を呼ぶ側のもので、生成した API を Go から直接呼ぶ面では値はコピーされずに受け渡される — その側の契約は 8.7 節
- コピーを避けたいときは、Go が所有する可変の値として `Go.Slice T` を使う（`extern` で宣言する）。`List` を `Go.Slice` パラメータへ渡してもやはりコピーする — no-copy は `Go.Slice` から `Go.Slice` への引き渡しだけ。`Go.Slice` の値のまま読むための操作は `Go.Slice` の型メンバー群（8.3 節。`length`/`get`/`slice`/`fold`/`to_list` — すべて読み取り専用）が持つ
- このコピーのため、`sort.Strings xs` のような in-place 変更を契約に持つ Go 呼び出しはコンパイルできても Gom の値に効かない。結果が `Unit` の Go 呼び出しがコピーされる引数を `[]T` パラメータへ渡すとき、リンターは W0501 を警告する（[decisions/go-mutation-warning](decisions/2026-11-05-go-mutation-warning.md)）。コピーされるのは `List` 引数（`..xs` スプレッドと `Go.Slice` 宣言パラメータへの `List` を含む）、`List` 位置へ物質化される `Go.Slice` 引数（チャネル送信の `[]T` パラメータなど）、それに引数位置で `List` をコピーする `Go.conv xs : Go.Slice T` のインライン変換。素の `Go.Slice` が `[]T` スロットへそのまま渡る経路 — `Go.Slice` 宣言パラメータへの `Go.Slice` 引数、`..gs` スプレッド、`...[]T` の要素位置 — はバッキングを共有するので警告しない。読み取り専用の消費者は値を返すので（`strings.Join` など）警告されない。`signal.Notify` のように副作用を持つが slice を書き換えない Unit 関数も警告対象になり得る — 警告の文言は条件形で、事実としてコピーが起きることだけを述べる
- Gom が表現できない型（`unsafe.Pointer`、8.1.6 節の形でない制約を持つジェネリック関数など）を含むシンボルは公開しない。`import go` 自体は失敗せず、そのシンボルを参照したときに理由付きの診断を出す
- Go のジェネリック関数は、8.1.6 節の制約のものを呼べる。型引数は宣言順に推論される — `S ~[]E` / `M ~map[K]V` のスロットは `Go.Slice` / `Go.Map` パラメータとして読まれるので `List` 引数はコピーされ（in-place 変更の契約を持つ呼び出しは W0501 の対象）、`S` を返す関数の結果は `Go.Slice` になる。制約を満たさない実引数は E0508（判定は出力する Go の型で行う — `Int` は `~int64` を満たし `~int` は満たさない）、扱えない制約を持つ関数の参照は E0506 で報告される。`~[]E`/`~map[K]V` の引数が Go に写せない要素 — Gom の宣言型 — を持つ `Go.Slice`/`Go.Map`/`List` のとき、要素どうしを Gom の型等価で比較して判定する（同一生成パッケージでは等しい Gom 型は同じ Go 型に emit される — `slices.SortFunc` に `List Rec` と `Go.func2 cmp` を渡せる）。union の項が `~X`（approx 項）のときは、defined type に emit される宣言型（1 フィールド record — `type UserId = {v: Int}` → `type UserId int64`）も型引数になれる — フィールドの emit される underlying で判定し、`type UserId int64` は `~int64` を満たす。exact 項は defined type を含まない — `UserId` は `int64 | string` のような union や `T int64` の制約を満たさない（Go の defined type と同じく、exact 項は同名の型自身だけを含む）。フィールドが `Array T`（`[]T` に emit される）の defined type や直接の `Array T` 型引数は `[]T` の綴りで判定する — `type Ws = {xs: Array Byte}` は `~[]byte` を満たす。ファイルが builtin の型名を再宣言した綴り（`type Int = {x: Bool, y: Bool}` を剥がして得た `Int` など）は builtin と読み違えられるので棄却する。`comparable` の判定も出力する Go の型で行う — interface 表現に emit される宣言型（sum・`Option`）は payload まで畳んで判定する: Go では静的に comparable でも、非 comparable な payload を持つ variant／要素の `==` は実行時に panic するので、全 variant の全フィールド（sum）・要素型（`Option`）が comparable のときのみ制約を満たす。再帰的な宣言型はその再帰自体を comparable として残りのフィールドで判定し、`Task` は handle（chan と pointer）の比較なので常に comparable、import した宣言型のフィールドも同じ規則で畳む — そのフィールド内で非修飾に書かれた型名は import 先パッケージの builtin を指し、呼び出し側ファイルの shadow 宣言は届かない。実装集合の見えない interface（`GoError`・`Go.Any`・named Go interface）は Go と同じ受理のまま残す — 動的値が非 comparable のとき panic するリスクは手書き Go と同様に残る

### 8.6 nil と不正な値

- Go から受け取った Gom の ADT 型の値が `nil` などの不正な値だった場合は契約違反とし、`match` の時点で型名を含むメッセージで panic する
- 名前付きペイロードのフィールド参照 `s.f` も同じ族である — 値がそのフィールドを宣言するバリアントを保持していなければ契約違反として panic する（3.2 節）。これは Gom が作った正当なバリアント値にも起きる（値は正しくとも、期待するバリアントではない）
- Go の interface 型やポインタ型の値は `nil` になり得る。メソッド呼び出しや参照で Go と同じく panic する。`*T` の既定は `Ptr T`（[decisions/ptr-t-default](decisions/2026-11-06-ptr-t-default.md)）。`Ptr T` の nil 判定は `Ptr.is_nil`、安全な読み出しは `Ptr.to_option`、nil の供給は `Ptr.nil` で行う（8.3 節）。`Ptr` 以外の nil-able な型 — `Go.Slice`・`Go.Map`・`Chan`・`Go.Any`・`GoError`・Go の関数型・underlying が interface・スライス・マップ・チャネル・関数・ポインタの named Go 型（`os.DirEntry`・`sort.IntSlice` など）— の nil は `Go.nil : Unit -> a` で書く。結果型は呼び出し位置の期待型から決まるので `Go.nil ()` は それ単体では型が定まらず、確定できない場合は注釈が要る（E0407）。nil を持てない型への適用は E0513（[decisions/nil-spelling](decisions/2026-11-23-nil-spelling.md)）

### 8.7 Go から Gom を呼ぶ

生成コードは普通の Go パッケージなので直接 import できる。生成コードは「読めて、デバッグでき、Go から自然に使える成果物」であり、人が直接書き換えて保守するソースではない。sum type は sealed interface として公開され、Go 側では type switch で分解する（網羅性は Go 側では保証されない）。ただし `Result a e` は例外で、非公開タグ付きの struct として公開し、`Ok` / `Err` のコンストラクタ関数と `Tag()` / `Unwrap()` で扱う。

この面では 8.5 節のコピーは置かれず、値は Gom の型の Go 表現（8.3 節）そのままで受け渡される — `List` の引数・戻り値は `list.List[A]`（`[]A` の名前付き型）のバッキングを共有し、record やバリアントの `List` ペイロードも同じである。`.gomi` で公開した `let`（7 章の定数）は Go の package 変数として公開される（型変数を含む宣言は niladic のジェネリック関数）— Go には読み取り専用の exported 変数がないので、不変の束縛は `var` の形で見える。

record 型はフィールドごとの exported field を持つ Go の struct として公開される。ただしフィールドが 1 つだけの record は Go の defined type（`type UserId int64`）として公開する — Go 側からは `UserId(7)` で構築し、`int64(u)` で中身を取り出す（フィールド名は Go 側から見えない）。例外として、唯一のフィールドの型が裸の型パラメータのとき（`type Box a = { item: a }`）は Go が `type Box[A any] A` を許さないため struct のままになる（[decision](decisions/2026-11-21-record-single-field-defined-type.md)）。

Gom の値の不変性は Gom 側の操作が守る契約であり、境界は Go 側の書き込みを防がない（[decisions/boundary-mutation-contract](decisions/2026-11-05-boundary-mutation-contract.md)）。Go 側は、受け取った `List` の要素を書き換えてはならず、export された `let` の変数へ代入してもいけない。`Ref`・`Array`・`Ptr`・`Go.Slice`・`Go.Map`・チャネル・関数値のように Gom の中でも可変または共有される表現を持つ値は、両側でそのまま共有される。違反は 8.6 節の契約違反であり、診断はされない — 違反したプログラムについて Gom の保証は効かない（OCaml の FFI が C 側に immutable ブロックへの書き込み禁止を委ねるのと同じ立場）。

### 8.8 Go の func 型と関数値の変換

Go の API が引数・フィールドに持つ `func(A, B) R` 型は `Go.FuncN` として読まれる（[decisions/func-interop](decisions/2026-11-17-func-interop.md)）。`Go.FuncN` は `Go.` 修飾の型で、型引数は **生の Go ビュー** — パラメータ N 個のあとに結果 0..8 個が続く可変長で、`func(A, B) R` は `Go.Func2 A' B' R'`、`func(A) error` は `Go.Func1 A' GoError`、`func(A) (T, error)` は `Go.Func1 A' T' GoError`、`func(A) (T, U, error)` は `Go.Func1 A' T' U' GoError`、`func() R` は `Go.Func0 R'` と書く。`N` は 0..3、結果は 0..8 個 — 4 個以上のパラメータ、9 個以上の結果、variadic な func 型を持つシンボルは従来どおり公開されない（E0501）（[decisions/nary-go-results](decisions/2026-11-18-nary-go-results.md)）。

`Go.FuncN` 値は Go の func 値そのもので、Gom の中では値として束縛・受け渡しできる（そのままでは適用できない — Go 側から返された func 値は `Go.FuncN` のまま引き回し、Gom 側で呼ぶには明示変換 `Go.fnN` を通す）。Gom の関数値 `A1 -> ... -> AN -> R` を `Go.FuncN` に変換するのは明示変換 `Go.funcN` だけで、暗黙の変換はない — `Func` 型の値を `Go.FuncN` のパラメータに直接渡すと E0401 になる。

`Go.funcN f` は 1 つの関数値だけを取る。`f` のパラメータ数は N と一致し（カリー化された `A -> B -> R` は 2 段に平坦化する）、ラベル付きパラメータ・非関数・Arity 違いは E0510。`Go.func0` は `Unit -> R` のサンクを `func() R` に対応させる。各パラメータ・結果は 8.5 節の境界変換表で橋渡しし、`Result Unit GoError` は `error`、`Result T GoError` は `(T', error)`、`Result (T, U, ...) GoError` は `(T', U', ..., error)` に読む — `Result (Option T) GoError` も同じ `(T', error)` に読み、`Some` が値・`None` が nil を出入りする（import mapping の `(I, error)` 宣言ビューの往復で、`option.NilOr` を emit する）— ただし `T` の raw ビューが nil を持てる型（`error`・`any`・ポインタ・スライス・マップ・チャネル・`Go.FuncN`・underlying が nil-able な named 型）に限り、`Option Int` のような非 nil-able ペイロードは `None` を書けないので E0510。トップレベルのタプル結果 `(A, B, ...)` は Go の多値 `(A', B', ...)` に展開する（ペイロード内にネストしたタプルは変換できない）。ただし最終要素が `GoError`（`Option GoError` を含む）のタプルは `Result` 折り畳みと同じ `func` シグネチャを綴るため曖昧 — E0510 で拒否し、`Result ... GoError` と書く。Gom 側だけの型（ペイロード以外の位置の `Option`・`Result`・ネストしたタプル・カリー化した関数型）を持つ引数・結果は E0510 で拒否する — ユーザ宣言型は生成パッケージの名前付き Go 型としてそのまま通る（`Go.func2` の cmp コールバックが `Rec` を扱える — `slices.SortFunc` が完結する）。期待される `Go.FuncN` がある位置では、produced と expected の型引数を raw ビューで互換比較し（`int`/`int64` 境界、`List`↔`Go.Slice` の要素毎のずれは許す）、食い違えば E0401。inline lambda への引数はその期待型からパラメータの型を引く（`http.HandleFunc` 相当へ渡す `Go.func2 (fn w r -> ...)` の `w`/`r` が具体化する）。

`Go.fnN f` は逆方向の明示変換（[decisions/fn-interop](decisions/2026-11-18-fn-interop.md)）— `f : Go.FuncN` の値を Gom の関数値に変換し、以後はふつうの Gom 関数として適用できる。型引数の読みは `Go.funcN` の seed と同じ写像で、パラメータ `p'_i` は Gom ビューに読み（`Go.Int` → `Int`、境界位置の `Go.Slice T` → `List T`）、結果は 0 個 → `Unit`、`error` 1 個 → `Result Unit GoError`、`(T, error)` → `Result T GoError`、末尾が `error` の 3 個以上 → `Result (T, U, ...) GoError`、その他の 2 個以上 → タプル `(T, U, ...)`、単一 `T` → その Gom ビュー、と読む — ただし `Result` ペイロード内（単一でもタプル要素でも）でメソッドを持つ interface（`error`・`io.Reader` など）のとき、import mapping と同じくその位置は `Option` で包まれる（nil を `Ok` に忍ばせないため）。単一 `error` は必ず honest な `Result` で、`GoError` 素の never-nil claim は `Go.fnN` では表せない。引数・結果の型が生ビューと Gom ビューでコンテナ内部まで一致しない場合（`[]int`・`map[string]int`・`chan int`・`*int` — 要素ごとの変換はいまの境界では書けない）は E0510 で拒否する。`Go.fn0` は `func() R` を `Unit -> R` のサンクに対応させる。arity の不一致（`Go.fn2` に `Go.Func1`）・`Go.FuncN` でない引数・ラベル付き引数は E0510。生成される値は通常のカリー化された関数値で、内部で flat な `f(a0, a1)` を呼ぶ — 引数・結果の型が raw ビューとずれる位置（`int`/`int64`、`List`/`Go.Slice`）では両側に変換が入る。

メソッドを持たない named func 型（`context.CancelFunc`、`fs.WalkDirFunc` など `type F func(...)`）は underlying の func 型そのものに読まれる — `context.CancelFunc` は `Go.Func0` として読まれ、`Go.fn0` での呼び出し・`Go.func0` 値の named パラメータ・型注釈への受け渡しが unnamed の func 型と同じ規則で通る（[decisions/named-func-types](decisions/2026-11-21-named-func-types.md)）。この読み替えは Go の代入規則が named と unnamed を両立させる位置 — シグネチャのパラメータ・戻り値のトップレベル、メンバーの宣言型、構造体フィールド — に限る。コンテナ要素（`[]F`・`*F`・`map[K]F`・`chan F`）や func 型の内側の引数・戻り値では名前が載っているので名前的な型 `pkg.F` のまま読む（`[]F` と `[]func()` は別の Go 型なので）。`Go.FuncN` 値は Go の interface パラメータ（`any`・`interface{}` など）に渡せ、`Go.conv f : Go.Any` も受理する — 境界の型関係は `Go.FuncN` を実際の func シグネチャとして検査する。メソッドを持つ named func 型（`http.HandlerFunc` の `ServeHTTP` など）は名前的な型 `pkg.F` のまま — `Go.fnN` には渡せない（E0510）が、そのメソッドは呼べるし、`Go.conv f : pkg.F` で `Go.FuncN` 値から named キャストできる。`Go.conv` の逆向き（`pkg.F` や `Go.FuncN` から Gom の `Func` 型へ）は `Go.fnN` の仕事なので、func 型どうしの形の違うペアは E0507 で拒否する。

### 8.9 Go インターフェースの実装 `@method`

自分の record 型（単一フィールド record 由来の defined type を含む）に Go のメソッドを生やして、Go の interface に適合させられる（[decisions/go-iface-impl](decisions/2026-11-21-go-iface-impl.md)）。`@method` を付けた `fn` はレシーバを先頭に書く — `fn (r: T) name (p1: A) -> R`。Gom の中ではふつうの関数 `name` として呼び、シグネチャのパラメータ位置はレシーバを含む。emit では 2 つ出力される: ふつうの Go 関数と、それに転送する薄い Go メソッド（`func (r T) Name(p1 A) R { return name(r, p1) }`）。メソッド名は `ExportName`（`string` → `String`、`error` → `Error`）。`ServeHTTP`・`URL`・`ID` のように `ExportName` で作れない名（initialism を含む名前など）は `@method(GoName)` で verbatim に指定する — `@method(ServeHTTP) fn (h: Handler) serve_http ...` は `func (h Handler) ServeHTTP(...)` を emit し、interface 適合もその名前で照合される。引数は大文字始まりの識別子に限る（そうでなければ E0101）。`Unit` のダミーパラメータはメソッド側では落とされ、残りが `Unit` の戻り値も Go 側では結果なしになる（`http.Handler` のような結果を返さない interface に適合できる）。`.gomi` で宣言されたメソッド名は契約になる — 実装が別の名前を emit する（verbatim 引数の付け忘れ・打ち間違いなど）と E0609。

record の値は、その `@method` 集合が要求される Go interface のメソッド集合を（名前・シグネチャが `types.Equal` で）含むとき、interface が期待される位置にそのまま渡せる — `let s : fmt.Stringer = g`、`Go.conv`、interface を引数に取る Go 呼出し。Gom 側からメソッドを呼ぶ構文はなく、`name r` の関数適用がそのまま。

制約 — 違反は E0420: レシーバは自分のパッケージの record 型（`Ptr T` 可、型パラメータを持つ record 不可 — Go メソッドは型引数を取れない）、`@extern` との併用不可、`Result` 返り不可（メソッドの error 返却は Result 折り畳みと衝突するため）。また `Unit` 返りのメソッドは Go 側では結果なしに emit されるので、`struct{}` を結果として要求する Go interface メソッドには適合できない。`.gomi` は `@method` 行をそのまま保持し、インポート側でも同じメソッド集合として見える。

### 8.10 `@extern let` と `//gom:embed`

パッケージ変数を Gom 側で宣言だけして、実体を Go 側に置ける。

```
@extern let version : String        // 手書きの var version string を期待
fn get () -> String = version
```

- `@extern let name : T` は `let name : T`（`= e` なし）の形。型注釈は必須、束ねるのは単純名のみ。生成コードには何も出ない — 宣言した名は同じ生成パッケージの手書き Go が `var name <T>` で供給する（`@extern fn` の値版）。違反は E0512（注釈なし・`= e` ありは E0101）
- 名前はふつうの値として解決・参照できる。export は `.gomi` の `let` シグネチャで行う

**`//gom:embed`** — `@extern let` の直上（doc コメントや他の `//` と同じ連続したコメント行の中）に書くと、コンパイラが `//go:embed` 変数を生成する:

```
//gom:embed hello.txt
@extern let hello : String
```

→

```go
import _ "embed"   // ファイルが embed を他に import していなければ自動で付く

//go:embed hello.txt
var hello string
```

- ディレクティブは `//gom:embed` のあとに空白区切りで 1 つ以上のファイルパターンを取る。複数行書けば行ごとに `//go:embed` が出る（Go のルールどおり順に）
- 宣言の型は `//go:embed` が認めるものに限る: `String`、`Go.Slice Byte`、または named Go 型（`embed.FS` など）。違反は E0512
- パターンに空が1つもない・`@extern` のない `let` に付けた場合も E0512。パターンが実際にファイルを指すかは `go build` の側で検査される（Gom 側では検査しない）
- directive の付着規則は doc コメントと同じ: 宣言の直上に連続するコメント行の run の中にある `//gom:` 行だけが対象で、空行を挟むと付着しない

**blank import** — `import go _ "path"` は `_` をエイリアスに取る Go import で、名前は何も束ねず `_ "path"` を emit する。`//go:embed` が `embed` パッケージを参照しない場合の埋め合わせや、副作用のための import（`import go _ "net/http/pprof"`）に使う。`import go` でない Gom パッケージには書けない。

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

fn parse_port (s: String) -> Result Int GoError =
  Ok (strings.TrimSpace(s))
  |?> strconv.ParseInt(_, 10, 64)
  |?> check_port_range
```

（`os.ReadFile` の `[]byte` 戻り値は `List Byte` と読む（8.3 節）— `parse_toml` は `List Byte` を受ける形でこの例はコンパイルできる。`strconv.ParseInt` の `int64` 結果は対応表に従い `Int` に見える）

1. `r |?> f` は `r : Result a e` と `f : a -> Result b e` を取り、`Result b e` を返す。`Ok v` なら `f v`、`Err e` ならそれ以降のステップを飛ばして `Err` のまま流す
2. 優先順位と結合は `|>` と同じ。1 つの流れの中で `|>` と混ぜられる
3. **自動ラップ**: エラー型が `GoError` のとき、失敗したステップのエラーを `*rt.StepError` フレームで包む（`Step` はステップ名、`Unwrap()` は元のエラー — 描画される文言は `<ステップ名>: <元のメッセージ>`）。`errors.Is` / `errors.As` で元のエラーを判定できる
4. ステップ名は、右辺の式の先頭にある名前をソースに書かれたとおりに使う（`os.ReadFile`、`parse_with opts` なら `parse_with`、`strconv.ParseInt(_, 10, 64)` なら `strconv.ParseInt`）。`let f = parse_toml` のあと `|?> f` と書けば名前は `f` になる。関数値の由来は追跡しない。右辺がラムダなどで名前がなければソース位置（`config.gom:14`）を使う
5. 包むのはエラーが起きたステップでの 1 回だけ。後続のステップは `Err` を素通しする
6. エラー型が `GoError` 以外のときは包まない
7. 最初の値が `Result` でなければ `Ok` で包んで始める。`path |> os.ReadFile |?> ...` と書くと、最初の Go 呼び出しの失敗は包まずに流れる
8. `Result` を返さない関数は右辺に置けない。最初の `|?>` より前なら `|>` で、後ろなら `|> Result.map f` でつなぐ: `s |> strings.TrimSpace |?> strconv.Atoi |> Result.map double`（[decisions/result-pipe-step-must-return-result](decisions/2026-11-04-result-pipe-step-must-return-result.md)）

例: `validate` が `port must be > 0` で失敗すると、結果は `validate: port must be > 0`。

自動ラップは `errors.Is` / `errors.As` の結果を変えないが、`Error()` の文字列は変える。この文字列も公開される振る舞いの一部として扱い、規則を変えるときは決定記録を残す。

ラップは `*rt.StepError` フレームとして付く（[decisions/result-pipe-wrap-frame](decisions/2026-11-05-result-pipe-wrap-frame.md)）: `Error()` は `ステップ名: 元のメッセージ` の文言を保ち、`Unwrap()` は元のエラーを返し、`Step` フィールドがステップ名を構造化したデータとして持つ。Go 側（または `import go "github.com/podhmo/gom/rt"` した Gom 側）から `errors.As` で `*rt.StepError` を取り出せる。`strconv.Atoi` のようにエラー自身が関数名を名乗る場合、文言は `strconv.Atoi: strconv.Atoi: parsing "x": ...` のように重複する — 外側は呼び出し箇所の由来、内側はエラー自身の文言で、別の情報源であり重複は許容する。

文言を自前で制御したい場合の経路は既にある:

- 消費側で読み替える — `errors.As` で `*rt.StepError` を取り出し、`Step` フィールドと `Err` から好きな文言を組み立てる（Go 側、または `import go "github.com/podhmo/gom/rt"` した Gom 側）
- 自前のフレームを付ける — `rt.StepErr`（`import go "github.com/podhmo/gom/rt"`）を `Result.map_err` 経由で適用し、任意のステップ名で包む。標準ビューは `Result Unit GoError` を返すため、常に non-nil を返す `rt.StepErr` には `fn StepErr (step: String) (err: GoError) -> GoError` の extern 宣言（never-nil ビュー）を添える
- 素の `Result` を扱う — `|?>` の自動ラップを介さず `try` + `match` で処理する

`|?>` 側で文言を設定する構文・ディレクティブは future work（open-questions.md、[decisions/pipe-try-wording](decisions/2026-11-23-pipe-try-wording.md) の案 C）。

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

import した宣言型の本体は import 側から見えない（抽象の `type T` や extern 裏付け）ので、interface ファイルは `//gom:mutable <name>` ディレクティブでその型が可変な値を含むと表明できる。マークされた型は上のリントで `Ref` などと同じく数えられる。`gom info` の再生成は既存 `.gomi` の `//gom:mutable` 行を、同名の型宣言へ引き継ぐ（再生成される draft は実体を出す起点で、著者が abstract 形へ絞り直す際に directive を落とさないため）。

例外として、内部で同期が取れた Go の名前付き型を指す `Ptr` は警告しない — `sync.WaitGroup`・`sync.Mutex`・`sync.RWMutex`・`sync.Map`・`sync.Once`・`sync.Pool`・`sync.Cond`・`sync/atomic` の各型・`errgroup.Group` は共有が型の契約であり、ポインタを goroutine やチャネルで受け渡すのが正しい使い方。許可リストは閉じた組み込みテーブルで、並行安全を自らのドキュメントが保証する型だけが載る。

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
9. ファイル内ディレクティブ `//gom:allow <code>...` は、そのファイルで発火した指定コードの診断を緩和する。error コードは relaxable フラグの付いたものだけが対象で、error から warning に格下げされる（初期は E0301・E0302 — 欠けた腕は生成コードで panic、到達不能腕は落とされるので、局所的に回復できる）。格下げされても診断はコード付きで報告に残り、silent にはならない。warning コードは下げる段がないため、指定するとその warning を報告から除く（W0901 のような意図的な握り潰しの沈黙用）。登録外のコード、relaxable でない error コード、コードの無い行は W0003 で報告する
10. `--strict`（`gom check` / `gom build` / `gom test`）は warning 診断が残っていると終了コードを 1 にする（CI で warning 残置を禁じる用途）。`--strict=E0301,W0501` のようにコード列を付けると、そのコードの warning があるときだけ失敗する。warning の severity 自体は変わらない — 厳しくなるのは終了コードだけ

| コマンド | 出力するメッセージの `kind` |
| --- | --- |
| `gom build` | `diagnostic`、`summary`（生成した `.go` ファイルの一覧を含む） |
| `gom check` | `diagnostic`、`summary`（Go を生成せず、型検査とパターン検査だけ行う） |
| `gom test` | `diagnostic`、`test`（テストごとの成否・所要時間・失敗時の値と位置）、`summary`（`tests` の集計を含む） |
| `gom run` | コンパイル・リンクが失敗したときだけ `diagnostic`、`summary`。成功時はメッセージを出さず、プログラムの出力だけが stdout/stderr に残る |
| `gom fmt --check` | `unformatted`（整形が必要なファイルと差分）、`summary` |
| `gom fix` | `applied`（適用した編集）、`summary` |
| `gom explain <code>` | `explanation`（エラーコードの説明・例・修正例） |
| `gom schema` | メッセージ形式の JSON Schema |
| `gom info` | `.gomi` 下書きをファイルまたは `-o -` で stdout に書く。診断は stderr の同形式ストリーム |
| `gom list` | import 可能な stdlib の短名を 1 行 1 件で stdout に書く（`import "list"` 等が受理する表そのもの）。`-external` は代わりに、作業ディレクトリの module 文脈から import 可能な Gom パッケージ（main module 内の `.gomi` 所在ディレクトリと `go list -m all` の build list 内の同。vendor mode が有効なら `<modRoot>/vendor` 配下の `.gomi` 所在も vendored パスで列挙する）を import パスで列挙する — ファイルシステムを walk するため opt-in。メッセージは出さない |
| `gom dump -phase` | フェーズの IR ツリーを stdout に書く。診断は stderr の同形式ストリーム |

`applied` オブジェクトは適用した fix 1 件につき 1 つ。`code` は fix が答えた診断のコード、`title` は `fixes` にあった fix のタイトル、`edits` は診断の fix edit と同じ形（`file`・`start`・`end`・`text`）で、編集前のソースの座標系で解決される。

```json
{"kind": "applied", "code": "E0402", "title": "discard the value with `ignore`", "edits": [{"file": "a.gom", "start": {"line": 7, "col": 3, "offset": 93}, "end": {"line": 7, "col": 3, "offset": 93}, "text": "ignore ("}, {"file": "a.gom", "start": {"line": 7, "col": 16, "offset": 106}, "end": {"line": 7, "col": 16, "offset": 106}, "text": ")"}]}
```

`explanation` オブジェクトはエラーコード表のエントリをそのまま運ぶ。`code`・`severity`・`title`・`phase`・`explanation` は必須、`spec`・`example` は表が持つときだけ、`retired` は廃止コードのときだけ `true` で付く。引数なしの `gom explain` は全コードの `explanation` を出す（`--format text` ではコードとタイトルの一覧）。

```json
{"kind": "explanation", "code": "E0301", "severity": "error", "title": "non-exhaustive match", "phase": "pattern", "spec": "4", "explanation": "...", "example": "..."}
```

`test` オブジェクトは `gom test` が `go test -json` から変換した 1 テストの結果で、`diagnostic` の後、`summary` の前に出る。`name` は `test "name"` の名前（Gom が生成していないテストは Go の関数名）、`func` は生成された Go のテスト関数名（`go test -run` のセレクタに使える）、`ok` は成否、`elapsed` は `go test` が報告した秒数。`skipped` はスキップ時だけ `true` で付く（スキップは `ok: true`）。`span` はテスト宣言の名前の位置で、Gom が生成していないテストでは付かない。`output` はそのテストが出した出力 — 失敗時は `assert_eq` の両方の値と、生成 `//line` 経由で `.gom` に戻した位置を含む。panic や goroutine dump が出たとき `output` は 2 部構成になる — 先にフレームを `.gom` の位置に戻したトレース（`.gom` に解決できないフレームは生の `file:line` のまま残る）、続いて生の Go スタックトレースをそのまま出す。ただし `.gom` の位置に解決できるフレームが 1 つもない出力（ツールチェーンや手書き Go だけのスタック）は、書き換えの対象がないため変換せずそのまま返す — 再現しないバグの情報を落とさないため生の内容を必ず残す（design.md 4.8 節）。`gom test -q` / `--quiet` は生のトレース側を抑制する（`.gom` 位置付きのトレースは残る — 1 行も解決できなければ出力は変わらない）。`init` や `TestMain` のようなパッケージレベルの panic を運ぶ E0001 diagnostic の `output` も同じ規則に従う。

```json
{"kind": "test", "name": "rect area", "func": "TestRectArea", "ok": false, "elapsed": 0.001, "span": {"file": "area.gom", "start": {"line": 15, "col": 6, "offset": 232}, "end": {"line": 15, "col": 16, "offset": 242}}, "output": "    area.gom:16: gom: assertion failed:\n          actual:   Rect ~w: 1 ~h: 1\n          expected: Rect ~w: 2 ~h: 2\n"}
```

`gom test` が実行するのは `go test` のパッケージ粒度である — 入力ファイルのあるディレクトリのパッケージ全体のテストが走るので、同じパッケージの他の `.gom` 由来や手書きのテストも結果に含まれる。それらは `span` のない `test` オブジェクトとして `func` と同名の `name` で報告される。コンパイルにエラーがあるとき `go test` は実行しない（`tests` の集計はすべて 0）。Go のツールチェーン側の失敗（テストバイナリがコンパイルできない、`go test` が起動できない）は `"source": "go"` の `diagnostic` として報告する。

`gom run` は `go run` の綴り方である — 単一の `package main` ディレクトリをコンパイルし、生成コードをソースの隣に書き（`gom build` と同じ置き場）、`go build` で temp バイナリにリンクして実行する。`go run` でなく `go build` + 直接起動にするのは、子プロセスの終了コードをそのまま返すためである（`go run` はどんな失敗も `exit status 1` に潰す）。引数列の最初の `--` 以降はプログラムの argv に渡す。入力が複数ディレクトリにまたがる、または `package main` でない場合は使用エラー（終了コード 2）。コンパイルやリンクに失敗したときだけ通常のメッセージストリームを出し、成功時は `summary` も出さない — パイプで受けるプログラム出力を汚さないためである（[decisions/gom-run](decisions/2026-11-18-gom-run.md)）

`gom test` の引数列のうち最初の `--` 以降は `go test` にそのまま渡す（`gom test -- -race`、`gom test -- -run Bump -count=1` など）。`-json` は gom が結果の取得に使うフラグなので、これを打ち消す指定（`-json=false`）は結果ストリームを壊し、Go ツール側の失敗として `"source": "go"` の E0001 で報告される。未知・不正なフラグも gom 側の使用エラーではなく同じ経路で報告される（[decisions/gom-test-go-test-flags](decisions/2026-11-05-gom-test-go-test-flags.md)）。

## 付録 A. `//gom:` ディレクティブ索引

`//gom:` で始まるコメント行はコンパイラが読むディレクティブである。共通の規則:

- 行頭の `//gom:` だけが対象 — `x + //gom:...` のような行末は無効
- 宣言に付着する形は doc コメントと同じ規則: 宣言直上に連続するコメント行の run の中にある行だけが付着し、空行を挟むと外れる
- `gom fmt` はフォーマットでディレクティブ行を移動しない（付着 run を壊さない）

| ディレクティブ | 引数 | 付着位置 | 効果 | 関連 |
| --- | --- | --- | --- | --- |
| `//gom:allow` | `<code>...`（1 つ以上） | `.gom` ファイル内の行頭どこでも | そのファイルで発火した指定コードの診断を緩和 — relaxable な error は warning へ格下げ、warning は報告から除く | §12、W0003（誤用の報告） |
| `//gom:embed` | `<pattern>...`（1 つ以上のファイルパターン） | `@extern let` 宣言の直上 | コンパイラが `//go:embed` 変数を生成する。型は `String`・`Go.Slice Byte`・named Go 型（`embed.FS` 等）に限る | §8.10、E0512 |
| `//gom:noescape` | `<name> <index>...` | `.gomi` の `fn`/`let` シグネチャ直上（`gom info` が書き出し） | そのパラメータ位置のラムダがスコープを脱出しないことを表明し、import 側の E0419 判定に効く | §4（`Scope` 規則） |
| `//gom:result-notaint` | `<name> <index>...` | `.gomi` の `fn`/`let` シグネチャ直上（`gom info` が書き出し） | そのパラメータの生産物が結果に載らないことを表明 — 注釈のない位置は保守的に「載る」扱い | §4（`Scope` 規則） |
| `//gom:mutable` | `<name>` | `.gomi` の型宣言直上（著者が記述、`gom info` の再生成が引き継ぐ） | extern 裏付け・abstract export で中身の見えない宣言型が可変な値を含むと表明し、可変構造リントで `Ref` 等と同じく数える | §10 |

`@method(GoName)` のような `@`-attribute の引数はディレクティブではない — ここに列挙しない。
