# テストが遅い

- 状態: 対応中（10倍の高速化を目標に継続）
- 日付: 2026-11-05

## 状況

`make check` のフィードバックが遅い。利用者の実行例では `cmd/gom` だけで34.695秒。
同日の再計測では `go test -race -count=1 ./...` の build パッケージが74.535秒だった。
小さな変更の確認にも外部 Go ツールの起動・コンパイル・リンクが繰り返される。

## なぜ問題か

失敗を知るまでの待ち時間が大きく、小さく変更して確認する開発の反復を妨げる。
テスト実行を避ける方向で解決すると、型・評価順・コード生成・Go 相互運用の回帰を見逃す。

## 進め方と完了条件

テスト設計 → アーキテクチャ → 最後にキャッシュ、の順で検討する。
結果キャッシュを無効にした同等の検証を目安に約10倍の高速化を目指す。
一部のケースだけが速くなったことや、検証項目を別コマンドに移したことを10倍達成とは数えない。
全体を計測し、race 検査、仕様診断、意味論比較、Go 相互運用の検査内容も確認する。

## 現状分かっていること

- equality の許可判定に約18秒を費やす停止回帰があり、宣言グラフの探索へ変更して約0.01秒に短縮した。
- その変更だけでは全テストは約64秒。その後、独立native検証の最大4並列化と
  子Goツールの並列度制限などを加えた構成は48.685秒で全成功した。目標は未達。
- 型置換は変更のない枝のコピーを除いた。限定ケースは33 allocationsから0になった。
  全体に同じ倍率が出るわけではない。
- 固定44ケースと生成入力から受理された44ケースで、それぞれ別々に生成 Go をビルド・実行する。
- 単純な一括ビルドは全体実行を改善しなかったため採用していない。
- `make check` は今回触っていない sumtype 検査の5箇所のエラーで停止する。
  成功する完全チェックの比較には、この既存の検証エラーも解消する必要がある。

実施と採否は [decision](../decisions/2026-11-05-test-performance.md)、
計測方法・数値・検証結果は [sketch](../sketch/2026-11-05-test-performance.md) に残す。

## 2026-11-06: バイナリ再利用の試験

利用者の指示でキャッシュを最後の段階として試した。生成Goを安定した入力にする
`-trimpath` とGo標準の実行ファイルcacheを使うと、対象88比較は24.659秒から11.333秒になった。
ただし全体は再利用有効でも56.044秒で、明確な改善を確認できていないため既定は無効。
`GOM_TEST_BIN_CACHE=on` で試験できる。実行・Gom変換・期待値比較は毎回行う。
試作の詳細・採否・再実行方法は既存のdecision/sketchへ追記した。10倍の目標は未達。

## 2026-11-06: internal/build の原因特定と bundled runner 採用

### 遅さの構造（実測）

- テストプロセスの CPU は壁時計の 1–2% に過ぎず、遅さのほぼ全てが子プロセス
  （`go run`・`go list`・`go build`・gom）の起動待ち。cpuprofile はこの種の
  テストではほぼ空になるため、フェーズ分割のタイミング計測で分解する必要がある。
- `TestEvalMatchesCompiled` の1ケース内訳: インプロセス compile ~0.05秒
  （うち `go list -export` が ~0.047秒）、eval ほぼ0、**`go run .` ~0.33–0.98秒**。
- `go run` の内訳: main pkg コンパイル ~80ms + リンク ~150ms +
  **新規バイナリの初回実行ペナルティ ~200ms**（macOS 実測: 新規4バイナリの
  初回 exec 0.88s、同一バイナリの2回目 0.027s）+ go コマンド起動 ~50ms。
  ユニークな生成ソースでは毎回全額掛かり、worker 数を 4→8 にしても壁時計は
  変わらなかった（並列度ではなくケース当たりの子プロセス総量が制約）。
- `go build` は `go run` の実行ファイルキャッシュを温めない（action ID 系統が別）。
- `go list -export` は `import go` ごとに ~45ms のサブプロセス。`suiteOptions()`
  がケースごとに新規 `goimport.Loader` を作っていたため、パッケージキャッシュが
  効かず毎回支払っていた（TestSpec 等の直列スイートでも同様）。
