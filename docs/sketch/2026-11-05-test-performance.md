# テスト速度の調査: テスト設計 → アーキテクチャ → キャッシュ

日付: 2026-11-05。対象: `1a6c359` を起点とする作業ツリー。
環境: Go 1.27.3、darwin/arm64。同じ端末での一回ずつの計測であり、統計的な保証ではない。

## 結論

キャッシュ追加より先に直すべき問題があった。equality の許可判定が拡大する
ジェネリック型を具体化し続け、型比較に大量の時間を使っていた。
宣言グラフを辿る方式では、CLI の停止回帰が約18.36秒から約0.01秒になった。
ただし **全テストが10倍速になったわけではない**。
生成 Go のビルド・リンクを伴うケースが多く、次はテスト責務と外部ツール境界を見直す。
テストの削除、seed 数の削減、`-race` の除去、タイムアウト短縮、独自キャッシュの追加はしていない。

## 計測方法

```sh
go test -race -count=1 -json ./... > /tmp/gom-test-baseline.json
go test ./cmd/gom -run '^TestBuildEqBudgetTerminates$' -count=1 \
  -cpuprofile=/tmp/gom-eq.cpu -memprofile=/tmp/gom-eq.mem \
  -o /tmp/gom-cli-profile.test
go tool pprof -top /tmp/gom-cli-profile.test /tmp/gom-eq.cpu
go test -race -count=1 -json ./... > /tmp/gom-test-retained.json
```

`-count=1` は Go のテスト**結果**キャッシュを無効にする。Go 標準のビルドキャッシュは
通常どおり有効で、消していない。今回の変更によるキャッシュの追加はない。
外部 Go プロセスの再実行も含む計測で、コールドビルド時間の比較ではない。
JSON の最初と最後のイベントの差はパッケージ実行区間であり、先行するテストバイナリの
コンパイル時間や `make check` 全体の実時間とは区別する。

初回の sandbox 内実行は localhost の bind と Go ビルド領域への書き込みで失敗したため、
上の比較には含めていない。以降はそれらの制限を外して実行した。
変更前の全テストでは `TestPanicStackResolvesToGom` が失敗したが、残りのケースも最後まで実行された。

## 1. テスト設計の検討

### 遅いテストの役割

変更前の `-race -count=1` の主な内訳（パッケージ間は並列なので時間は足し合わせない）:

| 対象 | 秒 | 実際の仕事 |
| --- | ---: | --- |
| cmd/gom 全体 | 36.577 | CLI の結合テストと外部 Go ツールの実起動 |
| TestBuildEqBudgetTerminates | 18.36 | 発散する型でも E0906 で止まる回帰 |
| internal/build 全体 | 74.535 | 仕様・golden・意味論比較・生成テスト・fuzz seeds |
| TestEvalMatchesCompiled | 20.81 | 固定44ケースを eval と生成 Go で比較、各ケースで go run |
| TestEvalCompiledDiff | 18.13 | 80生成入力から受理44ケースをビルドし、各バイナリを2回実行 |
| TestSpec | 20.15 | 診断契約。うち E0906 の1ケースが15.52秒 |
| internal/goimport 全体 | 16.409 | Go 型の写像・ローダー・実ビルド/実行の検証 |
| TestTutorial | 8.33 | API と CLI の生成物比較と実行。章ごとに独立モジュール |

`go test -race ./...` は子の `go run` / `go test` へ自動的に `-race` を伝播しない。
親コンパイラの race 検査と、生成物の race 検査は別である。今回この範囲は変更していない。

### 一括ビルドの prototype: 不採用

固定44ケースを一つの一時モジュール内の別々の main パッケージに配置し、
一回の `go build -o bin/ ./case000 ...` でビルドした。実行は別プロセスのままで、
ケースごとの初期化、標準出力、panic の分離と全ケースの期待値を維持した。

- 単独実行は12.94秒で成功した。ただし変更前20.81秒は全体実行中の値なので、
  この二つだけを厳密な速度比較には使わない。
- 実際の `./...` では24.29秒。親のパッケージ並列性と子のコンパイル・リンク並列性が競合する。
- 子を `go build -p=2` に制限しても全体実行中は26.14秒、build パッケージ全体は65.857秒。
- 44本のリンクは残り、呼び出し回数を減らすだけでは十分でない。**変更を戻した**。

これらは一回ずつの観測であり、原因の内訳を CPU/IO レベルまで切り分けた実験ではない。
少なくとも全体実行での改善は確認できなかったため採用しない。

### 次に直すテスト責務

