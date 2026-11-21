# examples/rgrep の実装と説明構成の評価

- 日付: 2026-11-21
- 対象: 新規 `examples/rgrep/`（`.gom` 5 ファイル 507 行、`integration_test.go`、`README.md`、`build.sh`）
- 関連: [example 候補の調査（案 F）](2026-11-21-example-candidates.md)、
  [linkcheck のレポート](2026-11-21-linkcheck-example.md)、
  [skills/gom-agent-guide/references/concurrency.md](../../../skills/gom-agent-guide/references/concurrency.md)、[docs/implementation-gaps.md](../../implementation-gaps.md)
- 状態: 調査記録。規範ではない

## 何を説明する example にするか

案 F の要求は「linkcheck が見せられなかった並行イディオム — キャンセル伝播、
多段パイプライン、固定ワーカープール、`select` の送信腕/`default` 腕、
`signal.NotifyContext` — を1本の CLI で見せる」。再帰 grep は「大きな
ツリーの逐次走査は遅く、早期終了（`--max-count`・Ctrl-C）には上流を
止める仕組みが必須」なので題材の必然性もそのまま採用した。ここから
読者に教えることを 3 層に整理して構成した。

1. **キャンセルは「送り側だけ」が見ればよい** — この設計の核心。
   `done` チャネルを閉じて全段に伝播させるのだが、受信側は `done` を
   見る必要がない。受信は上流の `close` で終わるからである（`for x in
   ch` が自然に抜ける）。キャンセルが刺さるのは「チャネルが満杯で
   ブロックしている送信」だけ — だから全ての *送信* を
   `select { send out v -> true; _ <- done -> false }` で守る
   （`send_path`/`send_item`）。案 F の pitfalls にあった「送信側が
   done を見ないと printer 終了後にワーカーが漏れる」を、コード上の
   不変条件として表現した。
2. **`close` の連鎖とクローズ権限** — シャットダウンは
   `paths`(walker が閉じる) → `fin`(matcher 各自が抜けるとき send) →
   `results`(`wait_workers` が最後の `fin` で閉じる) の順に畳まれる。
   `results` を早く閉じると生きている matcher が closed channel に
   send して panic する — countdown がその防止策。一方 `done` は
   「閉じるのは main goroutine 上の `cancel` だけ」という単一クローザ
   不変条件で安全にした。`cancel` 内の
   `select { _ <- done; default -> close done }` は見た目は冪等だが、
   2 goroutine が同時に `default` 腕を取れば double-close する —
   コメントでその不変条件を明示している。
3. **`default` 腕とシグナルのチャネル化** — `default` 腕は
   「消費しない peek」として `is_done`（キャンセル後は次のファイルを
   開かない）と `cancel`（冪等化）の2箇所で使う。
   `signal.NotifyContext` の `ctx.Done ()` は `select` の腕にそのまま
   載るので、SIGINT も `--max-count` も同じ `done` キャンセルに収斂する。
   さらに SIGINT 腕では `signal.Reset` でデフォルト動作に戻す —
   ブロッキングな `ReadFile` には channel キャンセルが届かず、
   wedged なワーカーを2回目の Ctrl-C で殺せるようにするため。

ファイル分割は linkcheck と同じく「操作の木」:
`main`/`args`/`grep`（純粋な行判定）/`walk`/`pipe`（並行配管）。
`grep_text` は純粋関数として `test` で釘付け、`pipe.gom` のチャネル
補助関数（`is_done`/`cancel`/`send_item`/`wait_workers`）も `test`
ブロックで直接検証した（`time.After` を負け腕にした select で
「まだ開いている」「閉じた」の両方を確かめる形）。

## 実装の概要

| ファイル | 責務 | 中の並行要素 |
| --- | --- | --- |
| `main.gom` | `main`/`run`、正規表現コンパイル、終了コード | — |
| `args.gom` | `Command`/`Options`、引数文法、usage | — |
| `grep.gom` | `Match`/`Item`、`grep_text`/`grep_file` | —（純粋な中核） |
| `walk.gom` | 再帰ウォーカー → `paths` | `send` 腕 select + `done` |
| `pipe.gom` | プール、countdown closer、printer、キャンセル | `go`/`Chan`/`select`/`signal` |

パイプラインの形（`pipe.gom` 冒頭の図そのまま）:

```text
walk ──paths──▶ matcher ×N ──results──▶ print_all (main goroutine)
                 │   ▲                     │
                 └─fin┘                     └─can `done`──▶ everyone above
                 wait_workers closes results after N fins
```

`matcher` には `Ptr regexp.Regexp` ではなく `String -> Bool` の関数値を
渡す — 関数値は mutable でないので共有 lint（W0702）を踏まず、ptr を
越境させる必要もない（linkcheck で「呼び出しごとに client を作る」で
回避した問題の別解）。

CLI 契約: `rgrep [--concurrency N] [--max-count N] [-i] [--] PATTERN
[PATH...]`。match は stdout に `path:line:text`、警告（読めない path、
binary ファイル）は stderr。終了コード: 見つかれば 0、なければ 1、
警告/usage エラーは 2、SIGINT は 130。

## 案 F のチェックリスト評価

