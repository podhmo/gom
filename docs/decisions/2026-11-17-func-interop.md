# Gom の関数値を Go の func 型として渡す(typed adapter `Go.funcN`)

- 状態: draft
- 日付: 2026-11-17
- 問い: [open-questions](../open-questions.md)「Gom の関数値を Go の func 型として渡す」

## 問題

`http.HandleFunc` の `func(ResponseWriter, *Request)`、`http.Client.CheckRedirect` の `func(*http.Request, []*http.Request) error`、`sort.Slice` の `func(i, j int) bool` のように、Go の API は func 型を引数・フィールドに持つ。現在 mapType は `*types.Signature` を「func-typed parameters and results are not supported yet」として隠す(E0501)ため、これらのシンボルは丸ごと見えない。Gom の関数値の値表現はカリー化された `func(A) func(B) R` であり、Go の `func(A, B) R` と一致しないので、そのまま渡せない。

## 選択肢

- **(a) codegen bridge**: `Int -> Int -> Bool` を `func(int,int) bool` としてそのまま admissible にし、emit 側で自動 wrap する。型表示(`A -> B -> R`)と実表現(`func(A,B) R`)が一致せず、どの変換が暗黙に起きたか読めない。`*T` や `[]T` の要素変換を型から読み戻す判断を codegen に持ち込むことになり、「codegen に言語の意味を判断させない」規則に反する。**不採用**。
- **(c) extern-only**: コールバックが要る API はすべてホスト側(Go)でラップし、Gom からは見えない。`http.Client` 構築(decision 2026-11-17 (c))と組み合わせると結局すべてのライブラリ利用がホスト adapter 必須になり、Gom だけで stdlib を試す価値が失われる。**不採用**(ただし構造の最終形としては (c) 系の factory 供給と併用する — 下の gist-cli)。
- **(b) typed adapter(採用)**: Go の func 型を `Go.FuncN`(N=0..3)という Go 名前空間の型として surface し、`Go.funcN` という明示変換だけが Gom fn 値をその型へ変換する。変換は 1 箇所に明示され、型は `func(A,B) R` の実表現を正確に名指す。

## `Go.FuncN` の形

`Go.FuncN` の型引数は **Go ビュー**(生の Go 型)で、パラメータ N 個 + 結果 0..2 個:

```
func(A, B) R          ->  Go.Func2 A' B' R'
func(A)               ->  Go.Func1 A'
func(A, B) error      ->  Go.Func2 A' B' GoError
func(A) (T, error)    ->  Go.Func1 A' T' GoError
```

`A'` は生の Go ビュー — `int` は `Go.Int`、`[]*http.Request` は `Go.Slice (Ptr http.Request)`。Gom ビュー(`Int`/`List`)ではない。`Go.FuncN` の値表現は Go の func 値そのものであり、生成コードで `func(...) R` として render される。引数の可変長なので `Go.Func2 A B`、`Go.Func2 A B R`、`Go.Func2 A B T GoError` がすべて合法。

制約(隠し理由になりうるもの):

- パラメータ数 ≤ 3(survey で実在 API のほぼ全域を覆う;`filepath.WalkFunc` の 3 が最大の実用例)
- 結果は 0・1 個、または `(T, error)` 2 個のみ。3 個以上の結果・`(T, U, error)` は隠す
- 2 番目以降の結果が `error` でない多結果、variadic な func 型(`func(...T)`)は隠す
- それを超える func 型を持つシンボルは従来どおり hidden(E0501)

## `Go.funcN` の形

`Go.funcN f` — `f : A1 -> ... -> AN -> R` の **Gom ビュー** の関数値を `Go.FuncN` に変換する明示変換。返り値の変換規則は呼び出し境界の写像を逆向きに使う:

- `R = Unit` → 結果なし
- `R = Result Unit GoError` → `error`(err-only)
- `R = Result T GoError` → `(T', error)`(pair)
- その他の `R` → 単一結果 `R'`

パラメータ・結果の各要素は boundary の変換表(goConvKind)で橋渡しする:`Go.Int ↔ Int`(数値変換)、`Go.Slice T ↔ List T`(コピー)、`any` は box。Gom 側だけの型(`Option`、ユーザー ADT、タプル、カリー化された `Func`)を含む引数は変換できないので `Go.funcN` 適用時に E0510 で拒否する。

生成コードは:

```go
func(p0 T0', p1 T1') (R0', R1') {
    r := f(conv0(p0))(conv1(p1))   // conv_i は raw → Gom ビュー
    // errOnly: _, err := r.Unwrap(); return err
    // pair:    v, err := r.Unwrap(); if err != nil { var z T0'; return z, err } ; return conv(v), nil
    // plain:   return conv(r)
    // none:    f(...); return
}
```

変換の選択(どの ConvKind か)は lower が Core IR のノード `GoFunc` に確定して載せる — codegen はレンダリングだけをする。

## conformance point

関数値の変換は `Go.funcN` の適用点だけにある — 暗黙の変換は入れない。`f : Func` を `Go.FuncN` のパラメータに直接渡すと型不一致(E0401)— ユーザーが必ず `Go.funcN` を書く。

期待する `Go.FuncN`(want)との比較は **produced-vs-want の raw-view 互換**(`gomRawCompatible`)で行う:`Go.funcN` の引数が Gom ビューで型付けられるため、produced FuncN の型引数は Gom 側の写像を通る。一方 want の型引数は member の raw 写像を通るので、`int`/`int64` のように両ビューが `Int`/`Int64`/`Go.Int` の 2 者で重なる境界型は表記がずれる — `func(int)` は `x: Int` と `x: Go.Int` のコールバックを等しく受け入れる。この比較は goimport の `viewsCompatible` を写した view-tolerant な一致(int/int64 対、`List T`↔`Go.Slice T` の要素毎、同 constructor の引数毎再帰)であり、`types.Equal` の厳密一致ではない。不一致は E0401 で「`Go.funcN` produces `X`, expected `Y`」と報告する。

## inline lambda の seed

期待する `Go.FuncN` があるとき、raw ビューを Gom 側へ訳した seed 型が inline lambda の unannotated パラメータを具体化する(`x` が `Go.Any` に束縛される等)。seed は **引数が構文上 lambda のときだけ**作る — 名前付き関数は自分の型を既に持ち、seed への合致でも同じ不一致を二度報告してしまう。

## `Go.func0`

`func() R` の 0-ary は Gom の `Unit -> R` サンクに対応する — `fn () -> 41` が `Unit -> Int` に型付き、Unit パラメータが Go シグネチャから落ち、生成側の呼び出しは `f(struct{}{})` を渡す。

## callback の双方向性

`Go.funcN` が作った値は Go の func 値であり、Gom 側に「Go func 値を Gom の `Func` に戻す」逆変換は入れない(Go から再取得した func 値は `Go.FuncN` 型のまま → そのまま呼び出せないのか?→ `Go.FuncN` 値の適用は将来の問い;現状は Gom 側から apply 不可とする)。双方向テストは「import 面で FuncN が見える(Go→Gom)」と「`Go.funcN` で供給できる(Gom→Go)」を指す。

## ラベル

`Go.funcN` の入力はラベルなしパラメータのみ(Go の func 型にラベルはない)。ラベル付き fn を渡すと E0510。

## 影響

- spec 8.x に `Go.FuncN` / `Go.funcN` を追記
- `mapping.go` の `*types.Signature` が FuncN を返す(func-typed field `http.Client.CheckRedirect` も読めるようになる — 読み取りのみ。Go 値への代入面は別)
- gist-cli `host.NewClient` を callback 受け取りの factory に変え、`main.gom` から `Go.func2 (fn req via -> Err http.ErrUseLastResponse)` を渡す(30*time.Second は lane 4 待ち)

## 検討した実在 API(survey)

`net/http`: `HandleFunc`(2), `Client.CheckRedirect` field(2), `Server.Handler` は interface。`sort`: `Slice`/`SliceIsSorted`/`SliceStable`(2)。`path/filepath`: `Walk`/`WalkDir` callback(3)。`strings`: `Map`(1)、`FieldsFunc`(1)、`ReplaceAllFunc` 系(1)。`regexp`: `ReplaceAllFunc`/`ReplaceAllBytesFunc`(1)。`time`: `AfterFunc`(0)。`context`: `AfterFunc`(0)。`sync.Map`: `Range` callback(2)。`flag`: `Func`(第3引数が `func(string) error` → 1)。`os/signal`: `NotifyContext` 系は func ではない。実用は 0..3 arity、結果は 0/1/`error`/`(T,error)` でほぼ全域 → arity 上限 3、結果 ≤2 を採用。
