# テストを10倍速くするための設計と実施事項

- 状態: 採用（10倍の目標は未達）
- 日付: 2026-11-05

## 目標と順序

同等の検証を保ちながら、結果キャッシュなしの全テストを約10倍速くすることを目指す。
まずテスト設計、次にアーキテクチャを見直し、キャッシュは最後の手段とする。
問題は [テストが遅い](../problems/2026-11-05-slow-tests.md)、計測記録は
[sketch](../sketch/2026-11-05-test-performance.md) にまとめる。

## 実施: equality の許可判定

### 背景

`eqWalk` は `==` / `!=` / `assert_eq` の被演算子が関数型・rigid 型変数を
含むかを調べ、未確定 meta に equality 制約を伝える。
従来は型宣言に具体的な引数を代入し、経路上の `(宣言, 引数)` と構造比較していた。
`W a = {next: List (W (W a))}` では停止上限までの比較が支配的になった。

### 採用した変更

型の引数を先にすべて検査する規則は維持する。phantom 引数も対象となる。
その後、各ローカル型宣言の本体を一度だけ検査する。その宣言の型パラメータは
「既に検査済みの引数」として扱い、具体的な型の代入を行わない。
訪問済み集合は一回の検査内だけにあり、コンパイル結果のキャッシュは追加しない。

この判定は全引数の健全性と宣言本体の健全性の論理積なので、既に健全な引数を
代入しても新しい関数型・rigid 変数・meta は増えない。宣言本体が新たな関数型を
組み立てる場合は、代入前の本体にも Func があるため検出できる。
型パラメータの束縛は宣言本体の検査中だけに限定する。実引数の meta は宣言の
訪問済み判定より前に集めるので、同じ宣言の二つ目以降の適用でも制約が残る。

これは equality の許可判定だけの変更である。Go の comparable 判定、by-value
再帰検査、lower で実際の比較器を生成する処理は具体化した型を必要とするため変更しない。
E0906 の比較器生成上限も維持する。言語仕様の変更は意図しない。

### 検証

[計測・検討](../sketch/2026-11-05-test-performance.md)。既存の equality 仕様テストに加え、
[meta と束縛スコープ](../../internal/check/eq_walk_test.go)、
[再帰展開中に関数型を組み立てるケース](../../testdata/spec/E0409_eq_expanding_function.gom)
で検証した。gofmt・vet・通常テスト・全raceテストは成功した。

## テスト設計の試作と判断

- 不採用: 固定意味論比較44ケースを一回の `go build` に集約する方式。
  独立バイナリは保てたが、全体実行でリンク処理の競合が増え、改善を確認できなかった。
  `-p=2` の制限も試してから戻した。
- 採用: 固定ケースを独立したセッション・一時ディレクトリ・プロセスのまま最大4並列で実行する。
  ケース数、評価器との比較、期待する出力は変えない。
- 維持: `-race`、仕様テスト、生成 differential の seed 数、実 Go による実行検証。
  キャッシュ追加とデフォルト検証項目の削減は行わない。

## 付随するテスト修正

変更前から失敗していた `TestPanicStackResolvesToGom` は、コンパイルする入力と
逆引き index に同じ絶対パスを渡すよう修正した。位置復元の assertion は維持した。
[台帳](../implementation-gaps.md) に解消履歴を残した。

## 次に検討する境界

CLI のイベント整形と Go ツール起動の分離、goimport の型写像とモジュール探索の分離、
生成 differential の独立したビルド/実行の並列化。一回の処理内の Go import 問い合わせ集約も候補。
実測と回帰検証で効果を確認したものだけを採用する。

## 実施: ネイティブ検証の並列度を制御する

固定44ケースは各自の Loader と一時モジュールを持つまま最大4並列にする。
生成 differential も native の build/run だけ最大4並列にする。Go importer・型検査・
評価器は共有状態を持つためメイン goroutine で直列に保ち、結果集計は seed 順にする。
親の検証失敗時にも worker の終了を待ち、生成物を実行中に一時ディレクトリを消さない。
バイナリごとの2回実行、停止時間上限、seed 数は維持する。

