# Gom 設計ドキュメント

この文書はコンパイラの実装方針と、言語設計の理由を書く。利用者から見える振る舞いの正は [spec.md](spec.md)。個々の決定の経緯は [decisions/](decisions/) にある。

---

## 1. 設計目標

**v1 で必須**

- sum types とレコードを第一級で持つ
- `match` の網羅性チェックをコンパイルエラーとして強制する（警告ではない）
- すべての構文要素が値を返す式指向
- Hindley–Milner ベースの型推論
- イミュータブルがデフォルト
- 生成される Go コードは `go build` / `go vet` を通り、次の 2 つを満たす
  - **デバッグ可能**: 障害時に Go コードを読めば何が起きたか分かる（名前付け、素直な制御フロー、ソース対応）
  - **Go から使える**: Go のコードから自然な API で Gom のパッケージを使える
- Go パッケージを宣言なしで、Go と同じ書き方で呼び出せる
- コンパイラのメッセージは AI エージェントが読むことを第一に想定する（既定で JSON）

**非目標（v1 では扱わない）**

- 遅延評価（正格評価のみ）
- 型クラス / トレイト、高カインド型、GADTs
- エフェクトシステム、独自ランタイム、独自 GC
- OCaml 流ファンクタ（モジュールは Go パッケージに 1:1 で対応させる）
- 生成された Go コードを人が直接書き換えて保守すること（生成コードは読める成果物であり、ソースではない。これにより表現方式や最適化の自由度を確保する）

**最初の成功条件**: 多くの言語機能を持つことではなく、少数の ML 的な機能を使ったプログラムが、普通の Go プログラムと無理なく共存すること。

**型検査の位置づけ**: Gom の型検査は Go から独立している。Go が Gom と同じ型規則を再検査できる必要はないが、生成コードは必ず Go の型検査を通る。Gom の型を Go で直接表現できない場合は、具体化やラッパー生成で表現する。

### 1.1 他言語からの取捨

| 機能 | 由来 | 採否 |
| --- | --- | --- |
| ADT・パターンマッチ・let 式 | OCaml / Haskell 共通 | 採用 |
| 並置による関数適用 `f x y`（カリー化） | Haskell / OCaml | 採用 |
| パイプ `\|>` `<\|` と関数合成 `>>` `<<` | F# | 採用 |
| 失敗し得る処理のパイプ `\|?>` | F# の `Result.bind`＋Go の `%w` | 採用（独自の演算子。`GoError` は自動でラップ） |
| Go 呼び出しの `_` プレースホルダ `f(_, 10)` | Scheme の `cute`（SRFI 26）/ Scala | 採用（Go 関数のみ。`_` 以外の引数は作成時に評価） |
| ラベル付き引数 `~x` | OCaml | 必須ラベルのみ採用（省略可能引数 `?x` は不採用） |
| `.mli` 相当のインターフェースファイル | OCaml | 採用（`.gomi`。ライブラリパッケージでは必須） |
| ファイル内テスト | Rust | 採用（`test "名前" { ... }`） |
| 設定項目のない公式フォーマッタ | Go（gofmt）/ Elm | 採用（`gom fmt`） |
| オフサイドルール | Haskell | 不採用（インデントは意味を持たない） |
| `where` 節・`$`・型クラス | Haskell | 不採用（型クラスは v2 以降で検討） |
| `let ... in`・`begin ... end`・`;;` | SML / OCaml | 不採用（波括弧ブロックに一本化） |
| ミュータブル参照 `ref` | OCaml | `Ref a` 型として限定採用 |

---

## 2. コンパイラアーキテクチャ

コンパイラは 8 フェーズの一直線のパイプラインで、中間表現は「表層 AST → 名前解決済み AST → 型付き AST → Core IR → go/ast」の 5 段階。パターン検査は型情報が必要なので型検査の直後、脱糖の前に置く。

```
.gom ─► 1 字句解析 ─► 2 構文解析 ─► 3 名前解決 ─► 4 型検査・推論
                                                        │
        go build ◄─ 8 出力 ◄─ 7 Go コード生成 ◄─ 6 脱糖・match コンパイル ◄─ 5 パターン検査
```