- `go test` はテストバイナリの PATH 先頭に GOROOT/bin を挿入するため、
  PATH shim による `go` 起動フックはテスト内 `exec.LookPath("go")` に効かない。
  観測は一時計測コードの挿入で行った。

### 不採用・限界の確認

- 生成ソースのハッシュ重複排除: `//line` 除去後でも 44/44 全てユニークで
  同一 run 内の dedup 効果ゼロ。
- `GOM_TEST_BIN_CACHE=on`（`-trimpath` + `go run -exec cp`）: warm 再実行で
  EvalMatches 9.4→4.9s・EvalCompiledDiff 9.2→4.8s と約2倍効くが、cold では
  変化なし。入力は静的ケースと固定 PCG シードで run 間で安定しているため
  再利用は成立するが、既定有効化は引き続き保留。

### 採用と結果

- **bundled runner**（`internal/build/runner_test.go`）: 生成 `package main` を
  AST で `package cNNN`/`func Run` に書き換え、全ケースを1モジュール・1バイナリに
  リンク。`runner <name>` がケースごとの独立プロセスとなり、分離・panic クラス・
  終了コード・`//line` の契約は `go run` と同一。リンク・初回 exec が各1回に縮約。
  先に記録した「一括ビルドは改善しなかった」はケースごとの `go run` を残した
  一括化であり、1バイナリ化とは別物（不採用の記録はこの構造には当てはまらない）。
- **loader 共有**: prewarm（`sharedSuiteLoader`）または直列スイートでの
  遅延共有（`suiteOptionsWith`）で `go list` 嵐を解消。loader のキャッシュは
  mutex なし map のため、並列下では prewarm 済みパスの読み取り専用に限定。
- 数値（パッケージ単独）: EvalMatches 10.2→1.1s、TestSpec 3.4→0.56s、
  TestGolden 1.0→0.38s、TestGoldenSourceMap 1.0→0.44s、全体 38.5→約26s。
  フルスイート内では約53→約43s。
- 同日の別作業: internal/goimport の並列化 29→4.7s、cmd/gom の並列化
  36→7.4s（いずれも単独）。`stdBinds` map を書き換える
  `TestStdBindDriftHidesMember` は race 実測により直列維持。

### 残る構成要素（パッケージ約26s の内訳）

- `TestEvalCompiledDiff` ~10.5s（約41%）: seed ストリーム型のため bundled
  runner は未適用。native 側は `testbin.Build` + 2 回 exec、4 worker パイプライン済み。
- 中規模の直列テスト群 ~10s 相当: `t.Parallel()` 化は未着手の別検討。
- `TestNativeBinaryCacheInvalidation` ~2.8s: 分離検証そのもののため据え置き。

## 2026-11-06: 全体比較（origin/main 同一条件）

`git worktree` で origin/main を出して `go test -count=1 ./...` を同条件
（共有 GOCACHE・同一マシン・連続実行）で比較した。

| 計測 | origin/main | ブランチ | 倍率 |
| --- | --- | --- | --- |
| `go test -count=1 ./...` | 63.5s | 31.4s | 約2.0倍 |
| `go test -short -count=1 ./...` | — | 4.0s | フル実行比 約16倍 |

個別の重いパッケージは大きく削れた（internal/build 60.4→約13s、
goimport 29→4.7s、cmd/gom 36→7.4s、tutorial 11.2→7.3s）が、全体は
パッケージ並列実行時の CPU 競合と残存子プロセス（cmd/gom の E2E、
diff の native job、NativeBinaryCache 検証、FuzzCompile）で約31秒に
張り付く。**10倍の目標は未達**。ただし日常的な反復は `-short` ゲートで
4.0秒に落ち、旧フル実行比で約16倍の確認速度になった。

main 側の2失敗は本計測に影響しない既知のもの: `TestPanicStackResolvesToGom`
は PR で修正済みの既存バグ、`TestSkillSymlinks` は worktree 展開先が
/tmp シンボリックリンク配下であることによる環境由来の偽陽性。