外側のパッケージ並列 × ケース並列 × 各 Go 子プロセスの CPU 並列が重なるため、
`make test` は未指定時のみ `GOMAXPROCS=2`、パッケージの `-p=4` とテストの `-parallel=4` を既定にする。
明示した `GOMAXPROCS` は尊重する。コンパイラ本体や実際の利用者の Go ツール実行には
固定値を埋め込まない。この値の全体効果と限界は sketch の計測表で管理する。

## 実施: 型置換で変更のない枝を共有する

サブエージェントの調査で、`types.Subst.Apply` が置換のない Con/Tuple/Go まで
毎回全再確保していたと分かった。型の immutable 契約を使い、変化した枝だけをコピーする。
Func の canonical flatten と bound Meta の追跡は維持する。
空置換を無条件 return する方式は採用しない。
[入力不変・共有・Meta の回帰テスト](../../internal/types/subst_test.go) を追加した。
これは以前の結果を保存するキャッシュではなく、今回の処理の不要な割り当てを省く変更。

## サブエージェントが調査した実装側の次の候補

優先順位は実測が済んだ最終判断ではなく、処理量を減らせそうな候補の順である。

1. `internal/goimport/loader.go`: compile session 内の `go list -export` 問い合わせを集約。
   `-export -deps` を使い、Dir/GoBin/GOWORK/部分失敗と named identity を維持する必要がある。
2. `internal/goimport/method.go`: 求めるメソッド1個のために T と *T の全 method set を
   構築・線形走査しない。`LookupFieldOrMethod` を検討し、promoted method、pointer receiver、
   GoError、generic receiver を検証する。
3. `internal/goimport/mapping.go`: 同じ型を Gom view と Go view に二度再帰変換せず、
   一回の走査で両 view と互換性を返す。
4. `build.CompileFiles`: sibling 宣言と exports の再走査を package 共通処理にまとめる。
   単一ファイル中心の今のテストへの効果は小さい可能性がある。
5. `internal/build/build.go`: Lower 指定時の同一入力の LineIndex 二重生成を除く。

`stdlib.Apply` は既に `sync.OnceValues` を使用しており、追加キャッシュは優先しない。
Go メンバーを遅延して写像する案は、SignatureOf より先の hiddenDiag が E0501 を扱うため、
そこだけ遅延化すると診断契約を壊す。問い合わせ回数の削減を先に検討する。

## 到達点

採用構成の `GOMAXPROCS=2 go test -race -p=4 -parallel=4 -count=1 -json ./...`
は実時間48.685秒で全成功した。build パッケージ47.788秒、cmd/gom33.603秒。
10倍の目標は未達なので、[問題](../problems/2026-11-05-slow-tests.md) は対応中のまま残す。
ケース/seed の削減で達成扱いにせず、次は外部 Go ツールの実起動を必要とするテスト責務の
分離と compile session 内の import 問い合わせ集約を優先する。

## 2026-11-06: 生成結果ハッシュによるバイナリ再利用

テスト設計・実装側の改善を試した後、利用者が生成結果のハッシュによる再利用を明示的に依頼した。
キャッシュを最後に検討する方針の次段階として、まず独自キャッシュを試作し、その後Go標準の
実行ファイルキャッシュを使う最小方式へ切り替えた。

### 試験機能: Go標準の入力ハッシュで実行ファイルを再利用する

有効時、`internal/testbin.Build` は `go run -trimpath -buildvcs=false -exec cp . prog.bin` を使用する。
Go 1.27 の `go run` は実行ファイルもaction/content build IDで保存し、toolchain・flags・
GOOS/GOARCH・mainのbuild ID・依存packageのcontent IDを含めて検証する。
`-trimpath` で一時ディレクトリの違いを正規化する。

`-exec cp` は生成プログラムを起動せず、Goが用意した実行ファイルを各ケースの一時ディレクトリへ
コピーする。そのバイナリを既存のハーネスが毎回実行する。Gom変換、評価器、期待値比較、
differentialの2回実行とタイムアウトは省略しない。実行結果・成功判定のキャッシュは持たない。