| フェーズ | 内容 |
| --- | --- |
| 1 字句解析 | ソース → トークン列 |
| 2 構文解析 | 再帰下降＋Pratt 法で表層 AST。エラー回復付き |
| 3 名前解決 | 識別子をスコープとモジュールに束縛。依存パッケージの `.gomi` と Go パッケージの型を読み込む |
| 4 型検査・推論 | HM＋単一化で型付き AST。数値演算子の型をここで確定 |
| 5 パターン検査 | 網羅性・冗長性を usefulness で判定。失敗したら反例付きでエラー |
| 6 脱糖・match コンパイル | パイプ・`try`・`\|?>` を展開し、match を決定木へ。Core IR（ANF 風）を作る。テストを分離 |
| 7 Go コード生成 | 宛先渡しで式を文へ変換。Core IR → go/ast |
| 8 出力 | `go/printer` で整形、`//line` を付与 |

| 中間表現 | 生成フェーズ | 主な特徴 |
| --- | --- | --- |
| 表層 AST | 2 構文解析 | ソース位置をすべて保持。コメントと空行を trivia として持ち、`gom fmt` と共用する。糖衣構文を残す |
| 名前解決済み AST | 3 名前解決 | すべての識別子がシンボル（定義位置、Gom か Go か、テスト用スコープか）を指す |
| 型付き AST | 4 型検査 | 全ノードに型を付与。適用ノードには呼び出し情報（呼び出し対象のシンボル、型引数、適用された引数の数、未適用の引数、Gom / Go の呼び出し規約、直接呼び出しできるか）を構造化して持つ。パターン検査とエラー表示はこの段階で行う |
| Core IR | 6 脱糖 | 糖衣なし・ANF 風。match は決定木、適用は arity 情報付き |
| go/ast | 7 コード生成 | Go 標準ライブラリの AST |

各フェーズは純粋関数として実装し、フェーズごとにゴールデンテストを置く。エラーはフェーズ 4 と 5 で集めて一括表示し、以降のフェーズはエラーのない入力だけを受け取る。

### 2.1 Core IR の不変条件

Go コード生成器に言語の意味を判断させない。次のことは Core IR の時点ですべて確定し、`codegen` は決められた手順で Go AST を出力するだけにする。

1. **評価順序**: 部分式はすべて、評価順に並んだ束縛（ANF）になっている。ラベル付き引数の並べ替え、`_` プレースホルダ、`select` の腕の式、パイプの展開はすべてここで順序付きの束縛に変換済み
2. **関数の arity と呼び出し規約**: 各呼び出しは「既知の関数への飽和呼び出し」「部分適用（生成するクロージャを明示）」「未知の関数値への残り arity の呼び出し」「Go 関数の呼び出し」のどれかに分類済み
3. **Go 相互運用の変換**: `(T, error)` から `Result` への変換、数値変換、`any` へのボックス化、スライスのコピー、interface への代入はすべて明示的なノード
4. **match は決定木**: パターンは残っていない。ADT の不正値に対する panic の分岐も明示
5. **脱出**: `try` と `|?>` は、どの関数境界から抜けるかを明示した脱出ノード
6. **型の具体化**: 多相な定義と、実行時の関数値を区別する。多相な定義を値として使う箇所では型引数が具体化済みで、必要なラッパーも確定済み（関数値の表現は [decisions/function-value-representation](decisions/2026-11-02-function-value-representation.md)、多相定義との区別は [decisions/function-representation](decisions/2026-11-01-function-representation.md)）
7. **エントリポイントの判定**: `package main` の `fn main () -> Unit` という言語規則は `FuncDecl.IsEntry` として確定済み。codegen はパッケージ名・関数名を見ず、このフラグだけで `func main()` を出力する（[decisions/fn-main-entrypoint](decisions/2026-11-04-fn-main-entrypoint.md)）
8. **等価のレシピ**: `==` / `!=` / `assert_eq` がどう比較するか（ネイティブ `==`・ランタイムのフォールバック・エラーの文字列比較・生成した比較関数の呼び出し）は `core.Equal` / `CallBuiltin.Eq` のプランとして確定済み（4.9 節）
9. **ソースの由来**: すべてのノードが元の Gom ソースの範囲を指す

例: ラベル付き引数の並べ替え。

```
// Gom
transfer ~amount: (expensive_a ()) alice ~to: (expensive_b ())

// Core IR（評価はソース順、呼び出しは宣言順）
let t1 = expensive_a ()
let t2 = alice
let t3 = expensive_b ()
call transfer(t2, t3, t1)
```

### 2.2 ソース位置の対応

