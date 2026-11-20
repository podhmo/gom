# defer が解いている問題の調査と、Gom 側の解き方の比較

- 日付: 2026-11-21
- 目的: Go の `defer` の使われ方を分類し、Gom で解くべき問題と解き方の候補を試作で比べる
- 関連: TODO なし（新規の問い）
- 実験: [experiments/defer](../../experiments/defer/README.md)
- 決定案: [decisions/2026-11-21-scoped-cleanup](../decisions/2026-11-21-scoped-cleanup.md)（draft）

Go の `defer` 構文を Gom に入れる話ではなく、Go が `defer` で解いている
問題の一覧を作り、Gom の式指向・`Result`・`try` の流儀で解けるかを
今日のコンパイラで試した記録。規範ではない。

## 1. 使われ方の集計

`experiments/defer/survey` で Go 1.27 の `$GOROOT/src` を AST で分類した
（`if:*` の細分類は構文上のヒューリスティック）。

| 分類 | 非テスト | 備考 |
| --- | ---: | --- |
| ファイル | 3984 | テスト側 1799 |
| defer 文 | 1876 | テスト側 3360 |
| defer を含む関数 | 1517 | |
| 2 個以上の defer を持つ関数 | 224 | LIFO が意味を持つ |
| 関数のトップレベル | 1423 (75.9%) | |
| `if` の中 | 381 (20.3%) | |
| └ 登録が分岐の外まで生きる | 358 | 分岐を抜けた後の処理より後に走る |
| └ 分岐の先頭で即登録（guard のみ: `if p.trace { defer un(trace(...)) }`） | 208 | |
| └ 分岐内で取得してから登録（`if ... { mu.Lock(); defer mu.Unlock() }`） | 189 | |
| `switch` の中 | 47 | |
| ループの中 | 21 | 関数終了まで溜まる（`net/dnsclient_unix.go` の `defer cancel()` など） |
| `select` の中 | 4 | |
| defer される closure | 339 | |
| closure 内で `recover` | 72 | テスト側 330 |
| closure が named result を読む | 64 | `if err != nil { rollback }` 型 |
| closure が named result を書く | 61 | close エラーの合流・エラーの包み直し・recover → error |
| 引数に呼び出しを含む（評価時点が登録時） | 115 | `defer un(trace(p, "X"))` |

対象の内訳は `Unlock`/`RUnlock` が約 2 割、`Close` 系、`cancel()`、
`Done()`、状態の復元（`defer func() { x.f = old }()`）が続く。手元の
Go リポジトリ群（1282 ファイル・1001 defer）も同じ傾向だった。

## 2. 解くべき問題の一覧

| # | 問題 | Go での形 | 代表例 |
| --- | --- | --- | --- |
| P1 | 取得したものを全出口で解放（LIFO） | `defer f.Close()` | 全体の大半 |
| P2 | 条件付きで取得し、分岐の外まで保持 | `if c { mu.Lock(); defer mu.Unlock() }` | `net/http/httputil/persist.go`, `os/stat_windows.go` |
| P3 | 可変状態の一時変更と復元 | `defer func() { x.f = old }()` | `encoding/json/v2` 系, `testing` |
| P4 | 反復ごとの寿命 | ループ内 `defer cancel()`（実際は関数末まで溜まる） | `net/dnsclient_unix.go` |
| P5 | cleanup の失敗を結果へ合流 | named `err` を closure が書く | `if cerr := f.Close(); err == nil { err = cerr }` |
| P6 | 失敗時だけ巻き戻す（errdefer） | named `err` を closure が読む | tx rollback, 書きかけファイルの削除 |
| P7 | panic でも cleanup を走らせる | `defer` 全般 | lock 解放 |
| P8 | panic を捕まえて error に変える | `defer func() { if r := recover(); ... }()` | `debug/gosym/pclntab.go`, `internal/trace/reader.go` |
| P9 | 出口で結果を観測・加工する | named result を読む／包み直す | `defer un(trace())`, エラーの wrap |

## 3. 候補と検証結果

### (A) ローンパターン（今日の Gom、`experiments/defer/aloan`）

`with_res log name (fn r -> ...)` を Gom で書いた。P1 は書けるが資源ごとに
ラムダが 1 段増え、close エラーの合流（P5）は helper ごとに手書き。
P2 は「取得するかもしれない」ローンが書けず、`if` の両枝に残りの処理を
継続として渡す形（`with_lock_if mu need (fn () -> ...)`）になる。分岐が
2 つ以上の条件付き取得を持つと継続が組み合わせ爆発する。P7 は解けない
— Gom に `recover` がない（spec 6 章）ので、body が panic すると解放が
飛ぶ（テストで固定）。

### (B) ブロックスコープの `defer`（Swift / Zig 型）

ブロック末尾で走る規則だと P2 の `if c { lock; defer unlock }` は `if`
を抜けた瞬間に unlock する — 調査で `if` 内 381 件のうち 358 件が分岐の
外まで生きる登録だったので、主要ケースを解けない。Gom の `if` は式で、
ブロックの値と cleanup の順序（値を評価してから cleanup か）も新たに
決める必要がある。試作せず不採用候補とした。

### (C) 関数スコープの `defer`（Go 型の移植）

P2 は解けるが、スコープ境界が「最も内側の関数」で暗黙になる。Gom では
ラムダ・`try` 境界・self-tail-recursion のループ化が関数境界を動かす
ので、どこで走るかが書き方の小変更で変わる（tail call をループに
lowering した関数で `defer` すると反復ごとではなく関数末まで溜まり、
P4 の Go の罠をそのまま持ち込む）。P5/P6/P9 は named result という
暗黙の可変変数を利用者に見せることになり、明示的可変性の方針に反する。

