# `any` 引数位置の Go 型の値は `any(x)` に box せず素通しする

- 状態: adopted
- 日付: 2026-11-24
- 検証: [docs/sketch/2026-11-24-goany-arg-passthrough.md](../sketch/2026-11-24-goany-arg-passthrough.md)

## 決定

Go 呼び出しの `...any`（および `any`）パラメータへ、既に Go 型を
綴る引数（`GoError`、`Go` の named 型、`Go.Slice`/`Go.FuncN` などの
`Go.*` コンストラクタ、`Ptr`、チャネル、gom の関数型）を渡すときは
`any(x)` の box を生成せず、引数をそのまま emit する。

## 理由

`fmt.Errorf("ctx: %w", e)` の `e` は check が `Go.Any` 宛として
`Ascribe{Conv: true}` で包み、lower が `ConvBoxAny` — `any(e)` の
一時変数 — に落としていた。Go の実行セマンティクス上 `any(e)` は
`e` と同じ値だが、**静的型**が `error` から `any` に消える。
`go vet` の printf 解析は `%w` の引数が静的に `error` であることを
要求するため、`tmp := any(e)` を見て
`fmt.Errorf format %w has arg tmp1 of wrong type any` を報告した。

Go 型の値を `any` スロットへ渡すとき box は代入可能性上の no-op
なので、生成しない — これが `%w` error wrapping の第一級サポートの
実装（selfish 移植の `@extern` shim 退避の解消）。

## 範囲

- **呼び出し引数位置だけ**。`let x : Go.Any = e` など declared
  `Go.Any` 位置では従来どおり box を生成する — box が宣言型 `any`
  を emitted コードに残す役割を持つ（`Go.try_as` の後続 assertion
  が interface 型を必要とする）。
- Go 型を綴らない値（`Int`、`String`、record、sum、tuple、数値
  リテラルの `any(int64(42))` materialize）は従来どおり box。
- 結果側（`-> Go.Any` の本体、`Result Go.Any` の payload）も
  従来どおり box — `go vet` が読むのは呼び出し引数だけ。
