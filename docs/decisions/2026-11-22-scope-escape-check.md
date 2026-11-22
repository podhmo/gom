# Scope 値の脱出の静的検査（SC-2）

- 状態: 採用（実装・検証済み — `gom check` が E0419 を出す）
- 日付: 2026-11-22
- 問い: SC-1 で導入した `Scope` ハンドルがスコープの外へ漏れるのを、
  どう静的に止めるか（decisions/scoped-cleanup の回答項目 4–5）

## 決定

二本立ての規則で、診断はどちらも E0419。

### 型の位置の規則

`Scope` は**パラメータの葉の型**か**関数型の引数側の中**にだけ書ける。
結果型・レコードのフィールド・バリアントの payload・`Ref`/`Array`/
`List`/`Go.Map` などのコンテナ要素・let 注釈・ascription 先は禁止
（`a -> Scope` は OK、`Unit -> Scope` は NG）。fn 宣言・let 注釈・
extern 宣言・ラムダ引数注釈・フィールド型のすべてに効く。

### 使い方の規則（値レベルの脱出解析）

`s`（とそれを束縛した別名、それを捕まえたクロージャ）は、引数を
保持しない位置でだけ使える。判定の粒度は**値レベル**（フィールド
全体ではない）— `s` そのもの、`let x = s` の別名、自由変数として
`s` を捕まえたラムダを追う。

- 呼び出しの各引数は callee の「その引数を保持するか」判定に従う:
  - Gom の関数: その関数自身のコンパイルで強制されるので、`Scope`
    型の引数は常に非脱出。それ以外はパッケージ内の推論結果
    （fixpoint でパラメータごとの escape flag を求める）か、import
    された関数なら `.gomi` の `//gom:noescape` 注釈を読む
  - Go の関数・不明な呼び出し・コンストラクタ: 常に「逃げる」扱い
    （例外として `Scope.run` / `Scope.run_result` の本体引数は
    コンパイラが非脱出と配線する）
- 関数の本体の末尾値・`go {}` の中・`Task.spawn` 等の保持する
  引数・`Ref.set`/配列/レコード/タプル等の構築は escape する位置。
  if/match/try の腕やブロック末尾のような transparent な構造は
  文脈を伝播する
- 捕まえ方: escape するラムダがスコープ外で束縛された `Scope` 変数
  を使う（capture）とエラー。内側で自分が束縛した変数は捕獲では
  ないので、中で新しく作られた Scope の使用は許す
- 後片付け関数（`Scope.defer` など）の `f: Unit -> Unit` 引数は
  **保守的に escape 扱い** — cleanup はスコープに登録され寿命が
  伸びるため。今後必要になったら緩める

### `.gomi` 注釈

`.gomi` は属性を書けない（E0602）ので、ディレクティブコメント行
`//gom:noescape <名前> <引数番号>...` を使う。`gom info`（iface
draft）が推論済みの非脱出パラメータを書き出し、gomimport の
loader が読み取って `Symbol.Escapes` に載せる。組み込み stdlib
モジュール（List/Option/Result）も同じディレクティブを
internal/stdlib 経由で読む — スキャンと適用は internal/iface
の共有ヘルパ。注釈が無い関数はすべて「逃げる」扱い（保守的
デフォルト）。出力対象は関数型または型変数のパラメータのみ —
`Scope` 型のパラメータは型自体が非脱出を保証するため書かない。

## 実装

`internal/check/escape.go`: 型位置の検査（`checkScopeTypeParam`/
`Result`/`Free`）と使い方の解析（`checkScopeEscape`） — fixpoint で
`Symbol.Escapes` を求めてから報告 pass を回す。`internal/iface/
draft.go` が `//gom:noescape` を書き、`internal/gomimport` が読む。

## 非目標・制限

- `let` 束縛の関数値はパッケージ内では `fn` と同じ推論が効く
  （`argEscapes` が `*Var`・`*MemberExpr` callee の
  `Symbol.Escapes` を読む — `CallUnknown` であっても flag は
  引ける）。wave33 で拡張: `let f = g` の別名束縛は `g` の
  flag を `propagateAliasEscapes` で引き継ぎ、`let` メンバの
  `.gomi` 行にも `//gom:noescape` が出る（IfaceDraft と読み側
  `ApplyNoescape` はメンバ名ベースで変わらず）
- capture 判定は「ラムダ内で束縛されていない tainted 変数の使用」
  ベース — フィールド粒度ではなく変数粒度
- `gom fmt` 等で `.gomi` を再整形すると `//gom:` ディレクティブが
  失われる可能性がある（コメントは保持されない場合がある）