内部では「Gom ソースの範囲 → Core IR の由来 → 生成した Go ソースの範囲」の対応表を持つ。`//line` ディレクティブはこの対応を Go ツールチェーンに伝える手段の 1 つにすぎず、`go build` / `go vet` のエラーを Gom の位置に戻すときや、panic のスタックの変換にも対応表を使う。位置の正は UTF-8 のバイトオフセットで、行・列や UTF-16 の位置は出力時に計算する。

### 2.3 診断の内部表現

診断はコンパイラ内部のデータ型（`internal/diag`）を正とし、JSON Lines・text・LSP（将来は SARIF など）はそこからの変換にする。コンパイラ本体は出力形式を知らない。

### 2.4 実装言語

コンパイラは Go で実装する。`go/ast`・`go/printer`・`go/types`・`go/importer` をそのまま使えるので、Go パッケージの読み込みと Go コードの出力を自前で書かずに済み、配布も単一バイナリで済む。Go には sum type とパターンマッチがないため、コンパイラ内部の AST は sealed interface＋type switch で表す（Gom が生成するコードと同じ形）。type switch の網羅性は go-sumtype のような静的検査ツールで補う。ツールが実際に漏れを検出することを確かめるため、AST にバリアントを足したときに CI が失敗するテストを置く（Go のバージョンとジェネリック型への対応も確認する）。

### 2.5 Go パッケージの読み込み

`internal/goimport.Loader` は `import go` されたパッケージを、`go list -export` で得た export data と `go/importer.ForCompiler` で読み、`go/types` の情報から Gom の型を自動で作る（[採用済みの読み込み方式](decisions/2026-11-03-import-go-loading-mechanics.md)）。`go/packages` への依存は追加しない。`Config.Dir` は対象モジュールのディレクトリ、`Config.GoBin` は利用する Go コマンドを指定し、標準ライブラリだけでなく `replace` を含むモジュール依存も同じ経路で扱う。

`types.Identical` は named type の object 同一性を比較するので、1 回のコンパイルでは 1 つの `Loader`（`token.FileSet` と importer）を共有し、シンボルや import ごとにローダーを作り直さない。Gom 側の named type はパッケージパスと宣言名を持つ参照で表し、ローダーが同じ参照に対応する `go/types.Named` を保持してメソッド解決に使う。Go の alias は元の型へ正規化し、`error` への alias も単一 error / 末尾 error の戻り値規約を引き継ぐ（[error alias](decisions/2026-11-03-go-error-alias-results.md)）。

表現できない型を含むシンボルも理由を付けて名前解決の表に残し、参照されたときだけ E0501 を出す。パッケージとシンボルはローダー内でキャッシュし、export data は Go のビルドキャッシュを利用する。`extern` 宣言があれば導出したバインディングを上書きする。依存する Gom パッケージについては、実装ではなく `.gomi` だけを読み込む。

### 2.6 Builtin type identity

`types.Con{Qual: "", Name: "Result"}` cannot tell the builtin `Result` from a file-local `type Result`. When a file declares a `type` under a builtin name, the unqualified name denotes only the local declaration file-wide ([decisions/builtin-type-identity](decisions/2026-11-02-builtin-type-identity.md)). Every "is this `Con` the builtin X?" question consults the phase's own declaration index before the name:

- `check`: `typeInfo.sym` on `tyCons` (non-nil for local declarations) → `shadowedBuiltin` / `isBuiltinCon`
- `lower`: `typeD` → `declaredT` / `builtinName` / `isResultCon` / `isOptionCon`; `variantsOf` and the equality plans (`eqKindOf`) also try local declarations first
- `core.Validate`: `localT` collected from the file's `TypeDecl` names → `conIs` / `resultType`
- `codegen`: `g.localT`; Core carries the semantic decisions, so the guards only pick the generated representation
- `eval`: `interp.localT` → `VVariant.IsResult` / `resultTag` / `errIsGoError` (the evaluator and generated code share the same reading)

Constructs that specifically need a shadowed builtin — `try`/`|?>`, builtin constructors, list literals, `for`/`select`, foreign signatures mentioning the name, and so on — report E0415 in check, and the validator rejects the same reading on hand-built IR.

---

## 3. ディレクトリ構成