保存先と無効化はGo標準の `GOCACHE` に委ねる。独自の依存解析・hash台帳・保存領域は設けない。
既定は無効。`GOM_TEST_BIN_CACHE=on` で試験的に有効化する。未設定または `off` は元の
ハーネス（比較側は `go run .`、differential側は `go build`）を維持する。
`normalize` はパス正規化だけを適用する比較用設定。`cp` が見つからない環境は
正規化した通常ビルドへフォールバックする。build/copy失敗時は即returnし、
残存バイナリを実行しない。共有キャッシュの実体は直接実行しない。

対象は固定意味論比較44ケースと生成differentialで受理された44ケースなど、native実行用の
`runCompiledCmd` / `compiledDiffInDir`。source-map E2Eなどdebug位置を検査する経路には適用しない。
`go run` はDWARFを省略するためfallbackの `go build` とdebug情報は異なるが、プログラムの
出力・panic・停止分類は維持する。コピー先が `os.Executable()` / argv[0] になるため、それを
検査するケースは別のハーネスで扱う。実際の利用者のコンパイラ動作は変更しない。

検証はGoの出力traceで再利用時にcompile/linkが起動しないこと、依存ソースの内容変更
（mtime/sizeを戻した場合も含む）、module replace、linker flags、ビルド失敗、異なる実行環境での
再実行、ケース固有のコピーの独立性、無効時のfallbackで行う。

### 試作して戻した方式

1. 依存Go/assembly/embedファイルとtoolchainを独自に読むcontent-addressed cache。
   依存の無効化と再利用は動作したが、Goの依存モデルを複製する量と入力を読む費用が大きい。
2. ソースhashで候補を保持し、各出力にコピーして `go build` のbuild ID検証を受ける方式。
   Go標準cacheのウォームアップ後の88比較は、無効11.994秒、初回11.570秒、再利用11.624秒、
   再度無効11.558秒。差が小さく、独自保存層を残す理由にならなかった。

これらの試作コードと作業ツリー内の一時キャッシュは取り除いた。
Goの仕組みはローカルtoolchainの `src/cmd/go/internal/run/run.go`、
`src/cmd/go/internal/work/buildid.go` と `exec.go` を親とサブエージェントで確認した。

### 計測と採否

2026-11-06、同じ88比較・race有効・`-count=1` で往復比較した実時間:

| 方式 | 1回目 | 2回目 |
| --- | ---: | ---: |
| 元のハーネス | 25.187秒 | 24.659秒 |
| パス正規化のみ | 13.448秒 | 13.412秒 |
| Go標準の実行ファイル再利用 | 12.067秒 | 11.333秒 |

対象の比較テストでは約2.2倍。主な効果はパス正規化でGo標準cacheが効くようになること。
追加の実行ファイル再利用はその上で約2秒の差だった。全体を再利用有効で実行すると
56.044秒で全成功したが、以前の48.685秒より短くはなく、全体への有効性は未確定。
この全体計測は無効化検証を別packageに置いていた時点の値で、その後は実Goビルドの
検証を既存buildテスト群へまとめ、外側のpackage並列で余分に競合しないようにした。

したがって**既定での有効化は保留し、試験機能として残す**。限定比較の約2.2倍を
全テストの改善率とは扱わず、10倍の目標も未達のままとする。
依存ソース変更、module replace、linker flags、異なる実行環境、コピーの独立性、
再利用時のcompile/link省略は [回帰](../../internal/build/native_cache_test.go) で確認した。
外側のGoテスト結果cacheもruntime/stdlib変更を追えるよう、buildテストにそれらの
明示的な依存を追加した。Gomコンパイラ自体を再ビルドしない `GOM_CMD` では、元から
in-processまたは一回だけbuildする経路が多く、この負荷を直接解決しない。

