# V-1: 実行可能チュートリアルの検証レポート

- 目的: `tutorial/` にエンドユーザー視点の段階的なサンプルを作り、
  `gom build` → `go run` で実際に動かし、見つかった不備を TODO.md /
  docs/problems/ に記録する（TODO.md `V-1`）。
- 検証日: 2026-11-04, go1.27.1 linux/amd64, worktree `task/v-1`。
- 結果: **10 ステップすべてが実際にビルド・実行できる**（`tutorial/run.sh`
  と `go test ./tutorial/` で再現可能）。その過程でバグ 7 件・タスク 4 件・
  設計上の懸念 1 件を新規に記録した。

## チュートリアルの構成

```
tutorial/
  README.md            全ステップの解説 + 動かし方 + 既知の制約
  run.sh               gom をビルドし全ステップを実行・diff するシェル
  tutorial_test.go     同じドライバを `go test ./tutorial/` で実行
  01-hello … 09-capstone   main.gom + expected.txt + go.mod
  10-errors            badmatch.gom（E0301 用の意図的な失敗）+ expected-check.txt
```

各ステップは `module tutorial/NN` の独立 Go モジュール
（`replace github.com/podhmo/gom => ../..`）。生成 `main.go` は
コミットせず `.gitignore` 済みで、`cd tutorial/NN && gom build main.gom
&& go run .` がそのまま動く。

## 実行結果（実測）

| step | gom build | go run | 出力 |
| --- | --- | --- | --- |
| 01-hello | ok | ok | `hello, gom` |
| 02-values | ok | ok | `42 is big` / `1 is small` / `ratio: 1.5` / `bool: true` |
| 03-functions | ok | ok | `42` / `12` / `7` |
| 04-match | ok | ok | `3.14159` / `6` / `0` / `42` |
| 05-lists-records | ok | ok | `sum 10, len 4` / `moved to 15,20` / `at 15,20` / `answer = 42` |
| 06-option-result | ok | ok | `some 7` / `none` / `ok 10` / `err division by zero` |
| 07-go-interop | ok | ok | `ok 42` / `err strconv.Atoi: parsing "x": invalid syntax` / `GOM` / `gogogo` / `env: yes` / `a, b` |
| 08-pipes-labels | ok | ok | `6` / `22 / 22` / `42` / `hi, gom!` / `yo, pon!` |
| 09-capstone | ok | ok | `listening on port 8080` / `bad port: not a number` / `bad port: out of range` |
| 10-errors | — | — | `gom check` → exit 1, E0301 "non-exhaustive match on `Shape`"（missing_patterns: `Empty`） |

確認できた動作: `|?>` のステップ名自動ラップ（`strconv.Atoi: parsing "x":
invalid syntax`）、`try` 脱出、`>>`/`<<`/`<|`/`|>`、ラベル付き引数の
順不同・パンニング、レコードパターン・`as` 束縛・or パターン・ガード、
ADT の `==`（sealed interface 経由）、`_` プレースホルダ、メソッド呼び出し、
`Go.conv`、`for x in xs`、文字列補間、`test` ブロックの構文解析、
`import go` の `(T,error)`→Result・`(T,bool)`→タプル・`error`のみ→`Result Unit`
・`*T`→`Ptr T` の各マッピング。

## 新規に記録した不備

### [bug] — TODO.md `## Bugs`

1. **組み込み関数が `gom build` で呼べない**: `todo ()`・`not x`・`assert`・
   `assert_eq` は check を通るが build で E0901 "builtin X used as a value"。
   `internal/lower/expr.go` `applyCall` が CallInfo 振り分け前に `e.Fn` を
   `varExpr` へ下ろすため。`test` ブロック全滅。さらに E0301 の suggested
   fix `-> todo ()` がビルド不能なコードを挿入する。再現:
   `fn f () -> Int = todo ()`
2. **`==` が List で生成コードを壊す**: `[1,2]==[1,2]` は gom build 成功→
   go build で `slice can only be compared to nil`。ADT が List フィールドを
   持つ場合も `struct containing list.List[int64] cannot be compared`。
   spec 3.3 は構造的等価を要求（`rt.AssertEq` 相当の per-type compare が要る）。
3. **可変長 Go 関数への `f <| x` が壊れる**: `fmt.Println <| "hi"`
   → E0401 "expected Go.Slice Go.Any, got String" + E0407。RHS を可変長
   引数リスト（spread）として扱っている。非可変長 Go 関数・Gom 関数は正常。
4. **Go のパッケージ変数・定数が E0901**: `os.Args`・`math.Pi`・
   `strconv.ErrSyntax` → "core invariant violation: symbol X is not in
   scope"。spec 8.1.5 は定数の解決を約束。未実装なら内部エラーでなく
   ユーザー向け診断を出すべき。
5. **ガード内のパターン変数同士の `==` が E0407**: `P a b if a == b` →
   "the type of `a` could not be determined"。`a == 3` は通る。
6. **span を持たない診断**: `os.LookupEnv`（(String,Bool) タプル）を
   `Some`/`None` で match → `"span"` を欠く E0407 が 2 件。
7. **`GoError` のメソッドセットが露出していない**: `e.Error()` → E0401。
   エラーの中身を Gom 側で検査できない。

