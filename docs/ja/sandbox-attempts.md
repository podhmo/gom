# sandbox 探索 — 過去 run の試作一覧

次の探索者が**アイデアメモを書くときにだけ**見る、題材の重複回避用の
一覧。各項目はタイトルと 1 行説明だけ — アーカイブ本文へのリンクは
張らない（先に読んでしまう不具合を避けるため — 探索結果の先読みで
観測が引っ張られた run 2 の問題）。棚卸し（プロンプト B —
[sandbox-exploration.md](sandbox-exploration.md)）が run ごとに
追記していく。

## run 1（PR #163）

- `wcl` — wc クローン（行・単語・バイト数カウント）
- `b64` — base64 エンコーダ/デコーダ
- `pick` — 簡易 grep（行フィルタ）
- `jqp` — jq もどき（JSON フィールド抽出）
- `jot` — タイムスタンプ付きメモ追記
- `s256` — sha256sum クローン
- `duu` — ディレクトリサイズ集計（du もどき）
- `hs` — HTTP ステータスチェッカ
- `calc` — 式評価器（sum type の見せ場）
- `srv` — HTTP サーバ
- `runner` — タイムアウト付きコマンド実行
- `tpl` — text/template のラッパ
- probes — json.Marshal / flag / regexp / select+signal+ticker /
  @method（Stringer・ServeHTTP）/ slices.Sort / embed / 共有 Ref /
  generics / gom test

## run 2（PR #196）

- `csvcut` — CSV 列選択（cut 的）
- `until` — 日付カウントダウン
- `pscan` — TCP ポートスキャナ（spawn 扇出）
- `freq` — 単語頻度カウンタ
- `rmv` — 正規表現バッチリネーム
- `tabl` — テーブル成形（tabwriter 代替）
- `passgen` — パスワード生成
- `jsonl2csv` — JSONL → CSV 変換
- `shop/` — DMMF 風ドメインパッケージ（`.gomi` でコンストラクタ封じ）
- `sandbox/recheck/` — run 1 指摘項目の再検証 probe 群