```
gom/
├── go.mod
├── CLAUDE.md
├── TODO.md
├── cmd/
│   └── gom/          CLI（build, check, test, fmt, fix, gomi, explain, schema）
├── internal/
│   ├── syntax/       字句解析・構文解析・表層 AST（コメントと空行を trivia として保持）
│   ├── format/       gom fmt（プリティプリンタ）
│   ├── resolve/      名前解決、.gomi と Go パッケージの読み込み
│   ├── goimport/     go/importer・go/types から Gom の型を作る、extern による上書き
│   ├── types/        型の表現、単一化、HM 推論、ラベル付き引数
│   ├── check/        型検査（型付き AST を作る）
│   ├── pattern/      網羅性・冗長性の判定と反例の生成（match、let、関数引数で共用）
│   ├── iface/        .gomi の検査とハッシュ、gom gomi の下書き生成
│   ├── core/         Core IR の定義
│   ├── lower/        脱糖、決定木へのコンパイル、arity 解析、テストの分離
│   ├── codegen/      Core IR → go/ast（宛先渡し、sum type、_test.go の出力）
│   ├── diag/         診断の型、JSON / text 出力、エラーコード表、JSON Schema
│   ├── build/        パッケージグラフ、インクリメンタルビルド、キャッシュ、go build / go test の呼び出し
│   └── tool/         リポジトリのツール（同期ジェネレータなど。コンパイラのフェーズではない）
├── rt/               生成コードが import する補助（Tuple、Task、アサーションなど）
├── stdlib/           Gom で書いた標準ライブラリ（list, option, result, chan, task）。各パッケージに .gomi
├── testdata/
│   ├── spec/         仕様の規則ごとの .gom と、期待する診断 JSON
│   ├── golden/       生成される Go コードのゴールデンファイル
│   └── fmt/          フォーマッタの入力と期待する出力
└── docs/
    ├── spec.md
    ├── design.md
    ├── open-questions.md
    ├── grammar.ebnf
    └── decisions/
```

- 依存の向きはパイプラインと同じく一方向: `syntax` → `resolve` → `check` → `pattern` → `lower` → `codegen`
- `diag` と `types` はどのフェーズからも使う共通部品。`build` がすべてを束ね、`cmd/gom` は `build` と `format` を呼ぶだけの薄い層にする
- `rt` は生成コードから import されるので `internal/` の外に置く。名前は Go 標準の `runtime` との衝突を避けたもの
- `stdlib` はコンパイラ自身でビルドするので、最初の大きな実用テストになる

---

## 4. Go へのコード生成

中心となる難所は 4 つ: sum types の表現、`match` の展開、式を Go の文へ落とすこと、カリー化された関数呼び出し。

### 4.1 sum types → sealed interface＋バリアント struct

```go
// type Shape = | Circle (radius: Float) | Rect (w: Float) (h: Float) | Empty
type Shape interface{ isShape() }

type Shape_Circle struct{ Radius float64 }
type Shape_Rect struct{ W, H float64 }
type Shape_Empty struct{}

func (Shape_Circle) isShape() {}
func (Shape_Rect) isShape()   {}
func (Shape_Empty) isShape()  {}
```

非公開メソッド `isShape()` によりパッケージ外から新しいバリアントを足せない（sealed）。ただし Go 側では `var s Shape` が `nil` になり得るので、type switch の `default` は `panic("unreachable")` ではなく、型名を含む契約違反のメッセージで panic する（`rt.InvalidVariant("shapes.Shape", s)`）。引数なしバリアントはゼロサイズ struct なので割り当ては発生しない。バリアントはポインタ型ではなく値 struct にし、Go 側から typed nil が渡されても `default` で `rt.InvalidVariant` に落ちるようにする。

ジェネリックな ADT では、マーカーメソッドのシグネチャに型パラメータを含める。シグネチャに出ない型パラメータは phantom になり、呼び出し側の型推論が束縛できない——`Option[int64]` の値が `Option[int]` の引数を構造的に満たしてコンパイルが通り、実行時に `default` へ素通りする静かな誤推論が起きる（P0-1・P0-3 で確認。全 generic sealed ADT に効く罠）。

```go
// type Option a = | Some a | None
type Option[A any] interface{ isOption() A }

type Option_Some[A any] struct{ V0 A }
type Option_None[A any] struct{}

func (o Option_Some[A]) isOption() A { return o.V0 }
func (Option_None[A]) isOption() A   { var z A; return z }
```

組み込みの `List a` と `Result a e` はこの sealed interface ではなく別の表現にする: `List a` は `type List[A] []A`（スライス）、`Result a e` は `struct { state ResultTag; Value T; Error E }`（非公開の 3 状態タグ付き。4.6 節）。`Result` は保持するデータではなく失敗しうる呼び出し規約なので、構築・判定・分解が安い専用表現にする（[decisions/value-representation](decisions/2026-11-01-value-representation.md)）。