### [task] — TODO.md `## Bugs`（同節に [task] タグで記載）

8. **診断カスケード**: `slices.Contains` → E0501 + 誤解を招く E0401
   "takes 0 argument(s)" + E0407。`type Meters = Int` 誤用で同一 E0401 が
   3 連発。
9. **空 `select {}` が E0901**（内部不変条件エラーがユーザー入力で出る）。
10. **`gom build` が `test` ブロックを lower する**（spec 11.6 は test コードを
    `gom test` のみ出力としているが、build でも `assert_eq` の builtin バグを踏む）。
11. **spec の例がコンパイルできないものの整理**: 冒頭例（`List.map`/`List.sum`）、
    8.1 `report` 例（`ignore` + `strings.Join` の `List` 引数）、10 章
    （`Option.get`/`Chan`/`Task`）。

### [problem] — docs/problems/2026-11-04-goerror-construction.md

12. **GoError は作れない・調べられない**: `errors.New` が
    `Result Unit GoError` を返す設計のため `Err e` 束縛以外に GoError 型の
    式がなく、`errors.Is` の対象も `Err (errors.New ...)` も書けない。
    `e.Error()` 未露出（bug 7）と合わせて、Go 由来エラーを Gom で扱う
    出口がない。

## 既に記録済みで再確認したもの（重複記録しない）

- `ignore e` がパースできない → [problems/2026-11-04-ignore-discard-syntax-unparsed.md](2026-11-04-ignore-discard-syntax-unparsed.md)。
  チュートリアル 01 で最初に踏む場所（`fmt.Println` が Result を返す）。
- 複合値の表示が eval と生成コードでずれる → [problems/2026-11-04-interp-composite-display.md](2026-11-04-interp-composite-display.md)。
  `fmt.Println (Ok 42)` が `{1 42 <nil>}` と Go 内部表現を出すのも同系。
- `type Meters = Int` がエイリアスでなく variant `Int` になる罠 → [problems/2026-11-02-type-alias-body.md](2026-11-02-type-alias-body.md)。
- Go ジェネリック関数が全て hidden → [problems/2026-11-04-generic-go-fns-hidden.md](2026-11-04-generic-go-fns-hidden.md)
  （E0501 自体は出るが、後続カスケードが新規 task 8）。
- 同一パッケージ複数ファイルのスコープ共有 → open-questions.md（V-1 で
  `a.gom`/`b.gom` 実験で再確認。チュートリアルは単一ファイル構成で回避）。
- 未使用 let 束縛 → [problems/2026-11-02-unused-let-bindings.md](2026-11-02-unused-let-bindings.md)。

## spec と実装の食い違い（修正せず報告のみ）

- spec 冒頭例・8.1 `report`・10 章の例は未実装機能（stdlib P4-8、
  `ignore`、List↔slice 変換 P3-3）を使い現状コンパイル不可 → task 11。
- spec 8.1.5「Go の型なし定数は期待される型になる」→ 現状 E0901 → bug 4。
- spec 3.3「`==` は構造的等価」→ 現状 Go `==`（List で破綻）→ bug 2。
- spec 8.3「`(T,bool)` は標準バインディングで Option」→ `os.LookupEnv` は
  タプルのまま（P3-5 範囲、extern 未実装）。`extern` ブロック自体は
  パースされるが `Ptr T` で "Ptr takes 3 type argument(s)" と奇妙な arity
  エラーになることも確認（P3-5 未実装の範囲）。
- spec 11.6「テストコードは `gom test` のときだけ出力」→ `gom build` でも
  lower が `test` 本体を処理する → task 10。
- spec 12 の fixes が挿入する `todo ()` が build 不可 → bug 1 に包含。

## 観察した小さな差異・UX メモ

- `'z'`（Char）を `fmt.Println` に渡すと `122`（rune 値）だが `"${c}"` では
  `z`。scalars の display は strconv 系に合わせているため（P1-13 修正）、
  `fmt` 直渡しの表現とは見た目が違う。composite-display problem の隣の話。
- `GOTOOLCHAIN=local` で `gom build` が E0203 化する: gom 内部の `go list`
  が PATH の `go`（ここでは 1.26.8）を使うため。診断自体は `go list` の
  理由を正確に転写していて優秀。`tutorial/run.sh` は `$GO` を透過する
  形で回避を文書化。
- `gom build` は生成 `.go` を `go build`/`vet` しないので、codegen バグ
  （bug 2）が `gom build` 成功のままユーザー側の `go build` で初めて顕在化
  する。`//line` のおかげで位置は .gom に戻るが、spec 12.6 の
  `"source":"go"` 診断ラッピングは未実装（P4-5 系）。
- `os.Setenv`（`error` のみ返す）→ `Result Unit GoError` が `let _ =` で
  捨てられるのを確認。
- `extern` ブロックはパースされるが実装は P3-5（上記 arity エラーは
  未実装範囲の副産物）。

## 残る疑問

- `Char` の `fmt.Println` 表現（122 vs `z`）をどこまで揃えるかは
  display/Show の設計待ち。
- `fn main` の引数を取る形（`List String` など）への拡張は open-questions
  側の既存論点。
- `gom build` が `go vet`/`go build` 相当を走らせるか（生成コード検証の
  タイミング）— P4-5 の対応表と絡む。
