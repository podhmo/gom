# Gomトランスパイラーの実装 — ASTからGoソースまで

調査日: 2026-11-20。対象: [`podhmo/gom`](https://github.com/podhmo/gom)、`main` のコミット `e14baa034cf716ec6f1964ce6d1f481535a77722`。本書はGo/Python/TypeScriptを使用できる読者に向けた、実装のコード読解資料である。プログラミング言語処理系の用語は必要に応じて定義する。Go/Python/TypeScriptにたとえる比喩は用いない。

現行実装を説明する文書であって、言語仕様の規範ではない。言語として保証することは [`docs/spec.md`](https://github.com/podhmo/gom/blob/main/docs/spec.md)、設計意図は [`docs/design.md`](https://github.com/podhmo/gom/blob/main/docs/design.md)、実装選択の経緯は [`docs/decisions`](https://github.com/podhmo/gom/tree/main/docs/decisions) が正となる。例示する「概念的なIR」「概念的なGo」は実際のダンプをそのまま写したものではない。

## 1. AST生成後のコンパイラ全体像

Gomは式指向、静的型付けの言語である。型推論、代数的データ型（ADT）、パターン照合、関数の部分適用、Goパッケージ呼び出しを持つ。パーサーが生成した構文木をすぐにGoの構文へ置換するわけではない。後続フェーズは意味を一つずつ決定し、最後のコード生成には未解決の意味判断を持ち込まない。

```text
Gomソース .gom / インターフェース .gomi
  │ syntax: 字句解析・構文解析
  ▼
表層 AST  (*syntax.File)
  │ resolve: 名前解決・import・Symbolへの束縛
  ▼
Resolved  (表層AST + Defs/Usesなどの対応表)
  │ check: 型検査・制約解決・関数呼び出し分類
  ▼
型付き AST  (*check.File)
  │ pattern: パターンの網羅性・冗長性検査
  ▼
検査済み型付き AST （新しい中間表現は作らない）
  │ lower: ANF化、match決定木、部分適用・脱出・Go変換
  ▼
Core IR  (*core.File)
  │ core.Validate、初期化循環、@tailrecの検査
  ▼
codegen: 宛先指定によるGoコード出力
  │ Goの式・型の一部にgo/astとgo/printerを使用
  ▼
Goソース文字列 → go/format → //lineとSourceMap
  ▼
.go / _test.go → Goツールチェーン
```

| 段階 | 入力 | 生成物または結果 | 主な責任 |
| --- | --- | --- | --- |
| `resolve` | `syntax.File` | `resolve.Resolved` | 名前を宣言の同一性に結び付ける |
| `check` | `Resolved` | `check.File` | 各式の型、関数適用の規約、型引数を決定する |
| `pattern` | `check.File` | 診断 | `match`の網羅性・冗長性を判定する |
| `lower` | `check.File` | `core.File` | 評価順、分岐、制御効果、関数値の実装表現を確定する |
| Unit-level checks | 複数`core.File` | 診断 | パッケージ初期化の循環、相互再帰を検査する |
| `codegen` | 検証済み`core.File` | Goソース、SourceMap | 既に決まった意味をGoへ出力する |

このフェーズの順序は [`internal/build/build.go`](https://github.com/podhmo/gom/blob/main/internal/build/build.go) の `Compile` が示している。エラーがあれば次の意味解析フェーズへ進まない。`.gomi`は型検査までで止まり、`gom check`はパターン検査までで止まる。ソースコード生成の成功と生成後のGoコンパイラでの成功は別段階である。

設計境界として最も重要なのは、`codegen`にGomの意味を再判断させないことである。たとえば関数が既知か、適用が飽和しているか、Goとの変換が要るかという判断は型検査・loweringで決定済みの情報を受け取る。

## 2. コンパイル単位: ファイルとパッケージを区別する

同一ディレクトリの`.gom`はひとつのパッケージを構成する。別ファイルにある宣言を参照できるので、ファイル単位で名前解決・型検査を独立して完了させると前方参照が壊れる。

`build.CompileFiles`は先に全ソースを構文解析する。次に`resolve.Files`がパッケージ共通スコープで名前を集め、`check.Files`がファイルをまたぐ宣言集合を型検査する。結果は各ファイルの`check.File`へ分配する。その後は兄弟ファイルの宣言を`pattern.Options.Decls`、`lower.Env.Siblings`に渡してファイルごとに処理する。初期化循環・相互再帰は再びパッケージ単位で検査する。

つまり次の二種類の集約がある。

- 名前・型の解析では、複数ファイルにまたがる定義の同一性を共有する。
- Core生成とGo出力では各ファイルの成果物を維持し、ファイル横断的な性質だけを後で集約して調べる。

`import`はファイルローカルであり、トップレベル宣言はパッケージで共有する。この区別は`resolve.Files`のスコープ構築で実装されている。

`CompileFiles`の現行コードはファイルごとのGoコードを先に生成し、その後でパッケージ単位のCore検査を実施する。検査でエラーがあれば全ファイルの`Output`を破棄するので、完成物は部分的には返らない。ただし不要なコード生成が起きる余地がある。後述の調査記録に改善候補として記載した。

参照: [`build.go`](https://github.com/podhmo/gom/blob/main/internal/build/build.go)、[`check/files.go`](https://github.com/podhmo/gom/blob/main/internal/check/files.go)。

## 3. 名前解決 — 識別子の綴りから宣言オブジェクトへ

### 3.1 なぜ必要か

パーサーが生成した`syntax.Ident`は識別子の綴り・位置を持つが、それだけでは`x`がどの宣言か分からない。同名のローカル束縛、パターン変数、トップレベル関数、型コンストラクタ、外部パッケージのメンバーを区別する必要がある。以後の型検査・置換・コード出力が、その識別子の同一性に依存する。

`resolve.Resolved`は表層ASTを保持し、`Defs map[*syntax.Ident]*Symbol`および`Uses map[*syntax.Ident]*Symbol`などの対応表を持つ。`Defs`は定義を導入する位置、`Uses`は参照である。対応する`*resolve.Symbol`が同じであることが、綴りの一致より強い意味を持つ。

### 3.2 スコープとは

スコープは、ある位置で参照可能な名前を宣言へ対応させる環境である。実装は項（term/value）と型（type）の名前空間を区別し、親スコープをたどって探索する。パッケージのトップレベルスコープと、ファイルのimportスコープ、式や関数の局所スコープは役割が異なる。シャドウイングした同名の変数は、それぞれ異なるSymbolを指す。

### 3.3 複数パスで収集してから本体を読む

`resolve.Files`は、各ファイルで`collect`し、全ファイルで`bindTerms`し、最後に本体を`resolveDecls`する。外部宣言、型、コンストラクタなどを先に集めた後にトップレベルの項を束縛し、それから式やパターンの識別子を解決する。

これにより、後に宣言した関数や別ファイルの名前が参照できる。反対に、同じスコープへの重複定義を検出できる。名前を見つけることと、その名前の型・実行規約を決めることは別処理である。

### 3.4 型検査へ保留する参照

`x.M`の`M`が何かは`x`の型が分からなければ判定できない場合がある。レコードのフィールド、Goメソッド、Gom側のメンバーなどは型依存である。すべての識別子を名前解決だけで強制的に確定するわけではない。`check`が受け手の型を解いた後に確定する部分を残す。

### 3.5 外部宣言のロード

`import "path"`では`internal/gomimport`が`.gomi`インターフェースを読み込み、公開面の型・メンバー・コンストラクタを提供する。Goの`import go`では`internal/goimport`が`go list -export`、Goの型情報（`go/types`）を利用する。ローダーは複数利用者に対して同じセッション内のシンボルを共有し、型同一性を保つ。外部の名前を毎回文字列から再構築する方式ではない。

参照: [`resolve/resolve.go`](https://github.com/podhmo/gom/blob/main/internal/resolve/resolve.go)、[`resolve/scope.go`](https://github.com/podhmo/gom/blob/main/internal/resolve/scope.go)、[`gomimport/loader.go`](https://github.com/podhmo/gom/blob/main/internal/gomimport/loader.go)、[`goimport/loader.go`](https://github.com/podhmo/gom/blob/main/internal/goimport/loader.go)。

## 4. 型推論 — 型の未知数を解く

### 4.1 型表現を理解する

`internal/types`は、名付き型`Con`、剛性型変数`Var`、推論用のメタ変数`Meta`、関数`Func`、タプル`Tuple`、Go由来の型`Go`などを持つ。剛性型変数`a`は多相シグネチャに現れ、ある使用箇所で恣意的に`Int`へ固定しない。メタ変数`?m`は未解決の未知型であり、制約から`Int`などへ束縛できる。`types.Chase`は既に束縛されたメタ変数を追って具体型を取り出す。

例: 多相な同一性関数を二回呼ぶ場合、第一の使用箇所では`a`を`?m1`へ、第二では独立した`?m2`へ置換する。これがインスタンス化（instantiation）であり、それぞれ`Int`と`String`に解決できる。

### 4.2 単一化（unification）

単一化は、二つの型が等しくなるようなメタ変数への代入を計算する操作である。`unify(want, got)`は期待型と実際の型を受け取る。未知型なら束縛し、複合型なら型コンストラクタとその引数を再帰的に比較する。

```text
want: List ?a
 got: List Int
------------------
? a := Int     （概念図）
```

`?a = List ?a`という代入は無限型を要求する。`occurs`は未知型が代入先の内部に現れるか検査し、自己包含を拒否する。これをoccurs checkと呼ぶ。Gomにはさらに数値、整数、順序、等価性などの制約フラグがあり、メタ変数が具体化した時点で検証する。

`Go.Any`は値位置ではボックス化の受け皿になる。しかし`Option Go.Any`という型の引数位置では`Option Int`を同じ型として扱わない。これらを区別するため`unify`（値スロット）と`unifySub`（複合型の内部）の規則が分かれている。

### 4.3 Hindley–Milner型推論の構成要素

Hindley–Milner（HM）型推論には、型の制約を解く単一化のほか、`let`束縛での型一般化（generalization）と、参照時の具体化（instantiation）がある。一般化は、環境と共有されていない未解決の型変数を`∀a`のような多相型へ変える。参照時は型変数ごとに新しいメタ変数を生成する。

```text
let id = fn x -> x
  推論型: ?a -> ?a
  一般化: forall a. a -> a

id 10    : a -> a を ?u -> ?u に具体化、?u = Int
id "foo" : a -> a を ?v -> ?v に具体化、?v = String
```

ただしGomの実装は教科書のAlgorithm Wをそのまま走らせるわけではない。明示的アノテーション、期待型の伝播、Go型との適合、遅延される選択・適用、複数回の制約処理が加わる。

### 4.4 Value restriction（値制限）

一般化を無条件に許すと、型変数を含んだ計算結果が可変状態などを通じて共有され、使用箇所ごとに別の具体型を想定することが問題になり得る。Gomの`checkLetDecl`は、主にトップレベルの`let`で右辺が構文的な値（ラムダ、リテラル、値のみで作った集約など）かを検査し、一般化を制限する。通常の関数呼び出しの結果は構文的な値ではない。ローカル`let`が自由に多相化される設計ではない。

既に前方参照や再帰的参照で単相の形が使われた束縛は、一般化によって先の参照と食い違わないように制限する。トップレベルの`let`がラムダを保持すると、`FnDecl`へ再分類してGoの関数宣言を生成することもある。これには初期化依存グラフを不正に循環させない効果がある。

### 4.5 双方向の型付け（bidirectional typing）

式から型を合成する方向だけでなく、周囲が要求する型を式へ渡す方向もある。`checkExpr(e, want)`は必要に応じて期待型`want`を子の式へ伝える。ラムダの引数や空のコレクションなど、自身だけでは型を決めにくい式は、期待型があると確定できる。

型の誤りは`check`で診断される。エラーが起きても診断を集めるため部分的な型付きASTを返す場合があるが、それを`lower`へ渡してはならない。

### 4.6 型の最終確定は固定点反復

`check.finalize`は保留案件を一回だけ再訪するのではない。`revisitSelects`、`replayResolvedApplies`、フィールド代入や反復可能な型の保留処理を繰り返し、解決が進まなくなった場合にデフォルトの型を適用する。新しく解けた型が、別のメンバー参照を解決し、さらにその呼び出しが別の型変数を解くためである。

この「新たな情報が得られなくなるまで繰り返す」操作を固定点反復（fixed-point iteration）という。デフォルトの数値型を早く適用すると、本来は周囲の`Int64`などから決まるはずのメタ変数を誤って固定する可能性がある。最後に型リテラル範囲、変換、未解決変数なども検査する。

参照: [`check/check.go`](https://github.com/podhmo/gom/blob/main/internal/check/check.go)、[`check/unify.go`](https://github.com/podhmo/gom/blob/main/internal/check/unify.go)、[`check/expr.go`](https://github.com/podhmo/gom/blob/main/internal/check/expr.go)。

## 5. 関数適用の規約を型付きASTで固定する

### 5.1 呼び出しの分類

`f a b`という構文だけでGo出力は決まらない。`f`が既知のGom関数か、未知の関数値か、Go関数・メソッドか、指定された引数数が足りるかによって実装が異なる。

`check.Apply`は`CallInfo`を持つ。`Kind`は`CallDirect`（既知Gom関数の飽和呼び出し）、`CallPartial`（既知Gom関数の部分適用）、`CallUnknown`（関数値）、`CallGo`（Go関数規約）を区別する。`Callee`、`TypeArgs`、`Arity`、`FlatArity`、`Applied`、`Unapplied`、`ArgToParam`、`HoleIndex`、`Variadic`なども記録する。

`Arity`は宣言上の引数数であり、`FlatArity`は戻り値がさらに関数である場合の引数列まで平坦化した数である。ラベル付き引数が返された関数のパラメータを指定することもあるため、この違いは必要になる。

### 5.2 実引数の評価順と引数配置は別

`ArgToParam[i]`は「ソース上のi番目の実引数が宣言側の何番目の引数を埋めるか」の対応を表す。ラベル引数を宣言順に並べ替えるときも、副作用を持つ実引数の評価順序はソース順に維持しなければならない。loweringは一度ソース順で値を束縛した後、呼び出しの引数スロットへ配置する。

### 5.3 メンバー選択が未確定なら呼び出しも保留

`x.M`で`x`の型がまだメタ変数なら、`M`がGoメソッドかGomメンバーか判定できない。`check`は`pendingSels`と`pendingApplies`を用いて選択と適用を保留する。受け手型が確定したら`replayGoApply`または`replayGomApply`で引数規約を検査し直す。`CallInfo`はこの最終処理で確定され、lowerでは名前の綴りやASTの形から判断をやり直さない。

参照: [`check/call.go`](https://github.com/podhmo/gom/blob/main/internal/check/call.go)、[`check/apply.go`](https://github.com/podhmo/gom/blob/main/internal/check/apply.go)。

## 6. パターン検査: 網羅性と冗長性の証明

### 6.1 これは実行用の決定木生成ではない

型付きASTの`match`に対し`internal/pattern`が行うのは、全値がいずれかの腕に一致するかという網羅性（exhaustiveness）と、到達不能な腕がないかという冗長性（redundancy）の検査である。ここでは新しいCore IRを作らず、診断を出す。実際に分岐して値を計算するための決定木は後段の`lower/pat.go`が生成する。

### 6.2 パターン行列（pattern matrix）

実装はLuc Marangetの *Warnings for pattern matching*（JFP, 2007）に基づく。パターン行列`P`は既に登場したパターン列を行として保持する。新しい候補パターン列`q`が、`P`にまだ覆われていない値を一つでも受理するなら、`q`は有用（useful）である。

形式的には`U(P,q)`を「`q`に一致し、`P`のいずれの行にも一致しない値が存在する」という命題として考える。`U(P,q)=false`なら候補の腕が既存の腕によって覆われ、到達不能と判断できる。

```text
Shape = Circle(Float) | Rect(Float, Float)
P = [Circle _]
q = [Rect _ _]
U(P,q) = true

P = [Circle _, Rect _ _]
q = [_]
U(P,q) = false
```

### 6.3 specialize と defaultMatrix

`specialize(P,C)`は先頭のパターンがコンストラクタ`C`に一致する行を取り、そのペイロードの各フィールドへ列を展開する。先頭がワイルドカードの行も`C`の引数数だけワイルドカードに展開される。他のコンストラクタの行は除外する。これによって入れ子の照合問題を再帰的に扱える。

`defaultMatrix(P)`は先頭がワイルドカードの行だけを残し、その先頭列を落とす。先頭列に現れたコンストラクタ集合が型の全候補を覆わないとき、列挙されていない値を扱う可能性はワイルドカード行によって判定する。

`useful`は候補がコンストラクタで始まればspecializeし、ワイルドカードなら既出のコンストラクタ集合が完全なときに各コンストラクタへ分岐、不完全ならdefaultへ進む。再帰型のワイルドカードを無限展開しないためにも、この分岐が必要になる。

### 6.4 反例の生成とguard

`missing`は未被覆のパターンを構築する。コンストラクタの宣言順と深さ優先で決定的に反例を生成し、診断`E0301`の`missing_patterns`に格納する。腕の到達不能は`E0302`である。`or`パターンは代替を展開する。

`guard`付きの腕は、guardの条件が偽になり得るため網羅性を保証しない。したがって網羅性行列には無条件の腕のみを投入する。引数や`let`のような反駁不能（irrefutable）パターンが求められる位置も別途検査される。

参照: [`pattern/matrix.go`](https://github.com/podhmo/gom/blob/main/internal/pattern/matrix.go)、[`pattern/cell.go`](https://github.com/podhmo/gom/blob/main/internal/pattern/cell.go)、[`pattern/check.go`](https://github.com/podhmo/gom/blob/main/internal/pattern/check.go)。

## 7. Core IRとANF: 構文上の複雑さを減らす

### 7.1 Core IRが表す契約

中間表現（Intermediate Representation, IR）とは、ある処理段階が次段階へ引き渡すプログラムの構造化表現である。CoreはGo出力に必要な意味を明示し、表層の糖衣構文を消したIRである。

`core.Expr`は、副作用を生まず安価に参照できる`Atom`と、`Let.X`や`Body.Tail`に配置する複合式の二種類に大きく分かれる。`core.Body`は順番のある`Lets`と結果となる`Tail`を持つ。Coreには元のパターンや`_`プレースホルダやパイプ演算子は残らず、それに相当する`DSwitch`、`Closure`、`Conv`、`EscapeResult`などの具体的ノードを持つ。

### 7.2 A-normal form（ANF）

A-normal formは、複合式の評価を一時変数へ分離し、各演算の入力を単純なオペランドへ揃える表現である。Gomでは厳密な純粋ANFというよりANF風の順序付き束縛を採用する。

```text
入力の概念例:
  f (g ()) (h ())

Coreの概念例:
  let t1 = g()
  let t2 = h()
  tail = f(t1, t2)
```

これらの一時変数は単なる最適化ではなく評価順序の意味を保持する。`g()`と`h()`に副作用があるとき、引数を宣言順へ並べ替える前にソース順で評価する必要がある。`lower/expr.go`の`seq.emit`は順序付きの`Let`を追加し、`atom`/`asAtom`は複合式を必要に応じて一時束縛へする。`exprBody`は`Let`の列と末尾式を組み立てる。

ブロックの途中の式はその値を捨てても、作用のある計算を実行する必要がある。`stmt`と`blockExpr`がこの扱いを担う。

### 7.3 不変条件（invariant）

不変条件とは、そのフェーズの成果物が必ず満たすと次段階が仮定してよい性質である。Coreには、オペランドが単純なこと、複合評価の順序が確定していること、型とソース位置を保持すること、適用規約・引数配置・型変換・脱出先が確定済みであること、パターンが残っていないことなどの条件がある。

`lower.File`は構築したCoreを`core.Validate`へ渡し、`codegen.File`もコード出力の前にCoreを検証する。後段で意味判断を再発明せず、入力の契約が壊れれば失敗とする設計である。

参照: [`core/expr.go`](https://github.com/podhmo/gom/blob/main/internal/core/expr.go)、[`core/node.go`](https://github.com/podhmo/gom/blob/main/internal/core/node.go)、[`core/validate.go`](https://github.com/podhmo/gom/blob/main/internal/core/validate.go)、[`lower/expr.go`](https://github.com/podhmo/gom/blob/main/internal/lower/expr.go)。

## 8. パターンを実行用の決定木に変換する

`lower/pat.go`は「未処理の照合義務」のキューを持つ`armState`を処理する。各義務は「この対象AtomがこのPatternと一致するか」という組である。`armState`には照合待ちの`todo`、一致の途中で得た`binds`、腕本体の情報などがある。

1. 先頭の腕に照合義務が残っていなければ、その腕を実行する葉を作る。guardがあれば条件分岐する。
2. 変数、ワイルドカード、タプル・レコードの分解など、失敗しないパターンは部分対象への義務を追加する。
3. `or`パターンは状態を各代替に複製する。
4. コンストラクタ、リテラル、リスト形状など、値の判定が必要なパターンが来たら、その判定による分岐を構成し、各枝で残った腕群を再帰的に処理する。

Coreの決定木は`DLeaf`、`DGuard`、`DSwitch`、`DPanic`などのノードを用いる。`DSwitch.Kind`には`SwitchType`（ADTの型）、`SwitchTag`（Result等のタグ）、`SwitchValue`（値）、`SwitchList`（リスト形状）がある。

```text
概念的な決定木:
  switch Shape のコンストラクタ
    Circle → payloadを取り出す → 円の計算
    Rect   → payloadを取り出す → 矩形の計算
    default → invalid-variant panic
```

静的検査が網羅性を証明していても、Go側の不正な値やゼロ値の侵入を考慮して`DPanic`を残す。パターン変数を分岐の複数箇所へ展開しても束縛が衝突しないよう、Symbolの置換も行う。

「網羅性を判定するパターン行列」と「実行分岐を組み立てる義務キュー」は目的も出力も異なる。前者は診断を出し、後者はGoに変換できる決定木を作る。

参照: [`lower/pat.go`](https://github.com/podhmo/gom/blob/main/internal/lower/pat.go)、[`core/tree.go`](https://github.com/podhmo/gom/blob/main/internal/core/tree.go)。

## 9. 部分適用、カリー化、関数値の表現

### 9.1 `CallInfo`をCoreのノードへ写す

`lower.applyCall`は型検査が確定した`CallInfo.Kind`を受け、`directCall`、`partialCall`、`unknownCall`、`goCall`へ分岐する。

| `CallKind` | Coreの主な表現 | 意味 |
| --- | --- | --- |
| `CallDirect` | `CallDirect` | 既知のGom宣言の飽和呼び出し |
| `CallPartial` | `Closure`の列 | 不足引数を受け取る関数を生成し、その中で飽和させる |
| `CallUnknown` | `CallFunc` | 実行時の関数値へ引数を適用する |
| `CallGo` | `CallGo` | Goの引数規約に従い呼ぶ |

過剰適用はまず既知の宣言を飽和させ、その戻り値の関数へ残りの引数を適用する。部分適用の場合、中途半端な`CallDirect`を残さず、未適用の引数を受け取る`Closure`の連鎖を組み立てる。

### 9.2 canonical関数値と宣言のarity

Gomの関数値の標準的な表現は、引数1つにつきGo関数を1つ持つカリー化表現である。たとえば`A -> B -> R`は概念的に`func(A) func(B) R`となる。他方でトップレベルの`fn f a b`宣言はGoの`func f(a A,b B) R`に対応する。多引数宣言を「関数値」として渡すときは、必要に応じてカリー化アダプタを作る。

### 9.3 高階関数引数のflat表現

現行実装には、高階関数の宣言された複数引数の関数型パラメータを、flatなGo関数型へ適合させる規則がある。`lower/flat.go`の`Uncurry`、`curryEta`、`CallFunc.FlatArity`などが、canonicalな値とflatなパラメータを区別する。繰り返し使われるコールバックが`func(A,B)R`なら、各呼び出しでの中間クロージャの生成を減らせる。

重要なのは最適化だけでなく、部分適用によって副作用がいつ実行されるかという意味である。呼び出しの平坦化は型だけでなく適用タイミングに関係するため、Coreで規約を確定させる。

参照: [`lower/expr.go`](https://github.com/podhmo/gom/blob/main/internal/lower/expr.go)、[`lower/flat.go`](https://github.com/podhmo/gom/blob/main/internal/lower/flat.go)、[`core/call.go`](https://github.com/podhmo/gom/blob/main/internal/core/call.go)。

## 10. Go相互運用: 型と呼び出し規約の橋渡し

Goの関数は、カリー化、エラー表現、スライス、`any`、named type、複数戻り値などに関してGomと異なる。`goimport`はGoの元シグネチャを読み、`check`にはGom側から見た型を、`lower`には元のGo呼び出しを実装するための情報（`GoSig`）を提供する。

`core.GoResults`は戻り値の形を区別する。戻り値がない場合、単一値、errorだけ、`(T,error)`、ペア、一般の複数戻り値、末尾errorを持つ多値などである。`ResultFromGo`、`OptionFromGo`、`TupleFromGo`、`ErrorFromGo`、`EscapeCall`といったノードに分ける。

| Goの戻り値 | 必要な意味変換 |
| --- | --- |
| 0個 | 効果として呼び、Gomの`Unit`を得る |
| 1個 | 必要なら型変換する |
| `(T,error)` | Resultに組み立てる、または脱出条件へ融合する |
| `(T,bool)`等 | ペアの意味に従ってタプルやOptionを形成する |
| 3個以上 | 要素をタプル化し、末尾errorを必要に応じてResult化する |

`List`とGoのスライスを結ぶ際にはコピーが必要な場合がある。`any`へのボックス化、Goのinterfaceへの適合、nilの可否、数値変換も同じ境界で決定する。Goの戻り値が「二つ」というだけで`Result`にしてはいけない。正しい分類には元のGo型とGomで利用するときの型の両方が必要である。

参照: [`goimport/mapping.go`](https://github.com/podhmo/gom/blob/main/internal/goimport/mapping.go)、[`lower/expr.go`](https://github.com/podhmo/gom/blob/main/internal/lower/expr.go)、[`core/call.go`](https://github.com/podhmo/gom/blob/main/internal/core/call.go)。

## 11. `try`と`|?>`: 制御効果の脱出境界

`try e`はResultの成功値を取り出し、失敗なら周囲の関数から早期returnする。コンパイラが保存しなければならないのは「どの関数から脱出するか」である。`EscapeResult`は既に作られたResultを調べ、`EscapeCall`はGoの`(T,error)`を受け取る呼び出しと脱出条件を融合する。どちらも`To *core.Func`で脱出先を表す。

`|?>`はResultを返すパイプを、合成した局所的な脱出境界の中で評価できる。連続する成功経路は中間Resultを毎回構築せずに融合する。しかし、もともとユーザー関数を脱出先とする明示的な`try`を、合成クロージャの内側へ移すとreturnの行き先が変わる。

そこで`hasUnguardedTry`で局所ラムダに遮られていない`try`を検出し、必要なら`pipeTryMixed`でチェーンを分割する。明示的`try`のある部分は元の境界で評価し、安全な連続区間は局所境界内で融合する。制御効果のスコープを保ったまま変換する操作である。

参照: [`lower/expr.go`](https://github.com/podhmo/gom/blob/main/internal/lower/expr.go)、[`pipe-try-segmented-fusion`](https://github.com/podhmo/gom/blob/main/docs/decisions/2026-11-03-pipe-try-segmented-fusion.md)。

## 12. 末尾再帰と初期化依存グラフ

### 12.1 Tail position

関数の結果をそのまま返す位置を末尾位置（tail position）という。自己再帰呼び出しがその位置なら、呼び出しを`TailCall`としてループへ変換できる。戻ってからさらに演算する位置や、他の引数の計算の内側は同じ変換ができない。ANF化後のCoreを調べることで位置を明確に判断できる。

### 12.2 `@tailrec`が要求する保証

`@tailrec`は単に変換を試みる指示ではなく、変換が成立することを要求する。少なくとも一つの末尾自己再帰があること、許されない非末尾自己再帰がないこと、関数値として自己参照しないこと、相互再帰により保証が崩れないことを検査する。

相互再帰は参照グラフの強連結成分（strongly connected component, SCC）で考えられる。SCCは各頂点から他の頂点に相互に到達可能な集合である。関数・変数の参照関係が循環し、自分だけの成分ではないなら、単一関数の自己再帰をループにするだけでは一般にスタック消費を防げない。`CheckTailRecMutual`はパッケージ横断でこれを調べる。

### 12.3 パッケージ初期化循環

Goのトップレベル変数初期化には依存順がある。`CheckInitCycles`はCoreの変数・関数の参照関係を調べ、関数本体を経由する参照も含めて変数間の循環を検出する。元のGomの宣言ではなく生成するCoreの宣言形を基礎にするため、実際のGo出力に近い依存を調べられる。

参照: [`lower/tailrec.go`](https://github.com/podhmo/gom/blob/main/internal/lower/tailrec.go)、[`lower/tail.go`](https://github.com/podhmo/gom/blob/main/internal/lower/tail.go)、[`lower/initcycle.go`](https://github.com/podhmo/gom/blob/main/internal/lower/initcycle.go)。

## 13. データ型をGoへ写像する

一般的なGomのADTはGoで閉じたインターフェースとバリアントごとの構造体として表現する。概念的には次のようになる。

```go
type Shape interface { isShape() }
type Shape_Circle struct { Radius float64 }
type Shape_Rect struct { W, H float64 }
func (Shape_Circle) isShape() {}
func (Shape_Rect) isShape() {}
```

`match`は`type switch`などへ変換される。コンストラクタは構造体の生成、分解はフィールドの取得である。`Result`は例外的に専用のタグ、成功値、失敗値を持つ表現を使い、`SwitchTag`で分岐する。`Unit`は`struct{}`、Gomの`Int`は`int64`に対応し、Goの`int`とは区別される。

型の論理的同一性と、Goでの物理的表現を混同してはいけない。型の写像は`codegen/types.go`にあり、選択した表現に応じてCoreに別のノードが現れる。コード生成が毎回元のGom構文から型表現を選び直すわけではない。

参照: [`codegen/types.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/types.go)、[`codegen/decl.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/decl.go)。

## 14. Goコード生成: destination passing

### 14.1 式の結果をどこへ置くか

Goの`if`や`switch`自体は値を返さない。一方Gomには値を返す`if`や`match`がある。`codegen/emit.go`は式を出力する際に宛先（destination）を渡す。宛先には`Return`、`Assign(name)`、`Declare(name)`、`Discard`、リスト構築用の宛先がある。

```text
概念的なCore:
  let label = if n > 0 then "pos" else "neg"
  tail = label

概念的なGo:
  var label string
  if n > 0 { label = "pos" } else { label = "neg" }
  return label
```

各分岐は同じ宛先へ代入し、末尾ならreturnする。不要な一時クロージャを挿入しなくても式指向の意味をGoの文へ変換できる。使われていない中間値でも副作用は実行する。Goは未使用ローカル変数を拒否するので、値が不要な場合には束縛の宣言を省き、評価だけを出力する。

### 14.2 Go全体を`go/ast.File`で組み立ててはいない

現行実装の重要な詳細として、Goソースはハイブリッド方式で作る。型や式の一部を`go/ast`で表し`go/printer`で書き出す一方、宣言や制御文の多くはコード生成バッファへ直接テキストとして追加する。最後に`codegen.render`が`go/format.Source`で整形する。`Output.ParseFile()`は完成済みの出力をGo ASTへ再解析する補助関数である。

そのため、既存の`docs/design.md`にある「Core IR → go/ast」という説明は、現行のコード生成経路を厳密には表していない。今回の調査記録で文書と実装の差として扱った。

### 14.3 ソース位置の保持

すべてのCoreノードが`diag.Span`を保持する。出力時には一時的な`//gomsrc`印を入れ、`go/format`の後に`//line path.gom:N`へ置換する。この印を利用して`SourceMap`のアンカーも作る。生成Goのエラーやpanicの位置をGomソースへ対応させるためである。

`test`宣言と`@test`ヘルパーは`_test.go`へ分離でき、`TestMapping`がGom側のテスト名と生成したGoテスト関数名を対応させる。

参照: [`codegen/codegen.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/codegen.go)、[`codegen/gen.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/gen.go)、[`codegen/emit.go`](https://github.com/podhmo/gom/blob/main/internal/codegen/emit.go)。

## 15. 簡単な例を段階別に追跡する

次の例は構造を説明するためのものであり、出力コードとIRの具体的な綴りは実際のダンプで確認する。

```gom
package main

type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)

fn area (s: Shape) -> Float = match s {
  Circle r -> 3.14159 * r * r
  Rect w h -> w * h
}
```

| 段階 | 処理の結果 |
| --- | --- |
| parse | `TypeDecl`、`FnDecl`、`Match`、`CtorPat`、乗算式のAST |
| resolve | `Shape`、各コンストラクタ、`area`、`s`、`r/w/h`のシンボル束縛 |
| check | 対象とパターンの型、各演算と各腕の`Float`型を確定 |
| pattern | `Circle`と`Rect`がバリアントを覆うことを確認 |
| lower | コンストラクタ別の`DSwitch`、フィールドの取り出し、腕の`DLeaf`を組み立てる |
| codegen | Goの型宣言、`type switch`、分岐内の計算とreturnを出力する |

この例で`check`は型と呼び出し規約、`pattern`は値の集合に対する被覆、`lower`は実行する分岐構造をそれぞれ扱う。三者を同じ「matchの処理」として一括りにしないことが重要である。

## 16. 実装を読みながら検証する方法

コードを読む際は、まず`build.Compile` / `CompileFiles`を確認し、次に`resolve.Files`、`check.Files`、`check.unify`、`check.Apply`の順で追う。その後、静的検査の`pattern/matrix.go`と実行木生成の`lower/pat.go`を比較し、`lower/expr.go`のANF化・呼び出し分類を追って、`core.Validate`、`codegen/emit.go`へ進む。

`gom dump`には`syntax`、`resolve`、`check`、`core`の四フェーズがある。

```sh
gom dump -phase=syntax main.gom
gom dump -phase=resolve main.gom
gom dump -phase=check main.gom
gom dump -phase=core main.gom
gom check main.gom
gom build main.gom
```

ダンプはS式風のツリーで、`name%N`は同一シンボルを、`@start:end`はUTF-8のバイト範囲を示す。`pattern`は独立したIRを作るフェーズではないため、専用の`dump -phase=pattern`はない。複数ファイルのパッケージでは、`gom dump`も同じディレクトリの兄弟ファイルをコンパイル単位に含める。

変更の検証では、型付きAST・Core・生成Goの差分をそれぞれ見て、Goコンパイルと実行結果による確認を加える。`core.Validate`の不変条件テストだけで実行意味の一致を保証したとはしない。

## 17. 理論用語と実装の対応

| 理論概念 | 意味 | 対応箇所 |
| --- | --- | --- |
| スコープ | 位置ごとの名前と宣言の環境 | `resolve/scope.go` |
| シンボル同一性 | 同名でも異なる宣言を区別する識別 | `resolve.Symbol` |
| 単一化 | 二つの型を等しくする代入の計算 | `check/unify.go` |
| occurs check | 無限型となる自己包含を禁止する | `check/unify.go` |
| 一般化・具体化 | `let`の型変数の量化と使用時の新しい未知型への置換 | `check/check.go` / `check/unify.go` |
| value restriction | 多相化を許す`let`の値の条件 | `check/check.go` |
| 固定点反復 | 解決が進まなくなるまで保留制約を再訪 | `check.finalize` |
| パターン行列 | 値集合の被覆を再帰的に計算 | `pattern/matrix.go` |
| 決定木 | 実行用の条件分岐を表す木 | `lower/pat.go`, `core.DTree` |
| ANF | 複合評価を順序付き束縛へ分離 | `lower/expr.go`, `core.Body` |
| η展開・uncurry | 関数値の表現を変換する | `lower/expr.go`, `lower/flat.go` |
| 制御効果の境界 | `try`の脱出先を明示する | `core.EscapeResult`, `core.EscapeCall` |
| SCC | 互いに到達できる参照グラフの成分 | `lower/tailrec.go` |
| destination passing | 値の格納先を指定して文を生成する | `codegen/emit.go` |
| source map | 生成位置をソース位置に対応付ける | `diag.SourceMap` |

## 18. 設計変更時に確認すべきこと

1. 新しい構文は既存の型付きASTに正しく意味を持たせられるか。そうでなければ`syntax`・`check`で表現を追加する。
2. 定義の同一性を`resolve`で固定できるか。受け手の型が必要な参照は`check`で保留すべきか。
3. 型制約を解くには何が必要か。期待型、デフォルト型、Goとの変換が既存の規則と干渉しないか。
4. パターンの網羅性と実行用の決定木を別々に定義・検証できるか。
5. Coreに評価順序、部分適用の実行タイミング、脱出先、Go境界の変換を明示できるか。`core.Validate`で不正形を検出できるか。
6. 最後にGo出力を決める。`codegen`へ新しい意味規則を持ち込まず、Goによる型検査・実行テストまで行う。

本調査中に確認した設計文書とコードのずれ、および未検証の改善候補は別紙[`docs/sketch/ja/2026-11-20-transpiler-pipeline-review.md`](../sketch/ja/2026-11-20-transpiler-pipeline-review.md)に分離した。GitHubの接続権限で403エラーとなり、リポジトリへの書き込みは未実施である。

## 19. 参考資料

- [Gomの言語仕様](https://github.com/podhmo/gom/blob/main/docs/spec.md)
- [Gomの実装設計](https://github.com/podhmo/gom/blob/main/docs/design.md)
- [決定の記録](https://github.com/podhmo/gom/tree/main/docs/decisions)
- Luc Maranget, *Warnings for pattern matching*, Journal of Functional Programming, 2007（実装の`pattern/matrix.go`が明示している直接のアルゴリズム上の基礎）