### (D) 明示的スコープハンドル（今日の Gom + ランタイム、`experiments/defer/dscope`）

```gom
fn copy_file (log: Ptr fake.Log) (src: String) (dst: String) (data: String) -> Result Unit GoError =
  with_scope_result
  <| fn s -> {
    let r = try fake.Open (log, src)
    s.Defer (Go.func0 (fn () -> ignore r.Close ()))
    let w = try fake.Open (log, dst)
    s.DeferErr (Go.func0 (fn () -> w.Close ()))
    let _ = try r.Read ()
    w.Write data
  }

fn guarded (log: Ptr fake.Log) (mu: Ptr fake.Mutex) (need: Bool) -> Unit =
  with_scope
  <| fn s -> { if need { mu.Lock (); s.Defer (Go.func0 (fn () -> mu.Unlock ())) }; log.Add "work" }
```

スコープの境界は `with_scope*` に渡したラムダの終わりで、登録は
ハンドル `s` へのただの呼び出しなので `if` / `match` の腕 / 入れ子の
ラムダのどこでもできる。`try` は body ラムダから抜けるので、失敗時も
スコープの cleanup を通る。今日のコンパイラで `@extern` の多相関数と
Go のランタイム（`scope.Run` / `scope.RunResult`）だけで動いた。

| 問題 | (D) での書き方 | 結果 |
| --- | --- | --- |
| P1 | `s.Defer` / `s.DeferErr` を取得直後に | LIFO で解放、取得失敗時は登録済みだけ解放 |
| P2 | `if need { mu.Lock (); s.Defer ... }` | unlock は `work` の後 |
| P3 | `s.Defer (... Ptr.set p old ...)` | P1 と同じ機構 |
| P4 | 反復の中で `with_scope_result` を開く | 反復ごとに解放。tail call のループ化も維持（生成 Go は `for` + 即時関数呼び出し） |
| P5 | `s.DeferErr` | Ok の結果だけを close エラーが置き換える（body のエラーが優先） |
| P6 | `s.OnError` | Err か panic のときだけ rollback |
| P7 | 何もしない | panic 時も cleanup が走り、panic は再送出 |
| P8 | 対象外 | spec の「recover は提供しない」を維持 |
| P9 | スコープが式なので `with_scope_result (...) \|> Result.map_err wrap` | named result 不要 |

named result + `defer` が要るのは「どの出口からでも結果を書き換える」
ためだが、Gom ではスコープ全体が 1 つの式で、`try` の脱出先もその
境界なので、出口が 1 つにまとまる。結果の加工（P9）はスコープの値に
関数を適用すれば済み、cleanup 側から結果に触る必要が残るのは P5 / P6
だけ — この 2 つを `DeferErr` / `OnError` という限定された形で提供すれば、
利用者が可変な結果変数を見ることはない。

費用: (1) body が毎回クロージャになりランタイムのスタックを持つ、
(2) `Go.func0 (fn () -> ...)` の儀式、(3) `s` がスコープ外へ逃げても
検査されない（閉じた後の登録は実行時 panic）、(4) `gom fmt` が
複数行の body を 1 行に畳むことがある（`guarded`）。

### (E) (D) のコンパイラ組み込み化（目標の生成 Go、`experiments/defer/cgen`）

`Chan` / `Task` と同じくコンパイラ組み込みの `Scope` モジュールにし、
Core IR でスコープ境界と登録位置を確定させる案。スコープの body が
Go 関数（Gom 関数そのもの、または入れ子なら即時関数）になり、登録が
同じ Go 関数内に字面上あるなら、登録位置にそのまま Go の `defer` を
出せる — Go の `defer` は関数スコープなので、`if` の中の登録が分岐の
外まで生きるという Gom 側の意味とちょうど一致する。

```go
func guarded(log *fake.Log, mu *fake.Mutex, need bool) struct{} {
	if need {
		mu.Lock()
		defer mu.Unlock()
	}
	log.Add("work")
	return struct{}{}
}

func copyFile(log *fake.Log, src, dst, data string) (res result.Result[struct{}, error]) {
	...
	defer func() {
		if cerr := w.Close(); cerr != nil && res.IsOk() {
			res = result.Err[struct{}, error](cerr)
		}
	}()
	...
}
```

`DeferErr` / `OnError` は生成 Go 側でだけ named result と closure になる
（ユーザーの要件「変換後の Go に named return と defer が出るのは可」）。
`OnError` を panic 時にも走らせるには `ok` フラグが要る（`cgen/transfer`）。
`cgen` は手書きで、(D) と同じシナリオテストを通す。

## 4. 未決の論点

- cleanup 失敗の合流規則: 最初のエラー優先（Go の慣用、現実装）か `errors.Join` か
- `OnError` を panic 時にも走らせるか（現実装は走らせる）
- `s` の脱出を静的に禁じるか（第二級値・E コード）、実行時検査で済ますか
- 登録 API の面: `Scope.defer s (fn () -> ...)` のようなモジュール関数か、
  `use` 束縛のような糖衣（`use r = acquire; release`）を足すか
- 登録時に引数を評価する Go の挙動（115 件）は Gom ではラムダの捕捉で表すので
  意味の差はないが、捕捉が `let` 束縛の値であることを明記する
- P8（recover）は対象外のまま別の問いにする。テスト側の 330 件は
  `t.Fatal` 系の検査なので、Gom の `test` ブロックに必要かは別途調べる
