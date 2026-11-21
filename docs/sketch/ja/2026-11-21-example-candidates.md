# まだ examples で見せていない機能のための example 案

- 日付: 2026-11-21
- 関連: [gist-cli リファクタの知見](2026-11-21-gist-cli-refactor.md)、
  [tutorial/README.md](../../../tutorial/README.md)、`skills/gom-agent-guide/references/`
- 状態: 調査記録（案。規範ではない。実装はまだ）

## 目的

`examples/` には gist-cli しかない。tutorial（01〜12 章）は言語の基礎と
Go 呼び出しを小さく見せ、`testdata/realworld/` は単一ファイルの小さな CLI
（cal, wc, csvtojson, httpfetch など）を回帰用に持つ。どちらにも「実用サイズで、
その機能を使う必然がある」例がない機能を洗い出し、それぞれに合う題材を選ぶ。

## 現状の網羅

| 機能 | tutorial | realworld | gist-cli（リファクタ後） |
| --- | --- | --- | --- |
| sum type / match / list パターン / record 更新 | ○ | ○ | ○ |
| `try` / `|?>` / Go の `(T, error)` | ○ | ○ | ○ |
| 複数ファイル 1 パッケージ | × | × | ○ |
| Gom `test` ブロック | × | × | ○ |
| `Ptr` / `Go.Map` / `Go.funcN` | × | 一部 | ○ |
| `go {}` / `Chan` / `select` / `Task` | × | × | × |
| `.gomi` による公開 API と `import "mod/path"` | × | × | × |
| 生成した API を Go から呼ぶ（spec 8.7） | × | × | × |
| 辞書 record による ad-hoc 多相、`stdlib/cmp` の合成 | 一部（11 章） | × | × |
| ラベル付き引数・ラベル付きバリアント | ○（08 章） | × | × |
| or パターン / as 束縛 / ガード | 一部（04 章） | × | × |
| `extern`（シグネチャ上書き）/ `@extern fn` | × | × | × |
| `|?>` の StepError を `errors.As` で読む | 一部（07 章） | × | × |

## 案

### A. `examples/linkcheck` — 並行処理（優先度: 高）

Markdown/テキスト中の URL を並行に HEAD して、壊れたリンクを報告する CLI。

- 見せるもの: `Task.spawn`/`Task.await` によるワーカー、`Chan` で結果を集める
  fan-in、`select` + `time.After` による全体タイムアウト、`for x in ch`、
  並行数の上限（バッファ付きチャネルをセマフォに使う）、共有の linter 警告
  （concurrency.md の Sharing rules）を踏まない書き方
- 必然性: 逐次でも書けるが遅い。全体タイムアウトは `select` でしか素直に書けない
- テスト: gist-cli と同じく `httptest` で遅い/壊れたサーバを立てる。出力順を
  入力順に揃える処理（結果に index を持たせる）も純粋関数として `test` できる
- pitfalls 予想: `go {}` の中では `try` できない（E0702）ので、ワーカーは
  `Result` を値としてチャネルに流す設計になる。これ自体を見せる価値がある

### B. `examples/semver` — `.gomi` で公開するライブラリと Go からの利用（優先度: 高）

SemVer のパース・比較・範囲（`^1.2`, `>=1.0.0 <2.0.0`）判定ライブラリ。

- 見せるもの: `semver.gomi` で公開面を絞る（内部のトークナイザは非公開）、
  公開 sum type（`Constraint`）と関数、`import "github.com/podhmo/gom/examples/semver"`
  で別の Gom パッケージ（小さな CLI `examples/semver/cmd/semver-sort`）から使う、
  さらに Go の `_test.go` から生成 API（`semver.Parse` など、先頭大文字）を直接呼ぶ
- 必然性: 「ライブラリとして配る」形は gist-cli（`package main`）では見せられない
- ad-hoc 多相: バージョンの順序を `Ord` 辞書として公開し、`stdlib/cmp` の合成で
  「prerelease を無視する順序」などを作る（C と統合してもよい）
- pitfalls 予想: Go 側から見える型（ADT の Go 表現、`List` ↔ スライスのコピー規則
  8.7 節）の説明が要る

### C. `examples/table` — 辞書による多相と or/as/ガード（優先度: 中）

CSV/TSV を読み、列ごとに型（Int/Float/String/日付）を推定して、指定列で
安定ソート・グループ集計して表を出す CLI。

