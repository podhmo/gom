# examples/linkcheck の実装と説明構成の評価

- 日付: 2026-11-21
- 対象: 新規 `examples/linkcheck/`（`.gom` 6 ファイル 465 行、`integration_test.go`、`README.md`、`build.sh`）
- 関連: [example 候補の調査（案 A）](2026-11-21-example-candidates.md)、
  [gist-cli リファクタの知見](2026-11-21-gist-cli-refactor.md)、
  [skills/gom-agent-guide/references/concurrency.md](../../../skills/gom-agent-guide/references/concurrency.md)、[docs/implementation-gaps.md](../../implementation-gaps.md)
- 状態: 調査記録。規範ではない

## 何を説明する example にするか

候補ドキュメント案 A の要求は「`go {}` / `Chan` / `select` / `Task` と、
共有 linter 警告を踏まない書き方を、並行処理に必然のある題材で見せる」。
URL の並行 HEAD チェックは「逐次では遅く、全体タイムアウトは `select`
でしか素直に書けない」ので題材の必然性はそのまま採用した。ここから、
読者に何を教える例にするかを次の 3 層に整理して構成した。

1. **3 つの並行構文の使い分け** — `Task` は「結果が 1 つで宛先も 1 つ」
   （ファイル読み込みの fan-out）、`go {}` + `Chan` は「結果を 1 か所に
   集める fan-in」（URL プローブ）、`select` は「複数ソースの競合、
   特にデッドライン」（全体タイムアウト）。同じプログラムの中で役割が
   分かれているところを見せる。
2. **言語の制約が形作る設計** — 制約を回避するのでなく、制約から
   生まれた形として読めるコードにする:
   - `go {}` の中では `try` できない（E0702）→ ワーカーは `Outcome`
     という完了値をチャネルに流す。`try` は普通の `fn`（`head`）の中に閉じる
   - `select` の腕式は入るたびに評価される → `time.After` はループの外で
     1 度だけ作る（内側で呼ぶと受信ごとに締切が延びる）
   - `for x in ch` は break できない → デッドラインで抜ける受信は
     `select` の再帰ループ（`collect`）
   - 共有ルール（W0701/W0702）→ mutable なものはゴルーチン境界を越えない。
     `Ptr http.Client` を共有せず `head` の呼び出しごとに作る、チャネルには
     不変値（`Target`/`Outcome`/`Unit`）だけ流す、という実例で示す
3. **非決定性を純粋関数で回復する** — fan-in の到着順は決まらない。
   `Outcome` に入力順の `index` を持たせ、並行性を知らない純粋関数
   `ordered_outcomes` が入力順に並べ直し、届かなかった分を `Timeout`
   で埋める。これこそが `test` ブロックで釘付けにする部分。

ファイル分割は gist-cli と同じ「操作の木」で、`parse → read → extract →
check → report` の各枝が 1 ファイル。純粋な枝（`args`/`extract`/順序復元/
`classify`/`report`）には `test` ブロックを添え、副作用は `files`・
`check`・`main` に隔離した。

## 実装の概要

| ファイル | 責務 | 中の並行要素 |
| --- | --- | --- |
| `main.gom` | `main`/`run` の幹と終了コード | — |
| `args.gom` | `Command`/`Options`、引数文法、usage | — |
| `files.gom` | UTF-8 読み込み | ファイルごとに `Task.spawn`、入力順に `Task.await` |
| `extract.gom` | URL 抽出・重複除去・`Target` 化 | — |
| `check.gom` | `Verdict`/`Outcome`、HEAD プローブ、パイプライン | `go`/`Chan`/`select`/`time.After` |
| `report.gom` | 行の描画・集計・終了コード | — |

チェック部の形（`check.gom` 冒頭の図そのまま）:

```text
feeder ──jobs──▶ launcher ──per target──▶ go { check_one } ──▶ results ──▶ collect
                      ▲                                          ▲
                 slots (semaphore:                        deadline
                 at most concurrency                   (time.After)
                 in flight at once)
```

launcher が spawn 前にセマフォを取るので、生きているゴルーチン数も
`concurrency` で頭打ちになる。`results` は `n` 個バッファしてあるので、
collect がデッドラインで抜けたあともワーカーは送り先に困らず終われる。

CLI 契約: `linkcheck [--concurrency N] [--timeout D] [--all] [--] FILE...`。
失敗行のみ stdout（`--all` で全行）、集計は stderr、壊れ/未到達/timeout が
1 件でもあれば終了コード 1。

