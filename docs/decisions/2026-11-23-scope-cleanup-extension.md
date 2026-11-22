# scoped cleanup 拡張: エラー合流の一般化と取得+登録糖衣（案）

- 状態: 採択（案 D、2026-11-23 wave35 開始前インタビュー）
- 日付: 2026-11-23
- 問い: [scoped-cleanup](2026-11-21-scoped-cleanup.md) の残論点 —
  cleanup の失敗を `GoError` 以外のエラー型で合流させる版（結合関数を
  明示的に受け取る）と、取得と登録を一度に書く糖衣（`s.closing` など）
  ・専用ブロック構文のどちらに重みを置くか（[open-questions](../open-questions.md)）

## 背景

現状の仕組み:

- `rt.Scope` は `func() error` のスタックを保持し、`Close` が LIFO で
  実行して `errors.Join` する — 合流の単位は Go の `error` に固定
- `Scope.run_result`（`stdlib/result`）は本体が `Result a GoError` の版。
  `Scope.defer_err` のコールバックも `Unit -> Result Unit GoError` 固定で、
  失敗は `Close` が join して本体 Ok のとき結果に合流する
- `Scope.run` はエラー合流なし（本体が GoError を返さないケース用）

したがってドメイン独自のエラー型 `E`（Gom の ADT 等）で
cleanup の失敗を結果に合流させる入口はない。

## 今日でも書ける代替経路

調査で確認した代替:

1. **境界で `Result.map_err`** — `Scope.run_result` が既に cleanup の
   失敗を `GoError` に合流するので、そのまま `|> Result.map_err f` で
   ドメイン型 `E` に橋渡しできる。「cleanup 自体が `E` を返す」場合も
   `defer_err` のコールバック内で `E -> GoError` に変換して登録すれば
   合流には乗る。実害は「`E` → `GoError` → `E` の往復でエラーの形が
   失われる」点のみ
2. **取得+登録の糖衣はユーザー関数で書ける** — `s` は helper 関数への
   引数渡しが許される（escape 検査が「引数を保持しない呼出し」を許す）
   ので、

   ```gom
   let closing (s: Scope) (open: Unit -> r) (close: r -> Unit) : r = {
     let x = open ()
     Scope.defer s (fn () -> close x)
     x
   }
   ```

   がそのまま書ける。組み込みにする差分は綴りの短さのみ

## 各案の費用

### 案 A: 結合関数を受け取る版（`run_result` の `E` 一般化）

`Scope.run_result_with join (body: Scope -> Result a E) : Result a E` など。
cleanup が `E` を返すためにはスコープが `error` ではなく `E` を集める
必要があり、次のいずれかになる:

- `Scope` をエラー型で型パラメータ化する（`Scope e`）— 組み込み型の
  表面・`.gomi`・escape 検査の `isScopeType` まで波及する大きい変更
- `func() E` を集める別のランタイムスコープ型 + 対応するモジュール面 —
  `Scope` と `Scope e` が並立し、どちらを `s` とするかの規則が要る

どちらも「静的に逃げないスコープ値」という SC-1/SC-2 の単純な型と
整合させるのが難しい。`map_err` 経路で実害がほぼ塞がっている今、
型を触る費用に見合わない。

### 案 B: 取得+登録糖衣（`s.closing` / `using` 構文）

上記のとおりユーザー関数で書ける。組み込み糖衣にすると
`Scope` メンバまたは構文の追加で、元の decision の「使って困ってから」
の条件にまだ達していない。

### 案 C: 専用ブロック構文

`using s = Scope.open in ...` 類。B よりさらに大きい表面変更で、
同じく需要未確認。

## 決定（案 D）

**現状維持 + 代替経路の文書化。** spec §6.1（または guide）に
「cleanup の失敗をドメインエラー型へ合流させたいときは
`run_result` → `Result.map_err` で橋渡し」「取得+登録はユーザー側の
helper 関数として書ける」ことを明記し、案 A–C は引き続き
future work として残す。

案 A が必要になる条件は「cleanup 側がドメインエラー `E` を返し、
`GoError` への往復で形が壊れるのが許容できない」実例が現れたとき —
そのときはエラー型をパラメータに持つスコープ値の設計（案 A）を
再検討する。

## 選択肢

- 案 A: `run_result_with join` などで結合関数を受け取る版 — 棄却候補。
  scope が `E` を集める型変更まで波及する
- 案 B: `s.closing` 糖衣 — 棄却候補。ユーザー helper で書ける
- 案 C: 専用ブロック構文 — 棄却候補。B より大きい
- 案 D: 現状維持 + 代替経路の文書化 — 採択
