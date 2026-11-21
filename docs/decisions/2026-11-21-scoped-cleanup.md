# defer 相当: 明示的スコープハンドルによる cleanup 登録（案）

- 状態: 採用（SC-1 で実装・検証した — [sketch](../sketch/2026-11-21-defer-survey.md)。SC-2 / SC-3 は TODO）
- 日付: 2026-11-21
- 問い: Go が `defer`（と named result）で解いている問題 — 全出口での解放、
  条件付き取得、反復ごとの寿命、cleanup 失敗の合流、失敗時の巻き戻し、
  panic 時の解放 — を Gom でどう表すか

## 決定（draft）

調査と試作の記録は [sketch](../sketch/2026-11-21-defer-survey.md)、実験は
[experiments/defer](../../experiments/defer/README.md)。2026-11-21 に podhmo が
(E) を選び、下の論点にも回答した。compiler prototype で検証し、結果を sketch に
書いてから状態を進める。

2026-11-21: SC-1（`Scope` 型と rt-backed モジュール）をコンパイラに実装し、spec 6.1 と
design 4.7.1 を更新した。Gom の型はメソッドを持たないので、登録はモジュール関数
`Scope.defer s f` / `Scope.defer_err s f` / `Scope.on_err s f` で書く。
静的な脱出検査（SC-2）とネイティブ `defer` への lowering（SC-3）は TODO.md。

(E): スコープを値として開く組み込み `Scope` モジュール。

- `Scope.run (fn s -> body)` がスコープを開き、ラムダの終わり（`try` による
  脱出・panic を含む）で登録済み cleanup を LIFO に実行する。`Result` を
  返す版（`Scope.run_result`）は cleanup の失敗を結果へ合流させる
- 登録はハンドルへの関数呼び出し（`s.defer` / `s.defer_err` / `s.on_err`）で、
  `if` の腕・`match` の腕・ループ本体・その場で呼ばれるラムダのどこからでも
  できる。寿命はスコープまで（ブロックではない）
- 結果の加工はスコープの値に関数を適用して行う（`|> Result.map_err f`）。
  利用者に可変な結果変数（named result）は見せない
- lowering: 登録がスコープと同じ Go 関数内にあれば、登録位置に Go の
  `defer` を出し、結果に触る cleanup は生成 Go 側でだけ named result を使う。
  それ以外はランタイムの cleanup スタックへ落とす。判断は Core IR で行う

### 論点への回答

1. cleanup 失敗の合流: 本体の `Err` を優先する。本体が `Ok` なら、失敗した
   cleanup のエラーを実行順（LIFO）に `errors.Join` でまとめて `Err` にする。
   実験の `scope.RunResult` と `cgen/writePair` で確認済み
2. エラー型: `defer_err` は `GoError` に限る（`errors.Join` が Go の `error`
   専用のため）。独自エラー型向けに結合関数を受け取る版は future work
3. `on_err` は panic 時にも走らせる（巻き戻してから panic を続ける）。
   問題が見つかったら、その内容を把握してから見直す
4. `s` は「逃げない値」として静的に限定する。`s` を返す、record・
   コンストラクタ・可変セルに格納する、`Task.spawn` や Go の関数値へ渡す、
   のいずれかを型エラーにする。`s` を引数に取る helper や、その場で呼ばれる
   ラムダからの捕捉は許す
5. 「ラムダ引数が逃げない」の判定: 同じパッケージ内は関数本体から推論し、
   結果を `.gomi` に注釈として出す。Go の関数は常に「逃げる」扱い
6. 綴り: 関数とメソッドだけ（`Scope.run <| fn s -> ...`）。取得と登録を
   一度に書く糖衣（`s.closing` など）や専用ブロック構文は、使って困って
   から考える（Go の struct literal 対応と同じ扱い）

recover（P8）は本案の対象外。spec の「recover は提供しない」を維持する。

## 選択肢

- (A) 今日の Gom で書くローンパターン — 条件付き取得が継続渡しになり、
  panic 時に解放が飛ぶ（Gom に recover がない）。不採用
- (B) ブロックスコープの `defer` — `if` 内の登録（調査で 381 件中 360 件）が
  分岐を抜けた時点で走ってしまう。不採用
- (C) Go 型の関数スコープ `defer` — 境界がラムダ・tail call のループ化で
  暗黙に動き、named result を利用者に見せる。不採用
- (D) (E) と同じ面を `@extern` + ランタイムで提供 — 今日動く（実験済み）。
  費用はクロージャとランタイムスタック、`Go.func0` の儀式、`s` の脱出が
  静的に検査されないこと。(E) のフォールバック経路（ランタイムの cleanup
  スタック）として残す
- (E) (D) のコンパイラ組み込み化 — 選択