### 4.2 match → 決定木 → type switch

パターン行列を Maranget の決定木コンパイル（"Compiling pattern matching to good decision trees", 2008）で展開し、各分岐ノードを Go の `switch x := v.(type)` か値 `switch` にする。網羅性は検査済みなので、`default` には Go から来た不正値のための panic だけを置く。

```go
func area(s Shape) float64 {
	switch s := s.(type) {
	case Shape_Circle:
		return 3.14159 * s.Radius * s.Radius
	case Shape_Rect:
		return s.W * s.H
	case Shape_Empty:
		return 0.0
	default:
		panic(rt.InvalidVariant("shapes.Shape", s))
	}
}
```

網羅性と冗長性の判定は Maranget の usefulness アルゴリズム（"Warnings for pattern matching", JFP 2007）で行う。

1. 型検査後の型付き AST に対して実行する（コンストラクタ集合は型から決まる）
2. パターン行列 P とベクタ q に対し `U(P, q)`（q が P に対して有用か）を再帰的に計算する
3. 網羅性: `U(P, _)` が真なら非網羅。同じ再帰で反例パターンを構築する
4. 冗長性: 各腕 i について `U(P[0..i), p_i)` が偽ならその腕は到達不能
5. Int / String などシグネチャが無限の型は完全なシグネチャではないと扱い、デフォルト行列へ進む
6. ガード付きの行は、網羅性でも冗長性でも、後続の行を覆う行列からは除いて判定する（ガードが常に真とは限らないため）

最悪計算量は指数だが、実用的なコードでは問題にならないことが OCaml / Rust で実証済み。反例の列挙は先頭 5 件で打ち切る。

### 4.3 式指向 → 文: 宛先渡し（destination passing）

Go の `if` / `switch` は値を返さないため、IR の各式を「結果をどこへ置くか」という宛先付きで生成する。宛先は `Return`（関数末尾）、`Assign(tmp)`（一時変数へ代入）、`Discard`（Unit を捨てる）の 3 種類。値位置に `match` が来たら `var tmp T` を宣言し、各分岐を `Assign(tmp)` で生成する。即時実行クロージャ（IIFE）は使わない。読みにくく、エスケープ解析も悪化するため。

```go
// let label = if n > 0 { "pos" } else { "neg" }
var label string
if n > 0 {
	label = "pos"
} else {
	label = "neg"
}
```

### 4.4 カリー化 → 引数の数（arity）に基づく呼び出し

トップレベル関数は、宣言どおりの数の引数を取る Go の多引数関数として出力する。

| 呼び出しの形 | 生成する Go |
| --- | --- |
| 既知の関数に引数がちょうど揃う | 直接呼び出し `transfer(alice, bob, m)` |
| 引数が足りない（部分適用） | 残りを 1 引数ずつ受け取る入れ子クロージャ `func(p) ... { return transfer(alice, bob, p, ...) }` |
| 引数が多い（結果が関数） | 直接呼び出しの結果に残りを 1 引数ずつ適用 |
| 未知の関数値への適用 | 1 引数ずつの適用 `f(x)(y)` — 関数値自体がカリー化形なので |

関数値の表現はカリー化形（[decisions/function-value-representation](decisions/2026-11-02-function-value-representation.md) — P0-2 の flat 形を部分的に Supersede）: Gom の `T1 -> ... -> Tn -> R` の値は Go の `func(T1) func(T2) ... R` とし、矢印 1 本につき Go の関数を 1 つ生成する。`types.Equal` は矢印チェーンを平坦化して同一視するため、`Int -> Int -> Int` と `Int -> (Int -> Int)` は同じ型——表現を平坦化済みチェーンの純粋関数にすると、どの綴りでも同じ Go 型になり、境界に変換は要らない（P0-2 の flat 形は宣言 arity で表現を選んだため、check を通った `stage make` が型検査の通らない Go を生成していた）。宣言は従来どおり宣言 arity の Go 関数を出力する。値として参照できるのは宣言 arity 1 のときだけで、2 以上は `func(p0) func(p1) ... { f_(p0, p1, ...) }` のアダプタを生成する——アダプタの最後の矢印が宣言チャンク全体を起動するので、本体の副作用が最初のチャンク適用時に走る順序は変わらない。`.gomi` には引数リストの形でシグネチャを書くので、他パッケージの関数についても arity が分かる。