| 見せるもの（案 F より） | 判定 | 実際の箇所 |
| --- | --- | --- |
| 多段パイプライン（fan-in との対比） | ○ | walk → matcher×N → print_all の 3 段 + `fin` countdown の補助段 |
| 固定ワーカープール（spawn+semaphore との対比） | ○ | `spawn_matchers` が N 個 `go`、終了は `paths` の close で自然に来る |
| `Chan.close` の連鎖シャットダウン | ○ | `paths`→`fin` countdown→`results`。`wait_workers` が最後の `fin` で `results` を閉じる |
| `select` の送信腕 | ○ | `send_path`（walk）、`send_item`/`send_matches`（matcher）、いずれも `_ <- done` 腕付き |
| `default` 腕 | ○ | `is_done`（非ブロッキング peek）、`cancel`（冪等 close） |
| `signal.NotifyContext` + `ctx.Done ()` graceful stop | ○ | `search` が `ctx.Done ()` を `print_all` の select 腕に直載せ。`signal.Reset` で2回目 Ctrl-C 復活 |
| `context` 相互運用 | △ | `NotifyContext` 自体は動くが、返り値の `context.CancelFunc` は Gom から呼べない → 新規ギャップ（後述） |

テスト面の要求 — 「matcher の行判定を純粋関数 `test`」「一時ツリーで
`--max-count` 打ち切り後に素早く終わる」「ワーカーが漏れない」— は
すべて満たした。ワーカー漏れは直接 goroutine 数を数えるのでなく、
「`--max-count` 実行が束縛時間内に終わる」ことで保証する
（漏れていれば `wait_workers`/`print_all` が詰まって timeout する）。

pitfalls の「送信側が done を見ないと漏れる」は上記の設計として回避。
「`WalkDir` 相互運用は未検証」は —— そのまま未検証で、回避して
`os.ReadDir` 手動再帰にした（不足・限界を参照）。

## 検証

- `gom check -format text examples/rgrep` — エラー 0、警告 0
- Gom の `test` ブロック 11 個（args 5 / grep 2 / pipe 4。
  うち pipe の 4 個はチャネル動作の直接検証）
- `go test ./examples/rgrep` — 生成モジュール経由で test ブロックを実行しつつ、
  実プロセスの CLI を一時ツリー相手に検証: 再帰マッチと行番号・`-i`・
  no-match exit 1・ファイル直指定・binary+missing path の warn/exit 2・
  不正正規表現 exit 2・`--max-count 3` over 50 files で出力 3 行+即終了・
  引数エラー 5 件・`--help`・**実 SIGINT**（FIFO を読ませて wedged にし、
  1 発目で生き残り 2 発目で signal kill、またはキャンセルが先に効いて
  exit 130、の両経路を受理）
- Devin Review の指摘で引数パーサの sentinel バグと stdout 書き込み
  失敗の握り潰しを修正（`--` 直後の hyphen パターン・空パターン・
  `--max-count ''` の3点を `Option` 化で、`print_match` の `Fprintf`
  エラーは warn+キャンセルで exit 2 に）
- `gofmt -l .` / `go vet ./...` / `scripts/check-sumtype.sh` / `go test ./...` —
  すべてパス

## 発見した言語のギャップ

`signal.NotifyContext` の第 2 戻り値 `stop: context.CancelFunc` を
Gom から呼ぶ方法がない。`stop ()` は E0401（`context.CancelFunc` が
呼び出し可能型として扱われない）、`Go.fn0 stop` は E0510（named な
`func()` は `Go.Func0` に読まれない）、`Go.conv stop : Unit -> Unit` は
型検査を通るが `func(struct{}) struct{}` を生成して `go build` で落ちる。
`Go.FuncN` の unnamed func 対応は named func 型に及んでいないという
ことになる。rgrep ではプロセス終了直前なので `ignore stop` で回避したが、
ライブラリ的な再利用コードでは cancel 関数を呼べないのは実害になる。
`docs/implementation-gaps.md` に登録済み。

## 不足・限界

計画文とのずれと、CLI としての限界。

- **`WalkDir` 相互運用は未検証のまま**: 案 F で予想されていたとおり。
  `filepath.WalkDir` のコールバック型（`fs.WalkDirFunc`）は named func
  型なので `Go.FuncN` ブリッジが乗らない可能性が高く（今回の CancelFunc
  問題と同根）、未検証のまま `os.ReadDir` の手動再帰にした。named func
  型コールバックを取る Go API 全般が同じ壁に当たる — 検証と対応は
  implementation-gaps の追跡対象。
- **ブロッキング IO にキャンセルは届かない**: FIFO や応答しないデバイスの
  `ReadFile` に入った matcher は `done` を見るまでもなく詰まる。
  `signal.Reset` で2発目の Ctrl-C がプロセスを殺すフォールバックを
  用意し、これは教訓として README とテストの両方に残した。Go でも
  context は syscall を中断しないので言語固有の限界ではないが、
  「キャンセルできたはずなのに」を説明する恰好の例である。
- **`--max-count` は合計カウント**: GNU grep の `-m` はファイルごとだが、
  本実装はパイプラインの出口で数える総数。並行なので per-file は
  順序不定と相性が悪く、教訓上も総数のほうが素直。
- **警告が exit 2 を引く**: binary ファイルのスキップも `Warn` に流す
  設計のため、match あり + binary スキップ = exit 2。GNU grep は
  binary でも 0/1 を返す。警告を stdout の外に出す設計の見せどころ
  として採用したが、grep 互換ではない。
- **出力順は非決定**: worker プールなので `path:line:text` の順序は
  不定（grep -r と同じ制約）。テストは sort して比較。
- **matcher は `is_done` をファイル境界でしか見ない**: 巨大な 1 ファイルの
  途中ではキャンセルが効かない（行単位のチェックは入れていない — 
  見せる複雑さを増やさないため）。

## 今後

- named func 型（`context.CancelFunc`、`fs.WalkDirFunc` など）を
  `Go.FuncN` として読むか、呼び出し専用のブリッジを足すかは別途の決定。
  implementation-gaps に登録済み。
- 巨大ファイル内での行単位 `is_done` チェック、`--include`/`--exclude`
  グロブは例としての密度を損なわない範囲なら拡張余地。