## 案 A のチェックリスト評価

| 見せるもの（案 A より） | 判定 | 実際の箇所 |
| --- | --- | --- |
| `Task.spawn`/`Task.await` によるワーカー | △ | `Task` は `files.gom` の並行読み込みで実演。URL チェックのワーカーは `go` ブロック — 結果は `results` チャネルに流すので「1 結果 1 宛先」の `Task` が合わず、セマフォがゴルーチン数も束ねる。計画文とのずれ（後述） |
| `Chan` で結果を集める fan-in | ○ | `results: Chan Outcome`（`check.gom`） |
| `select` + `time.After` による全体タイムアウト | ○ | `collect`。タイマーはループ外で 1 度だけ生成 |
| `for x in ch` | ○ | launcher の `for job in jobs_in` |
| 並行数の上限（バッファ付きチャネルをセマフォに） | ○ | `slots: Chan Unit = Chan.make concurrency` |
| 共有 linter 警告（Sharing rules）を踏まない | ○ | `gom check` で警告 0。W0702 回避の実例として `head` が呼び出しごとに `Ptr.new` する形 |
| pitfall: `go` 内で `try` 不可 → `Result` を値として流す | ○ | ワーカーは完了した `Outcome` を送る設計そのもの |

テスト面の要求 — 「`httptest` で遅い/壊れたサーバ」「出力順を入力順に
揃える処理を純粋関数として `test`」— も満たした。`ordered_outcomes`
は `test` ブロックで順序復元と `Timeout` 補完の両方を検証している。

## 検証

- `gom check -format text examples/linkcheck` — エラー 0、警告 0
- Gom の `test` ブロック 12 個（args 4 / extract 2 / check 4 / report 2）
- `go test ./examples/linkcheck` — 生成モジュール経由で上記 test ブロックを
  実行しつつ、実プロセスの CLI を `httptest` 相手に検証: 入力順の出力と
  refs 併記・重複 URL の 1 回化・全 ok で無出力 exit 0・HEAD メソッド・
  接続失敗・`--timeout 1ms` で全件 `timed out`・`--concurrency 2` で
  同時 in-flight が 2 以下・引数エラー 7 件
- `gofmt -l .` / `go vet ./...` / `scripts/check-sumtype.sh` / `go test ./...` —
  すべてパス

## 発見した言語のギャップ

`Array.make` は「n 個のゼロ値」と spec にあるが、要素が `Option` などの
ADT だと Go の `nil` が入り、`Array.get` が "not a declared variant" で
panic する。`ordered_outcomes` の初版で `--timeout 1ms` 経路が実際に
panic した（`Array (Option Outcome)` → `Go.Map Int Outcome` に置き換えて
回避）。`docs/implementation-gaps.md` に登録済み。

## 不足・限界

計画文とのずれと、CLI としての限界。

- **ワーカーが `Task` ではない**: 案 A の「`Task.spawn`/`Task.await` による
  ワーカー」の字面ではチェックのワーカーも `Task` の想定だったと読める。
  実装では「`Task` = 結果の宛先が 1 つのとき」の対比として `files.gom` に
  残し、fan-in 側は `go` + `Chan` にした。例としては構文の使い分けが
  見える分よいが、案文どおりではない点は残す。
- **HEAD のみ**: HEAD を正しく扱わないサーバ（405、HEAD で異なる応答）で
  誤判定し得る。GET へのフォールバックや `--method` はない。
- **抽出は正規表現**: Markdown AST ではなく `https?://…` の素直な正規表現。
  コードブロック内やコメント内の URL も拾う。行番号は持たず refs は
  ファイル名のみ。
- **リクエストごとの 10 秒は固定**: `--timeout` は全体締切で、単発の
  request timeout は変えられない。
- **全体タイムアウトはベストエフォート**: 締切後は残りを `Timeout` と
  報告して終わるが、飛行中のリクエスト自体はプロセス終了まで走り続ける
  （結果は捨てられる）。
- **リダイレクトは Go 既定で追跡**: 3xx を壊れ扱いしないのは意図どおりだが、
  追跡しないモードはない。

## 今後

- `Array` の ADT 要素問題は implementation-gaps に乗せた。spec 側で初期値
  引数にするか、ランタイムで既定バリアントを生成するかは別途の決定。
- refs に行番号を載せる、`--get-fallback`、抽出の厳密化は、例としての
  説明密度を損なわない範囲なら拡張余地。