最終配置・既定offでは、vet・全raceテスト・通常の全テストが成功した。
全体は61.647秒（race）/61.216秒（通常）で、既定有効化を支持する計測ではない。
試験機能と再現手順を残し、全体の10倍を解消扱いにしない。

## 2026-11-06: -short ゲートと tutorial の子プロセス計測

### -short による長時間テストの除外

`go test -json -count=1 ./...` の各テストの Elapsed から 0.3 秒以上のものを抽出し、
トップレベル 66 関数（`FuzzCompile` 含む）の先頭に
`if testing.Short() { t.Skipf(...) }` を挿入した。skip メッセージには計測値を残す。
サブテスト名は `c.name` / `dir` など動的生成でソース上の位置を一意に特定できないため
関数単位の粒度とし、親が閾値を超えた場合は配下の速いサブテストもまとめて skip
される副作用を許容した。`-short` 実行で全対象が skip され、通常実行の振る舞いは
変わらないことを確認した。

### tutorial のボトルネック特定と対策

`go test -cpuprofile -memprofile` で tutorial を単独計測したところ、テストプロセスの
CPU は実時間 6.05 秒のうち 80ms（1.3%）しかなく、遅さのほぼ全てが `gom` / `go`
子プロセス起動への待ちだった（memprofile も 30MB で問題なし）。子プロセス支配の
テストでは cpuprofile がほぼ空になる点に注意が必要。個別計測では
`go run .`（約0.45秒）、`go list -deps -json`（約0.19秒）、`gom build` 起動が
ステップあたり約0.44秒を構成していた。これを踏まえ次を採用した。

- `TestTutorial` の各ステップサブテストに `t.Parallel()` を付け、子プロセス起動を重ねた。
  `TestEvalMatchesCompiled` の並列ケースと同じ構成で、各ステップは独立した一時
  ディレクトリを持ち、gom バイナリは読み取り専用で共有する。
- `trackGoInputs` の `go list -deps -json` を `-f` テンプレート（約10倍高速、
  0.018秒/回）に替えた。追跡するファイル集合（Dir 一覧・GoFiles 等・module の
  go.mod / go.sum）は変えない。
- `runStep` は各ステップの `go.mod` を読むのをやめ、`run.sh` と同じ
  `module tutorialrun/<name>` の scratch go.mod を合成する。
  これにより `tutorial/*/go.mod` はテストから要求されなくなった。
  goimport の Loader は最寄り go.mod を上方に辿るため、ステップ go.mod を
  一時退避させた状態でも全件成功することを確認した。ファイルの集約・削除は
  本変更とは別の作業として扱う。

結果、単独実行 6.0 秒→3.8 秒、全パッケージ並列下では 16.6 秒→7.5 秒。
残る直列分は gom バイナリを一度だけ build する `buildGom`（約1秒）と、
並列化後も最遅のステップの `go run`（約0.45秒）である。

併せて、実行バイナリを `GOM_TRACE_EXEC_WRAPPER` 経由で包み子プロセス時間を
観測する `internal/testbin` の足場を導入した。既定では素通しで、テスト実行の
振る舞いは変わらない。vet・gofmt・全テストは成功した。全体の10倍目標は
変わらず未達のままとする。

続けて go.mod の必須箇所を調査した。Gom パッケージ import 解決
（`gomimport.Loader` の `dirOf`/`entryPath`、遅延評価で単一ファイルには不要）、
`gomtest` のスイート dir 発見、E2E の scratch モジュール合成、モジュール解決
自体を検証する fixture は正当な要求と判断した。一方 `tutorial/*/go.mod` 11件は
`runStep` の読み取りだけが要求元で、パッケージ跨ぎも別モジュール検証もないため
冗長と判断し削除した。`tutorial/go.mod` を新設すると入れ子モジュールで
`./...` の対象外になるため置かず、リポジトリルートの1つに統一した。
README と `tutorial_test.go` の "Each chapter is a separate module" という
記述も整合させた。`testdata/` の fixture go.mod（multifile・gocall・
realworld・sumtype）は別モジュール検証そのものなので残す。
