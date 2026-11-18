# cons-recursion 特殊化の検証記録 (wave19 lane 1)

- 日付: 2026-11-18
- 対象: internal/lower/consrec.go (markConsCalls) — decision: [2026-11-18-cons-recursion-specialization](../decisions/2026-11-18-cons-recursion-specialization.md)

## 確認したケース

手動の .gom コンパイル + 実行 (生成された Go を `go run` / `go build` で実走):

| ケース | 結果 |
| --- | --- |
| `mymap` (`[f h, ..mymap f t]`) | buffer loop 化、100,000 要素で 0.167s 完走（再帰版はスタック超過） |
| `myfilter` (cons 腕と素 TailCall の混在) | 同一ループ内で動作、結果一致 |
| `myappend` | buffer loop 化、結果一致 |
| 複数 head `[h, h, ..f t]` | append が順序通り |
| `[] -> []` 基底 | `buf = append(buf, tail...)` で反復終了 |
| 多相 `pmap [a,b]` (恒等インスタンシエーション) | 変換され動作 |
| `@tailrec` 付き cons 再帰 | 受理される（R1/R2 を TailCall として満たす）— spec-visible、ok_tailrec_cons.gom で固定 |
| `rest` の 2 度読み (`[List.length rest + h, ..rest]`) | 変換されず再帰のまま（golden の `keep`） |
| head が Go package 変数 (`os.Stdout`) | 変換されず再帰のまま（golden の `keepvar`、stableHead） |
| stdlib `concat_go` (`append xs (concat rest)` 形 — 呼び出しが cons 引数の内側) | 形状外のため未変換（lane 3 の対象） |

## 固定しているテスト

- `testdata/golden/cons_loop.gom` / `.golden.go` — 変換・混在・複数 head・rest 2 度読み・不安定 head の 5 形
- `testdata/spec/ok_tailrec_cons.gom` — `@tailrec` 受理

## ゲート

`gofmt -l .` 空、`go vet ./...`、`go test ./...`、`sh scripts/check-sumtype.sh` すべて通過。

## Devin Review との往復

- round 1: 「mutable head の読み出し時点がずれる」指摘 — Go field は既に `let` snapshot 済みで無関係だが、Go package 変数 (`Var{Sym.IsGo}`) の読み出しは atom のまま残るため本当にずれていた → `stableHead` で拒否（コミット 619053d）