1. **CLI の結果整形を実ツール起動から切り離す。**
   `cmd/gom/test.go` の `runGoTests` は起動・イベント集約・名前/位置の対応・表示を一緒に扱う。
   `build.RunGoTest` を呼ぶ関数を引数で注入できる境界を作り、イベントから Report への変換は
   記録したイベント列で検査する。グローバル関数の差し替えは避ける。
   一方で、生成ファイルを実行する成功/失敗、panic、ビルド失敗、引数転送、複数パッケージ、
   interface による出力抑止は、実ツールを通す結合テストで維持する。
2. **goimport の型写像をローダー起動と分離する。**
   型の写像・signature 分類は小さな `go/types` の値で検査できる。
   Go モジュールの探索、replace、named type identity、nil・slice・interface を含む双方向境界は
   実モジュールの結合テストに残す。全61成功テストを一律に fake に置き換えてはならない。
3. **固定意味論ケースと生成探索を分けて運用できるようにする。**
   固定ケースは既知の回帰として実行を維持する。生成 differential は80入力のうち44本を
   実ビルドし、停止・panic・非決定性まで調べるため、単に eval だけへ置き換えられない。
   開発用の短いチェックと完全検証を別入口にするなら、削った検証と実行場所を明記する。
   それは「同じ make check が10倍速い」という成果とは区別する。今回は Makefile を変更していない。

## 2. アーキテクチャの検討と採用変更

### CPU を使っていたのは codegen ではなく equality の許可判定

`TestBuildEqBudgetTerminates` の non-race CPU profile は合計2.29秒。
`internal/types.Equal` が累積2.08秒（90.83%）を占め、主な呼び出し元は
`check.eqWalk.expand` → `sameTypes` と `eqWalk.has` だった。
小さな入力でも、具体化した型引数同士の深い比較が停止上限まで繰り返される。

上限チェックを既訪問型の比較より前へ動かす小変更も試したが、race 付きで14.68秒を要した。
十分な改善ではなく、この prototype も戻した。

### equality の可否と比較器の具体化を分離する

従来の eqWalk は型引数を先にすべて検査している。従って宣言の型パラメータを
「既に検査済み」として扱えば、型宣言本体を具体化せずに関数型・rigid 変数の有無を調べられる。
各宣言を一度ずつ辿る有限グラフに変え、`W (W a)` の引数を拡大し続ける処理をなくした。

- 実引数の meta は各適用で検査し、後から関数型になった場合の拒否を維持する。
- 宣言のパラメータ束縛を局所化し、呼び出し元の rigid 変数を誤って許可しない。
- phantom 引数の検査、関数型を持つ record/ADT の拒否を維持する。
- lower の実際の比較器生成、128個の生成上限、E0906 の診断内容は変更しない。
- 永続キャッシュ・ファイルキャッシュ・コンパイル結果の再利用を追加しない。

[decision](../decisions/2026-11-05-test-performance.md)、
[実装](../../internal/check/unify.go)、[meta/束縛の回帰](../../internal/check/eq_walk_test.go)、
[関数型を組み立てる再帰宣言](../../testdata/spec/E0409_eq_expanding_function.gom)。

### 外部ツール境界の次の見直し

`goimport.Loader` は一つの compile session 内で型同一性を保つ必要があるが、
現在は importer の lookup ごとに `go list -export` を起動する。
採用済み [loading mechanics](../decisions/2026-11-03-import-go-loading-mechanics.md) にもある通り、
必要な import 群を一回の `go list -export -deps` で取得する設計が候補になる。
これは無効化規則を持つ新キャッシュではなく、一回の処理の仕事をまとめる変更である。
ただし別モジュール・GOWORK・GoBin・失敗診断・named identity を維持する検証が必要で、今回は未実装。

複数の固定ケースを単一バイナリにまとめる方法はリンク回数を減らせるが、全パッケージの
init が各実行で走ると検査対象の意味が変わる。case ごとの初期化と副作用の分離を証明せずに採用しない。

## 3. キャッシュは最後

上の責務分離と外部呼び出し削減の後でも遅い場合に検討する。
Loader をテスト全体の singleton にする、前回の生成物を使う、結果を独自キーで保存する、
という対応は今回は行っていない。module/replace/toolchain/環境/生成物の依存関係の
無効化を正しく扱う負担に対し、まず処理自体を減らす余地がある。

## 最終計測・検証

結果キャッシュなしの検証を継続中。目標は未達として扱う。

追加の実測:

- equality の改善だけ: build パッケージ64.106秒、cmd/gom27.836秒。
- 固定ケース最大4並列: build59.187秒、cmd/gom32.907秒、goimport26.870秒。
- 同じケース並列化で `GOMAXPROCS=2 -parallel=4`: build47.106秒、cmd/gom21.882秒、goimport11.933秒。
- 生成 differential の native build/run も最大4並列: 単独10.33秒。
  変更前と同じ typed40入力/33比較、mechanical40入力/11比較、差異・停止・既知バグとも0。
- 型置換の copy-on-change を追加後、通常のCPU設定で全体を実行: race コマンド65.786秒。
  一部ケースの並列化だけでは、外側のテストと子 Go ツールとの競合により全体は伸びない。

### サブエージェントによる型置換の限定ベンチマーク

16段の `List Int` に無関係な変数の置換を適用（GOMAXPROCS=1、100ms、各1回）:

| 実装 | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| 変更前 | 529.3 | 1344 | 33 |
| copy-on-change prototype | 106.9 | 0 | 0 |
| 取り込み後の通常設定で再確認 | 120.5 | 0 | 0 |

限定ケースの約5倍であり、コンパイラ全体が5倍という意味ではない。
`internal/types/subst_test.go` の `BenchmarkSubstUnchanged` で再計測できる。

### 検証上の既存問題

`make check` を実行したところ69.131秒で停止した。fmt/vet/全raceテストは通過したが、
check-sumtype が今回触っていない5箇所を報告した:

- `internal/lower/expr.go`: Atom の4種類を default 分岐で扱っている。
- `internal/format/expr.go`: parenLed の部分 type switch。
- `internal/codegen/fixture_test.go`: 二つの Expr walker で ErrorFromGo が未列挙。
- `internal/core/core_test.go`: Expr の集合検査で ErrorFromGo が未列挙。

従って69.131秒を、成功する `make check` 全体の時間としては使わない。
この時点では後続の staticcheck / experiments target まで実行されていない。

### 採用構成の最終計測

```sh
GOMAXPROCS=2 go test -race -p=4 -parallel=4 -count=1 -json ./...
```

| 対象 | 参考: 最初の計測 | 採用構成 |
| --- | ---: | ---: |
| build パッケージ | 74.535秒 | 47.788秒 |
| cmd/gom | 36.577秒 | 33.603秒 |
| goimport | 16.409秒 | 23.500秒 |
| コマンド実時間 | 未取得 | 48.685秒 |

採用構成は全成功した。80入力/44 native 比較と固定44比較を維持した。
初期計測の開始時点には sandbox 内の失敗 run が一部残っており、後で中断したため、
上の比較は厳密に隔離したA/Bではない。パッケージ間の競合も大きく、これだけで
安定した高速化率を保証しない。明らかに10倍の到達点ではない。

- `gofmt -l .`: 出力なし。
- `go vet ./...`: 成功（1.133秒）。
- 通常設定の `go test -race -count=1 -json ./...`: 全成功（実時間65.786秒）。
- `go test -count=1 ./...`: 全成功（実時間54.093秒）。
- 採用構成の全raceテスト: 全成功（実時間48.685秒）。
- `git diff --check`: 成功。
- `make check`: 前述の既存sumtypeエラーで失敗。完全な成功時間は未計測。

目標と未解決項目は [problem](../problems/2026-11-05-slow-tests.md)、
実施・不採用・次の検討順は一つの [decision](../decisions/2026-11-05-test-performance.md) に集約した。


## 2026-11-06: 生成結果が同じ場合の実行ファイル再利用

利用者の依頼で、キャッシュを最後に検討する段階へ進めた。Gom変換と実際の実行・
出力比較は毎回行い、生成Goが同じ場合のnative buildを省けるかを試した。

### 試作した3方式

1. **独自の依存content hash**: 生成Goと `go list -deps` のGo/assembly/embed、module manifest、
   toolchainと環境を読み、成功binaryだけを保存する。88件の再利用は動作したが、Goの入力モデルを
   複製する量と毎回hashする費用が大きかった。試作を取り除いた。
2. **候補binaryをコピーして `go build` で検証**: ソースhashで候補を選ぶが、必ずGoが依存を
   検証する。ウォームアップを分けた実時間は無効11.994秒、空の独自保存層11.570秒、
   再利用11.624秒、再度無効11.558秒。追加の保存層に有意な短縮を確認できず取り除いた。
3. **Go標準の実行ファイルcache**: `go run -trimpath -buildvcs=false -exec cp . prog.bin`。
   Go標準のaction/content build IDに入力検証と保存を任せる。`cp` はprogramを起動せずコピーし、
   既存ハーネスがそのコピーを直接実行する。独自のhash台帳や保存領域は不要。