### 4.5 その他の変換

- Go 関数の呼び出し → タプルを作らず `pkg.F(a, b)` に直接変換する。数値型の変換、`any` へのボックス化、`(T, error)` から `Result` への変換もここで挿入する
- `_` プレースホルダ → `_` 以外の引数を一時変数に評価してから、それを捕捉するクロージャを作る
- ラベル付き引数 → ラベルは消去し、Go 関数の引数は宣言順。呼び出し側の評価順を保つため、並べ替えは一時変数で行う
- パイプ・関数合成 → 脱糖の段階で普通の関数適用に戻す。`>>` で作った関数を値として渡す場合だけクロージャになる
- `try e` → `r := e` として `r.Tag()` で分岐する。`result.ResultTag_Err` なら `return result.Err[...](r.Error)` で最も内側の関数から脱出、`result.ResultTag_Ok` なら `r.Value` に進む。分岐は invalid（タグ 0）を含む完全なものにし、`default` は `rt.InvalidVariant` で panic する
- 自己末尾再帰 → 引数を再代入する `for` ループ。相互再帰は保証しない。A parameter a nested `Closure` or `Spawn` can capture is copied to a fresh per-iteration binding at the top of the loop body and every use is rebound to it (internal/lower/tail.go `snapshotCapturedParams`) — without the copy a capture would read the value of the *last* iteration rather than the call that created it (docs/problems/2026-11-02-tailloop-param-capture.md)
- `@tailrec` → 変換とは別に、ファイル全体を lower し終えて `markTailCalls` が走ったあとで検査するパス（internal/lower/tailrec.go）。残った自己 `CallDirect` があれば E0902、`FuncRef` で値として参照されていれば E0903、ファイル内の直接参照グラフの強連結成分に自分以外がいれば E0904、変換可能な自己末尾呼び出しがなければ E0905。宣言 arity が 2 以上の関数を値として使う場合（`let g = f`）は `FuncRef` ではなく adapter の `Closure` 列が出るが、その末端の `CallDirect` は `Adapter` フラグを持つので同じく E0903 を報告する — adapter の呼び出しは「値の発火呼び出し」であって R1/R2 の呼び出し箇所ではなく、`go` の中でも R3 の免除はない（値はゴルーチンを離れられる）。`Spawn` の中の自己呼び出しは R2/R4 の対象外（新しいスタックで走るため）で、変換もできないので R1 の証拠にもならない。`markTailCalls` の出力を読むので、ANF が `let` に落とした呼び出しは自動的に末尾位置から外れ、検査が codegen とずれない。R4 のグラフは `FuncDecl` だけでなく `VarDecl` も頂点にする — パッケージレベルの `let` は関数値を保持できるので、`let g = f` や束縛パターン越しの参照巡回（`f` → `g` → `f`）は普通の相互再帰と同じくスタックを増やす。辺は `CallDirect`・`FuncRef` に加えて `VarDecl` を指す `Var` で、`CallFunc` の被呼 `Var` もここに含まれる。`VarDecl` 側の辺だけは初期化時呼び出しと値の振る舞いを区別する — 本体トップレベルの `CallDirect`（`let r = f 0` は init 時に一度走るだけで、保持された値が `f` を呼び戻すわけではない）は辺にせず、内側の `Closure` の中の呼び出しだけを数える。`FuncRef` と `Var` の辺は位置を問わず数える — 値として渡された関数は活性化内で適用され得る、という既存の保守的近似と同じ根拠
- ラムダ → Go の関数リテラル。捕捉変数はすべて不変なのでコピー意味論の問題は起きない（the TailLoop loop registers are the exception — they are rebound every iteration, which is why the snapshot above exists）
- トップレベルの多相関数 → 型パラメータ `[A any]`。一般化した `let` の非関数値（`let e = []`）は niladic のジェネリック関数 `func e[A any]() list.List[A]` として出力し、参照側は `e[T]()` の呼び出しにする。使用箇所の型引数は checker が型付き AST の `TypeArgs` に記録し、Core IR の `CallDirect`/`FuncRef`/`CtorApp` へそのまま移す（[decisions/let-polymorphism-emission](decisions/2026-11-02-let-polymorphism-emission.md)）
- 生成コードには `//line foo.gom:12` ディレクティブを埋め込み、panic のスタックトレースとデバッガを元ソースへ対応させる

