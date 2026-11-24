# sandbox 探索 — 過去 run の試作一覧

次の探索者が題材の重複回避に見る一覧。各項目はタイトルと 1 行説明
と結果の 1 語分類だけ — アーカイブ本文へのリンクは張らない
（先に読んでしまう不具合を避けるため — 探索結果の先読みで観測が
引っ張られた run 2 の問題）。棚卸し（プロンプト B —
[sandbox-exploration.md](sandbox-exploration.md)）が run ごとに
追記していく。

見るタイミング:

- **アイデアメモ時**: タイトルと説明だけを見て題材の重複を避ける
- **再検証フェーズ（探索後）**: 結果欄も見てよい — 過去の
  `断念`・`一部断念` が直ったかを `sandbox/recheck/` の候補にする。
  `成功` が多い領域は次の題材から外し、難易度の高い題材に寄せる
  材料にも使う

結果の凡例: `成功` / `一部迂回` / `一部断念` / `断念` / `未検証`。
結果欄はその時点の最新状態 — 過去 run で失敗しても後の run の
recheck で成功したら `成功` に更新する（棚卸し時・recheck 実施時に
直す）。

## run 1（PR #163）

- `wcl` — wc クローン（行・単語・バイト数カウント） — 成功
- `b64` — base64 エンコーダ/デコーダ — 成功
- `pick` — 簡易 grep（行フィルタ） — 成功
- `jqp` — jq もどき（JSON フィールド抽出） — 成功
- `jot` — タイムスタンプ付きメモ追記 — 成功
- `s256` — sha256sum クローン — 成功
- `duu` — ディレクトリサイズ集計（du もどき） — 一部迂回
- `hs` — HTTP ステータスチェッカ — 成功
- `calc` — 式評価器（sum type の見せ場） — 成功
- `srv` — HTTP サーバ — 成功
- `runner` — タイムアウト付きコマンド実行 — 成功
- `tpl` — text/template のラッパ — 成功
- probes — json.Marshal / flag / regexp / select+signal+ticker /
  @method（Stringer）/ 共有 Ref / generics / gom test — 成功
- `slices` で record ソート（`Go.conv`/`SortFunc`/`Sort` 全経路）
  — 成功（run 2 recheck: `~[]E` 要素一致 + `let gs : Go.Slice T = xs`
  で可能）
- record に `http.Handler` 実装（`@method` が ServeHTTP initialism
  を出せない） — 成功（`@method(ServeHTTP)` verbatim 名が実装済み）
- `//go:embed` / blank import — 成功（`//gom:embed` + `@extern let`
  で実装済み）

## run 2（PR #196）

- `csvcut` — CSV 列選択（cut 的） — 成功
- `until` — 日付カウントダウン — 成功
- `pscan` — TCP ポートスキャナ（spawn 扇出） — 成功
- `freq` — 単語頻度カウンタ — 成功
- `rmv` — 正規表現バッチリネーム — 成功
- `tabl` — テーブル成形 — 一部断念（`text/tabwriter` は uint で断念、
  手書きパディングに退避）
- `passgen` — パスワード生成 — 成功
- `jsonl2csv` — JSONL → CSV 変換 — 成功
- `shop/` — DMMF 風ドメインパッケージ（`.gomi` でコンストラクタ封じ）
  — 成功
- `sandbox/recheck/` — run 1 指摘項目の再検証 probe 群 — 未検証