- 見せるもの: `Ord a`/`Show a` 相当の辞書 record、列型ごとの辞書の切り替え、
  `stdlib/cmp` の composer での複合キーソート（`--sort a,-b`）、
  型推定の `match` で or パターン（`"" | "-" | "NA" -> Missing`）、as 束縛、ガード
- 必然性: 列の型が実行時に決まるので、比較関数を値として選ぶ必要がある
- `realworld/csvtojson` とは「データの型付け」と「多相」で差別化する

### D. `examples/kvstore` — ラベル付き引数・バリアントと状態（優先度: 中）

ファイルに追記する簡易 KV ストア（`put`/`get`/`delete`/`compact`）。

- 見せるもの: ラベル付きバリアント（`| Put (~key: String) (~value: String)`）での
  ログレコード、ラベル付き引数の API（`open ~path: p ~sync: true`）と部分適用、
  `Go.Map` をインデックスに使う、`bufio.Scanner` の Go 相互運用、
  ファイルを閉じる順序（`defer` がない点の扱い — gist-cli の `read_body` と同じ論点）
- 必然性: 引数の多い API でラベルの有無が読みやすさに直結する

### E. `examples/extern-hash` — `extern` と `@extern fn`（優先度: 低）

ファイルのハッシュ（`crypto/sha256`）を並べる小さな CLI。Go の API のうち
Gom の型対応では扱いにくいもの（`hash.Hash` の `Sum(nil)`、配列型 `[32]byte`）を
`extern` のシグネチャ上書きで扱い、速度の要るループだけ `@extern fn` で Go に書く。

- 必然性: 「Gom だけでは書けない/書きにくいとき、どこまで Go に逃がすか」の
  境界を示す。gist-cli は host を無くしたが、逃がし方の正しい見本は別に要る
- pitfalls 予想: `@extern fn` は名前・シグネチャのずれを `go build` まで検出しない

### F. `examples/rgrep` — 多段パイプラインとキャンセル伝播（優先度: 高）

A（linkcheck）実装で残った語り残しを埋める並行 grep。
walker が再帰的にパスを流し、固定 N 個の matcher（ワーカープール）が各行を
調べ、printer が出力する 3 段パイプライン。`--max-count` か Ctrl-C で
早期終了すると done チャネルを閉じ、全段へキャンセルが伝播する。

- 見せるもの: 多段パイプライン（linkcheck の fan-in collector とは別の形）、
  固定ワーカープール（linkcheck の spawn+semaphore との対比）、`Chan.close`
  の連鎖シャットダウン、`select` の送信腕（`send results m -> ...;
  _ <- done -> ...` で送り側もキャンセルを見る）、`default` 腕、
  `signal.NotifyContext` + `ctx.Done ()` による graceful stop、
  `context` 相互運用（コンパイル確認済み）
- 必然性: 大きなツリーの逐次 grep は遅い。早期終了・中断には上流を止める
  仕組みが必須 — キャンセルは linkcheck の「deadline で打ち切り」とは別の
  イディオム（能動的な伝播）
- テスト: matcher の行単位判定は純粋関数として `test` できる。結合テストは
  一時ディレクトリに深いツリーを作り、`--max-count` で出力打ち切り後に
  プロセスが素早く終わること、中断時に goroutine リーク相当（終了しない
  ワーカー）がないことを見る
- pitfalls 予想: 送信側が done を見ないと printer 終了後にワーカーが
  ブロックして漏れる（これ自体が教訓）。`os`/`fs` の `WalkDir`
  相互運用は未検証（`signal.NotifyContext`・`time.NewTicker`・
  `ctx.Done ()` はコンパイル確認済み）

## 推奨順

1. **A linkcheck**（並行処理は言語の 3 構文を占めるのに例が皆無）— 実装済み
   （[レポート](2026-11-21-linkcheck-example.md)）
2. **B semver**（`.gomi` と Go からの利用はライブラリ作者向けの最重要経路）
3. **F rgrep**（A の語り残し — キャンセル伝播と多段パイプライン）
4. C → D → E

A と B は gist-cli と同じく「ディレクトリ単位でコンパイル + Gom `test` + Go の結合テスト」の
ハーネス（`examples/gist-cli/integration_test.go`）をそのまま流用できる。
