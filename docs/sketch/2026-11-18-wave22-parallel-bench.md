# 独立 dir 並列コンパイルの実測ベンチ（wave22 lane1）

docs/design.md §5-6 で入れた compileInputs の dep 層並列スケジュール
（wave21 lane3）の効果を実測する。計測系は `scripts/bench-parallel.sh`、
fixture は実行時に `mktemp -d` 下へ生成する（コミットはスクリプトのみ）。

## 条件

- 環境: Devin VM, Intel Xeon, `nproc` = 8
- fixture: 1 モジュールに 2 構成 — `indep{N}`（相互 import なし）と
  `chain{N}`（chain{i} が chain{i-1} を import）。各 dir は 60 fn +
  `import go "strings"`（goimport ローダーの仕事を持たせるため）
- 比較対象:
  - **serial**: dir ごとに `gom <cmd> <dir>` を順次実行（複数 dir 指定が
    なかった頃相当。プロセス起動 + goimport `go list` が dir ごとにかかる）
  - **multi-dir**: 1 回の `gom <cmd> $ALL` — compileInputs が dep 層で
    並列実行。GOMAXPROCS=1 / 4 / default(8) でスケーリングを見る
- `gom info` で dir ごとの `.gomi` を事前生成（E0601 回避。`gom info`
  は再帰しないため dep 順の per-dir ループ）
- `GOM_BUILD_NOCACHE=1` で増分キャッシュを無効化し、毎回フルコンパイルを
  計測

## 結果

### N=16（indep 16 + chain 16、5 回平均 ms）

| 実行形態 | gom check | gom build |
| --- | ---: | ---: |
| serial (per-dir) | 686 | 773 |
| multi-dir GOMAXPROCS=1 | 231 | 369 |
| multi-dir GOMAXPROCS=4 | 207 | 318 |
| multi-dir GOMAXPROCS=default | 208 | 300 |

### N=32（indep 32 + chain 32、3 回平均 ms）

| 実行形態 | gom check | gom build |
| --- | ---: | ---: |
| serial (per-dir) | 1538 | 1697 |
| multi-dir GOMAXPROCS=1 | 553 | 820 |
| multi-dir GOMAXPROCS=4 | 461 | 590 |
| multi-dir GOMAXPROCS=default | 433 | 560 |

## 読み取り

- **multi-dir 化の効果の大半は単一プロセス化**。GOMAXPROCS=1 でも serial
  比 check は約 2.8 倍速い（N=32, 1538→553ms）。goimport.Loader の
  プール（`go list` が dir ごと→モジュールごと）とプロセス起動・
  go.mod 探索の償却が支配的。
- **層内並列化の上乗せは 2〜3 割**。GOMAXPROCS=1→default で check は
  -22%（553→433）、build は -32%（820→560）。伸びが頭打ちになる理由は
  fixture 構造に内在する: chain は 32 層の逐次クリティカルパスなので、
  並列に回せるのは層幅（≒indep 数）まで。indep 群が chain 層の待ち時間を
  埋める形でしかスケールしない。なお層の形も固定で、indep 群はすべて
  chain1 と同じ第 0 層に入り、層間バリアの後に chain の逐次テールが
  続く構成 — 実測の並列分は実質「太い第 0 層 1 回分」。複数並列チェーンや
  中間層幅のある fixture なら別の伸び方になる（評価は fixture 依存の
  相対値として読む）。
- **build の方が並列化の効きが良い**（check -22% vs build -32%）。codegen
  + ファイル書き出し分の CPU 仕事が層内で分散できるため。

## 結論

- 複数 dir 指定が効く場面（モノレポ的に多数パッケージを一度に処理する
  ケース）で、wave21 lane3 の層並列スケジュールは GOMAXPROCS=1 相当でも
  serial を大きく上回り、並列度を上げるとさらに縮む — 仕組みは期待どおり
  機能している。
- 並列化の伸びしろは「層幅 vs チェーン深さ」で決まる。深い dep 連鎖が
  支配的な構成では層内並列の恩恵は限定的で、さらに縮めるなら層内の
  パッケージ単位より細かい粒度（パッケージ内のフェーズ並列など）が
  必要になる — 現状の課題として open-questions の本項目はここで閉じる。

open-questions 該当項: 「独立 dir 並列コンパイルの実測ベンチ」を本記録で
消化した（docs/open-questions.md から除去）。
