# experiments/w23/noncons-accum

wave23 lane4 の実測 fixture。lower の cons-recursion 特殊化
（decision `2026-11-18-cons-recursion-specialization`）が**救えない**
蓄積形がどのくらいの大きさで quadratic になるかを測る。

## 構成

- `noncons.gom` — 5 つの蓄積形（被覆形 1 + 除外・対象外形 4）
- `noncons.gomi` — `gom info -o -` で生成したインターフェース
- `noncons.go` — `gom build` の実出力。`ConsMap` だけ buffer loop、
  他 4 形は素の再帰 + cons コピーのまま残る（コミット済み生成物）
- `bench_test.go` — `go test -bench .` で各形の ns/op・B/op・allocs
  を計測

## 結果（この VM、Intel Xeon、-benchtime 30x）

| 形 | n=500 | n=2000 | n=8000 | 増分（4×） |
| --- | --- | --- | --- | --- |
| ConsMap（被覆形・対照） | 6.8 µs | 23 µs | 78 µs | ~3.4× — 線形、10→18 allocs |
| ChainAppend（総要素 1k/4k/16k） | 345 µs | 1.78 ms | 26.6 ms | ~15× — O(k²·chunk) |
| FoldConcat | 309 µs | 3.30 ms | 48.7 ms | ~10.7×/14.7× — O(n²) |
| Mutual | 317 µs | 4.29 ms | 65.2 ms | ~13.5×/15.2× — O(n²) |
| ReadTwice | 330 µs | 4.41 ms | 70.7 ms | ~13.4×/16× — O(n²) |

n=8000 で対照形の ~600–900 倍。除外 3 形はほぼ同一プロファイル
（同じ機構: 成長する acc のコピー）。

判定材料・候補は `docs/sketch/2026-11-19-wave23-noncons-accum.md`。
