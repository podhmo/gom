# 8 要素超 tuple の匿名 struct emit — 検証メモ

- 日付: 2026-11-24
- 関連: [decision](../decisions/2026-11-24-wide-tuple-anon-struct.md)

## 手順

1. `testdata/golden/tuple9.gom` — 9 要素の spelled tuple を
   引数パターン・リテラル構築・分解に使い、golden で匿名 struct
   emit（`struct{ V0 int64; ...; V8 int64 }`）を確認。
2. E2E: 同じソースを `gom build` し、生成ファイルを
   `replace github.com/podhmo/gom => ローカル` な scratch module
   に置いて `go build` + 実行 — `big (1,...,9) = 10`、`y = 10`
   を確認（`undefined: rt.Tuple9` だった箇所が通る）。
3. `types.GoRepr` は reflect の綴り `struct { V0 int64; ... }`
   を返す — `Go.try_as` の inhabits 判定が匿名 struct の
   host 値に一致する。eval の `unboxAny` も V フィールドを
   reflect で展開して `VTuple` にする（`rt.TupleN` 側にも効く）。
4. 双方向 interop: `testdata/gocall/tuple9` — Go test が匿名
   struct literal で `Big`/`Nine`/`Echo` を呼び（Go→Gom）、
   `Echo` は生成コード内で `goiface.Identity` の T に匿名
   struct を束縛する（Gom→Go）。
5. `genVersion` を 1→2 に bump（emit 規則の変更、
   [build-cache decision](../decisions/2026-11-18-incremental-build-cache.md)）。

## 結果

通る。8 以下は `rt.TupleN` のまま（named 型なので 8 と 9 の
混在比較は起きない）。