### 4.6 `|?>`

各ステップを `err` の確認と早期リターンに展開し、中間の `Result` 値は作らない。`Result` は言語上は stdlib の sum type だが、Go 側の `Result`/`ResultTag`/コンストラクタはコンパイラが生成する標準ライブラリの `result` パッケージに置く（`List` など他の組み込み型の Go 側表現も stdlib 側の特別扱いとして生成する）。表現は `struct { state ResultTag; Value T; Error E }`（`state` は非公開、0=invalid・1=Ok・2=Err）で、コンストラクタは `result.Ok[T,E](v)` / `result.Err[T,E](e)`、判定は `r.Tag()`（[decisions/value-representation](decisions/2026-11-01-value-representation.md)）。ステップが返す `Result` の検査は必ず invalid を含む完全な分岐にする——bare な comma-ok 型アサーション（`r.(Result_Err[...])` 型）は invalid 値を Ok 側のパスに流すので使わない（P0-4 で確認）。

```go
// load_config の生成コード（抜粋）
b, err := os.ReadFile(path)
if err != nil {
	return result.Err[Config, error](fmt.Errorf("os.ReadFile: %w", err))
}
r := parseToml(b)
switch r.Tag() {
case result.ResultTag_Ok:
	// r.Value を使って続行
case result.ResultTag_Err:
	return result.Err[Config, error](fmt.Errorf("parse_toml: %w", r.Error))
default:
	panic(rt.InvalidVariant("result.Result", r))
}
```

### 4.7 並行処理

`go { e }` は `go func() { ... }()`、`Chan a` / `RecvChan a` / `SendChan a` は `chan A` / `<-chan A` / `chan<- A`、`Chan.recv` は `v, ok := <-ch` と `Option` の構築になる。`select` 式は宛先渡しで Go の `select` 文に変換する。各腕は Go の `case` に 1 対 1 で対応する。受信腕は `case v, ok := <-ch:` から `Option` を作り、腕の本体（中の `match` を含む）を通常どおり生成する。チャネル式と送信値は Core IR の時点で順序付きの束縛になっており、Go の評価規則と回数が一致する。

### 4.8 テスト

- `gom build`: 型検査のあと、脱糖の段階で `test` と `@test` 定義を取り除く
- `gom test`: `area.gom` から `area.go`（本体）と `area_test.go`（テスト）を出力する。テストは外部テストパッケージではなく同じ Go パッケージに入る
- `test "rect area"` は `func TestRectArea(t *testing.T)` になる。名前が衝突したら番号を付ける
- `_test.go` にも `//line` を入れる
- `gom test` は内部で `go test -json` を実行し、結果を Gom の JSON メッセージ（`kind: "test"`）に変換する
- `assert_eq` が失敗時に値を表示できるよう、表示用の関数をコンパイラがすべての型について自動生成する（利用者からは見えない）

```go
// area_test.go（生成）
package shapes

import (
	"testing"

	"github.com/example/gom/rt"
)

func unitSquare() Shape { return Shape_Rect{W: 1.0, H: 1.0} }

func TestRectArea(t *testing.T) {
//line area.gom:15
	tmp1 := area(Shape_Rect{W: 2.0, H: 3.0})
	rt.AssertEq(t, tmp1 == 6.0, tmp1, 6.0)
}
```

### 4.9 構造的等価の生成

`x == y` / `x != y` は `core.Equal` に下ろされる。`How` に載せる比較レシピ（`EqPlan`）を lowering が被演算子の静的型から決める — codegen はそれをレンダリングするだけで、型を見て判断し直さない（2.1 節の不変条件）。レシピは4種: スカラーや全フィールドがネイティブ比較できる複合は Go の `==`（`EqNative`）、`GoError` は nil 対応のメッセージ文字列比較（`EqError`）、Gom が表現を握らない型（名前付き Go 型・`Task`・`Go.Any`）は `rt.Equal`（`EqRuntime`）、それ以外の複合型は型ごとに生成する比較関数 `gomEq<N>` の呼び出し（`EqCall`）。`gomEq<N>` は `File.EqDecls` に集めてファイル末尾に出力し、リストは要素ごと・レコード/タプルはフィールドごと・ADT はバリアントの type switch＋ペイロード比較になる。再帰型は比較関数どうしの再帰呼び出しで表す（ネストした比較は必ず `EqCall` 経由なので、プラン自体が再帰しない）。`assert_eq` も同じ判定を `CallBuiltin.Eq` に載せ、`rt.AssertEq(t, 判定結果, actual, expected)` を生成する — `assert_eq` は常に `==` と同じ答えを報告する。詳細は [decisions/2026-11-05-generated-equality-comparators](decisions/2026-11-05-generated-equality-comparators.md)。

