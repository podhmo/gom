# defer 相当: 明示的スコープハンドルによる cleanup 登録（案）

- 状態: draft
- 日付: 2026-11-21
- 問い: Go が `defer`（と named result）で解いている問題 — 全出口での解放、
  条件付き取得、反復ごとの寿命、cleanup 失敗の合流、失敗時の巻き戻し、
  panic 時の解放 — を Gom でどう表すか

## 決定（draft）

採否は podhmo が決める。調査と試作の記録は
[sketch](../sketch/2026-11-21-defer-survey.md)、実験は
[experiments/defer](../../experiments/defer/README.md)。

推す案は (E): スコープを値として開く組み込み `Scope` モジュール。

- `Scope.run (fn s -> body)` がスコープを開き、ラムダの終わり（`try` による
  脱出・panic を含む）で登録済み cleanup を LIFO に実行する。`Result` を
  返す版は cleanup の失敗を Ok の結果へ合流させる
- 登録はハンドルへの関数呼び出し（`defer` / `defer_err` / `on_err` 相当）で、
  `if` の腕・`match` の腕・ループ本体のどこからでもできる。寿命はスコープ
  まで（ブロックではない）
- 結果の加工はスコープの値に関数を適用して行う（`|> Result.map_err f`）。
  利用者に可変な結果変数（named result）は見せない
- lowering: 登録がスコープと同じ Go 関数内にあれば、登録位置に Go の
  `defer` を出し、結果に触る cleanup は生成 Go 側でだけ named result を使う。
  それ以外はランタイムの cleanup スタックへ落とす。判断は Core IR で行う

## 選択肢

- (A) 今日の Gom で書くローンパターン — 条件付き取得が継続渡しになり、
  panic 時に解放が飛ぶ（Gom に recover がない）。不採用候補
- (B) ブロックスコープの `defer` — `if` 内の登録（調査で 381 件中 358 件）が
  分岐を抜けた時点で走ってしまう。不採用候補
- (C) Go 型の関数スコープ `defer` — 境界がラムダ・tail call のループ化で
  暗黙に動き、named result を利用者に見せる。不採用候補
- (D) (E) と同じ面を `@extern` + ランタイムで提供 — 今日動く（実験済み）。
  費用はクロージャとランタイムスタック、`Go.func0` の儀式、`s` の脱出が
  静的に検査されないこと。(E) を入れるまでの暫定、または (E) の
  フォールバック経路として残せる
- (E) (D) のコンパイラ組み込み化 — 推す案

## 未決

- cleanup 失敗の合流規則（最初のエラー優先か `errors.Join` か）
- `on_err` を panic 時にも走らせるか
- `s` の脱出を静的に禁じるか
- 登録 API の綴り（モジュール関数のみか、`use` 束縛の糖衣を足すか）
- recover（P8）は本案の対象外。spec の「recover は提供しない」を維持する
