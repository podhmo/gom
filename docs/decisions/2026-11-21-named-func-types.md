# named func 型(`context.CancelFunc` など)を `Go.FuncN` として読む

- 状態: 採用（実装・検証した — testdata/spec/ok_go_named_fn・E0507_go_named_fn_conv・E0510_go_named_fn_method、testdata/golden/go_named_fn で固定。spec.md §8.8 へ反映済み）
- 日付: 2026-11-21
- 問い: [implementation-gaps](../implementation-gaps.md)「named func 型の値を Gom から呼ぶ方法がない」

## 問題

`type CancelFunc func()` のような named func 型は `Go.FuncN` に読まれず名前的な型 `pkg.F` のままだった。その値は束縛・受け渡しできるが、`f ()` は E0401、`Go.fnN` は「`Go.FuncN` でない」E0510、`Go.conv f : Unit -> Unit` は型検査を通るのに `func(struct{}) struct{}` を emit して `go build` で落ちた（`convOK` の「unproven + named Go handle は受理」のフォールバックが func 型どうしの変換を通していた）。実害: `context.WithCancel`/`signal.NotifyContext` の stop が呼び捨てになる、`fs.WalkDirFunc` 型のコールバックに Gom fn を渡せない（rgrep 実装中に発見、[sketch](../sketch/ja/2026-11-21-rgrep-example.md)）。

## 選択肢

- **(a) FuncN として読む(採用)**: underlying が unnamed func 型で `Go.FuncN` の形に合う named 型は import mapping が `Go.FuncN` として読む。双方向が一度に通り、named パラメータ・型注釈・`Go.conv` の判定も unnamed の func 型と同じ規則に揃う — Go の assignability が `func()` ↔ `CancelFunc` の綴り違いを引き受けるので生成コードにキャストも要らない。失うのは型名とメソッド面（`http.HandlerFunc.ServeHTTP`）なので、**メソッドを持つ named func 型は対象外**とし、名前的な型のまま残す（`Go.fnN` は E0510、メソッドは呼べる、`Go.conv f : pkg.F` の named キャストは通る）。
- **(b) named のまま apply/変換を許す**: `Go.fnN` と直接適用を named func 型に許し、expected 側の互換比較を named↔`Go.FuncN` で行う。型名は残るが check の apply/conv 両側に「named の underlying が func 型」の分岐を増やす — (a) の mapping 分岐 1 箇所に比べて規則が増える。**不採用**。
- **(c) `.Call` メンバー**: named func 値に組み込みの `.Call` を見せる。`Go.fnN` と形が違う第 2 の呼び出し経路を増やすだけで、 unnamed func 型との一貫性がない。**不採用**。

## 実装

`namedType` が `assignable` な位置（シグネチャのパラメータ・戻り値のトップレベル、メンバーの宣言型、構造体フィールド）でのみ、メソッドを持たない named func 型を `Go.FuncN` に読み替える。コンテナ要素や func 型の内側は Go の型同一性で名前が載るため名前的な型のまま — `[]F` を `[]func()` と読み替えると、受理した `[]F` を生成コードが `[]func()` として渡して `go build` で落ちる（Devin Review 指摘）。`boundaryType` は `Go.FuncN` を `*types.Signature` として再構成するので、`Empty` などの interface パラメータへの代入や `Go.conv f : Go.Any` は Go と同じく受理する（同指摘の2件目）。

`internal/goimport/mapping.go` の `namedType` に 1 分岐: `NumMethods() == 0` かつ underlying が `*types.Signature` なら `mapFuncType` を試し、成功すればその `Go.FuncN` を返す。`mapFuncType` が拒否する形（variadic・4 個以上のパラメータなど）は従来どおり名前的な型に読む。`type F func() F` のように自分を名指すシグネチャは内側の出現を名前的な型に読む再帰ガード（`Loader.mapping`）を入れた。

この変更で `Go.conv stop : Unit -> Unit` のバグも解消する: `stop` が `Go.Func0` の Con になったため `convOK` の「unproven + `*types.Go` 受理」フォールバックに乗らず、func 型どうしの変換は oracle が `types.ConvertibleTo` で裁ける形になり、形の違うペアは E0507 で拒否される。

## 残る非対称

- メソッドを持つ named func 型は `Go.FuncN` に読まれない — `http.HandlerFunc` は名前的な型のままで `ServeHTTP` が呼べ、`Go.conv f : http.HandlerFunc` で `Go.FuncN` 値からキャストできる。`Go.funcN` の want が名前的な型のときは FuncN として seed しないので、inline lambda は `fn (x: T)` の注釈が要る。
- `f ()` の直接適用は unnamed の `Go.FuncN` と同じく不可 — `Go.fnN` を通す。
