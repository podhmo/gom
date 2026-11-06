# gomtest: pinning the fatal-path failures

日付: 2026-11-08

## 背景

`internal/gomtest` の golden ハーネスは、失敗を `t.Fatalf` と
`t.Errorf` で使い分けている:

- `Fatalf` — harness/generator の契約違反で比較自体が成立しない
  もの（`.gom` 無し、0 出力、flat の出力数超過、`.go` 以外の
  出力、dir 内に置かれない出力、golden への重複マッピング、
  manifest/読み書きエラー）。`Goexit` でその case を打ち切るの
  が正しい。
- `Errorf` — 比較は成立するが期待と合わないもの（golden の内容
  不一致、dir 内の stray golden、stale な flat
  `x_test.golden.go`）。case を進めて他の報告も出す方がよい。

どちらも subtest の失敗として集計されるが、case 内の
`Fatalf`/`Errorf` は親テストからメッセージを観測できない
（`t.Run` の戻り値は bool）。このため stray/duplicate/出力数の
経路は長らく未検証だった。

## 採った方法

helper process 方式（`os.Args[0]` を `-test.run` で再実行する、
標準 library の定石）:

- `TestGoldenFatalHelperProcess` は環境変数
  `GOMTEST_GOLDEN_HELPER` で scenario を選び、`RunGoldenTests`
  を TempDir の suite に対して普通に呼ぶ — 本物の `*testing.T`
  が得られるため失敗は自然に subtest 失敗→ exit 1 になる。
- `TestGoldenFatalPaths` は scenario ごとに子プロセスを起動し、
  exit code 1（test 失敗であり panic/不測の終了ではない）と
  出力中の期待メッセージを assert する。

failure injection（`t` を偽装して `Fatalf` を捕捉する構造）も
考えたが、`RunGoldenTests` のシグネチャは `*testing.T` 具体型で
あり、抽象化を入れると harness 本体の設計がテスト都合で歪む。
subprocess は本番経路をそのまま通せるため採用。

## 覆った経路（scenario → 期待メッセージ）

- `stray-dir-golden` — dir case 内の stray golden →
  `has no generated output`（Errorf 経路）
- `stale-flat-test-golden` — `tests:true` の flat ケースで
  `x_test.golden.go` だけ残った stale → 同上（Errorf 経路）
- `dup-golden` — 2 出力が同一 golden に写像 →
  `both map to golden`（Fatalf）
- `flat-too-many-outputs` — tests 無し flat で 2 出力 →
  `generated 2 files, want at most 1`（Fatalf）
- `flat-test-output-without-tests` — manifest 無しで
  `x_test.go` を生成 → `does not set "tests": true`（Fatalf。
  2 出力だと先に出力数チェックに掛かるので lone output で起こす）

既存の Fatalf/Errorf の切り分け自体は妥当で、Errorf 化が誠実な
箇所はなかった — stray 系は既に Errorf なので変換は行わず、
検証で pin した。