Go 1.27.3 のローカル実装 `src/cmd/go/internal/run/run.go`、`work/buildid.go`、`work/exec.go`
を親とサブエージェントが確認した。`go run` は実行ファイルをcacheし、link action IDには
Goバージョン・flags・GOOS/GOARCH・依存package内容・main build IDが含まれる。

### 往復比較

テスト本体のビルドを先に済ませ、同じテストバイナリ、GOMAXPROCS=2、race有効、
-p=4、-parallel=4、-count=1で次の順に実行した。Go標準の依存cacheは消していない。

| 順 | 設定 | コマンド実時間 | build package時間 |
| --- | --- | ---: | ---: |
| 1 | 元の方式（off） | 25.187秒 | 24.916秒 |
| 2 | パス正規化のみ（normalize） | 13.448秒 | 12.872秒 |
| 3 | 実行ファイル再利用（on） | 12.067秒 | 11.466秒 |
| 4 | 実行ファイル再利用（on） | 11.333秒 | 11.058秒 |
| 5 | パス正規化のみ（normalize） | 13.412秒 | 13.133秒 |
| 6 | 元の方式（off） | 24.659秒 | 24.142秒 |

全runで固定44比較と生成側80入力/44比較を完了。生成側は typed33比較・mechanical11比較、
差異0。順序を戻しても傾向が残った。対象の約2.2倍は主に `-trimpath` が一時パスを
正規化してGo標準cacheを有効にする効果で、追加のbinary再利用はその上で約2秒の差。
単発比較なので統計的保証はなく、全テストの10倍達成を意味しない。

### 再実行方法

```sh
GOMAXPROCS=2 GOM_TEST_BIN_CACHE=off go test -race -p=4 -parallel=4 -count=1 ./internal/build -run '^(TestEvalMatchesCompiled|TestEvalCompiledDiff)$'
GOMAXPROCS=2 GOM_TEST_BIN_CACHE=normalize go test -race -p=4 -parallel=4 -count=1 ./internal/build -run '^(TestEvalMatchesCompiled|TestEvalCompiledDiff)$'
GOMAXPROCS=2 GOM_TEST_BIN_CACHE=on go test -race -p=4 -parallel=4 -count=1 ./internal/build -run '^(TestEvalMatchesCompiled|TestEvalCompiledDiff)$'
```

全体を試す場合は `GOM_TEST_BIN_CACHE=on make test`。既定は元のハーネスを維持する。
保存領域の指定や削除はGo標準の `GOCACHE` / `go clean -cache` に委ねる。
今回の独自キャッシュ試作が作業ツリー内に作った一時保存領域は除去した。

### 検証と制約

- [無効化テスト](../../internal/build/native_cache_test.go): 同一生成物の2回目でGoのtraceに
  compile/link起動がないこと、mtime/sizeを保った依存内容変更、生成Go変更、依存module追加・
  replace、linker flags、ビルド失敗、毎回の異なる実行環境、ローカルコピーの独立性、off時のfallback。
- 再利用有効で `go vet ./...` 成功、全raceテスト成功（56.044秒）、通常の全テスト成功（44.259秒）。
  この時点は検証が別package配置だったため、後でbuild群へ移動して追加のpackage競合を避けた。
- 全体は以前の48.685秒より短くなく、既定有効化は保留。実行ファイル再利用だけでは
  CLI結合テスト・Go import・ほかの実Go起動を含む全体の負荷は解消しない。
- `-trimpath` によりdebugのfile表記は変わり、`go run` はDWARFも省く。位置復元を検証する
  source-map E2Eは従来の直接build経路を維持する。
- native実行用に限定。コピー先が argv[0]/os.Executable() になる。build/copyが失敗すれば
  既存programは実行せずエラーを返す。2回実行・timeout・panic分類は維持する。

最終配置で `GOM_TEST_BIN_CACHE=off` を明示した検証:

- `go vet ./...`: 成功（0.472秒）。
- `GOMAXPROCS=2 go test -race -p=4 -parallel=4 -count=1 -json ./...`: 全成功（61.647秒）。
- `go test -count=1 ./...`: 全成功（61.216秒）。
- `TestNativeBinaryCacheInvalidation` / `TestNativeBinaryCacheFallback`: 成功。
  検証内ではon/offを明示的に切り替え、compile/link省略と無効化を確認する。
- `gofmt -l .`: 出力なし。`git diff --check`: 成功。

全体は外部Goプロセス間の競合や追加検証の影響も受け、既存の48.685秒に対する
改善を示す結果にはなっていない。従って実行ファイル再利用の既定はoffのまま残す。
`make check` の既存sumtypeエラー5件はこの作業では変更していない。
