# flat fn params（呼出側 uncurry）— 計測と検証メモ

wave20 lane 2。`docs/decisions/2026-11-18-flat-fn-params.md` の検証記録。

## 価値判定（先に計測）

n=100000、`stdlib/list` のベンチで curried 呼出の実害を pin した:

| | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| `BenchmarkFold`（curried `f(acc)(x)`） | 1368870 | 1600004 | 100000 |
| `BenchmarkSum`（同等の手続きループ） | 117420 | — | 0 |

要素ごとに 1 クロージャ確保 → 約 12 倍。価値ありと判定し (b) 呼出側 uncurry を採択（(a) flat 二面公開は「値が 2 つの表現を持つ」分岐を全位置に撒くため見送り。パラメータ位置だけ規約を変える (b) の方が影響範囲が狭い）。

## 実装形

宣言 FuncDecl の >=2 本矢印パラメータだけ `func(T1',...,Tk') R'` にし、canonical 値との変換は `Uncurry` / `curryEta` アダプタで行う。`Fold` の呼出は生成コード上 `f(acc, v)` の直接呼出になり、要素ごとの確保が消える（`stdlib/list/list.go` 再生成で `tmp26 := f(acc, tmp24)` を確認）。

## 検証

- `gofmt -l .` 空、`go vet ./...` クリーン、`go test ./...` 全緑
- `stdlib/list` の再生成コードで `Fold` が `func(B,A) B` の flat 呼出を pin
- `testdata/golden/go_slice_ops.golden.go` が flat 化で変化（他ゴールデンは不変 — 非 flat 経路の回帰なし）
- `TestEagerFnChunkTiming` は residual 適用形に更新（decision の「セマンティクス上の差」節参照）
- ベンチ再計測（flat 後、n=100000・benchtime=1x）: `BenchmarkFold` 270135 ns/op・**0 B/op・0 allocs**（curried 時は 1368870 ns・1600004 B・100000 allocs）→ 約 5 倍高速化・確保完全除去。`BenchmarkSum`（127186 ns）との差は残りの List 走査コスト