---

## 5. インクリメンタルビルド

`.gomi` は型検査の境界として最初から使うが、ビルドのキャッシュは後回しにする。初期は毎回すべての Gom ソースを変換し、Go 側の再ビルドは `go build` のキャッシュに任せる。キャッシュを実装するときは、キャッシュキーに次を含める: Gom ソースのハッシュ、依存先の `.gomi` の意味的なハッシュ、コンパイラのコード生成方式のバージョン、`rt` と標準ライブラリの ABI、`extern` などの相互運用規則、対象の Go バージョンとビルド条件。

1. 各パッケージのインターフェース（`.gomi`）を検査し、正規化した構文木からハッシュを計算する。コメントと書式はハッシュに含めない
2. パッケージは、依存先の実装ではなくインターフェースだけを読んで型検査とコード生成を行う
3. 再コンパイルするのは、自分のソースか依存先のインターフェースのハッシュが変わったときだけ
4. テストと `@test` 定義はハッシュに含めない
5. 生成した `.go` が前回と同じ内容なら書き換えない。Go 側の再ビルドは `go build` のキャッシュに任せる
6. 依存関係のないパッケージどうしは並列にコンパイルする

`.gomi` は Gom の意味論的な型（公開 API）を表す。生成される Go の API（型名・関数のシグネチャ）をどこまで安定させるか（Go ABI の方針）は open-questions.md。

sum type にコンストラクタを足すとインターフェースが変わり、依存側は再コンパイルされる。依存側の `match` が非網羅になる可能性があるので、これは望ましい動作である。

---

## 6. フォーマッタ

`gom fmt` は構文解析器を共有し、表層 AST の trivia（コメントと空行）を使う。行幅を超える式だけを Wadler 流のプリティプリンタで折り返す。冪等性と往復一致（`parse(fmt(x)) = parse(x)`）は、ランダム生成した構文木に対するプロパティテストで検査する。インデントに意味がなく改行が区切りになる構文なので、改行位置の境界ケース、コメントを含む入力、パースエラーからの回復もテストする。

---

## 7. 検証の方針

- **意味論の比較テスト**: 小さな Gom 評価器（単相の純粋な式、ADT、`match`、関数適用から始める）を作り、生成した Go コードの実行結果と比べる。評価順序、パターンマッチ、部分適用の検証に使う
- **Go 相互運用テスト**: Gom → Go と Go → Gom の両方向。特に `nil`、スライス、named type、interface、ジェネリクス
- **生成コードの品質テスト**: `go build`、`go vet`、`go test -race`、ベンチマーク、エスケープ解析
- 既存のゴールデンテスト、仕様テスト（`testdata/spec/`）、フォーマッタのプロパティテスト

## 8. 参考文献

4.2 節が依拠する論文（2026-11-02 に確認。詳細は [sketch: reference-verification](sketch/2026-11-02-reference-verification.md)）:

- Luc Maranget. "Warnings for pattern matching". *Journal of Functional Programming* 17(3), May 2007, pp. 387–421. doi:10.1017/S0956796807006223 — 網羅性・冗長性の検査（usefulness アルゴリズム）と反例生成の根拠
- Luc Maranget. "Compiling pattern matching to good decision trees". *Proceedings of the ACM SIGPLAN Workshop on ML* (ML '08), Victoria, BC, Canada, September 2008, pp. 35–46. doi:10.1145/1411304.1411311 — パターン行列から決定木へのコンパイルとヒューリスティクスの根拠。Le Fessant & Maranget "Optimizing pattern matching" (ICFP 2001) は backtracking automata の別論文なので混同しないこと

内部 AST の sealed interface 網羅性検査（2.4 節）: フォークの alecthomas/go-check-sumtype を `//sumtype:decl` 注釈付きで採用した（B-3、scripts/check-sumtype.sh。選定経緯は [sketch: sumtype-check-selection](sketch/2026-11-02-sumtype-check-selection.md)）。BurntSushi/go-sumtype はメンテナンスが停滞しておりジェネリクス未対応。`default` が単一文の `panic(...)` なら網羅性検査が継続するので、内部 AST の type switch 規約と整合する。
