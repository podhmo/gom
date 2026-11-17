# 生成行ごとの `//line` 再置き（per-line re-pin）で drift を消す

- 状態: accepted
- 日付: 2026-11-17
- 問い: [open-questions](../open-questions.md) ツール項、[implementation-gaps](../implementation-gaps.md)「`//line` ドリフトによる位置エイリアシング」
- podhmo 決定（2026-11-17）: (a) emission 変更を採用、(b) fold 側精緻化と (c) 現状維持は却下

## 問題

1 つの `.gom` 行が複数の生成行に展開されるとき、`//line` ディレクティブは直後の 1 行しか pin しない。pin されない継続行は Go の行番号算術で `N+k` にドリフトし、報告行が「実在するが無関係な」`.gom` 行に alias される。wave-13 の `ResolveMatch`（exact/drift/ambiguous fold）はこの算術を逆にたどって報告を構文要素に戻すが、alias そのものは残っていた — drift は消せないと仮定した上での近似だった。

## 選択肢

- **(a) emission 変更（採用）**: codegen が構文要素ごとに `//line` を再置きし、drift する行を生成しない。
- **(b) fold 側精緻化**: `ResolveMatch` の候補絞り込みをさらに育てる。**却下** — 「調整の繰り返しになる」。alias は発生側の構造問題であり、inverse 側で個別の形を追い続けるのは終わりがない。
- **(c) 現状維持**: **却下** — 報告が誤った実在行に落ちるのは診断の信頼性そのものを損なう。fold の ambiguous フラグは存在を示せても、利用者には何も改善しない。

## 採用した設計

re-pin の境界規則は「construct の先頭を列挙する」のではなく、**emit される物理行すべて**にした。emission は行指向なので、行を出す場所すべてが構造の境界になる:

- `gen.line(sp)` は span を sentinel 行として即座には書かず、`gen.curSpan` に記録するだけにした。
- `gen.pad()` — 各行頭のインデントを出す唯一の場所 — が pending な行頭で `curSpan` の sentinel を 1 行吐く。つまり emit される全行が自分の pin を持つ。
- `open()`/`close()`/`closeInline()`/`midElse()` で `curSpan` を `spanStack` に push/pop し、`}`・`} else {` などのデリミタ行は「その括りの内側の construct の span」（通常は括っている宣言の span 開始行）を名指す。

具体例での帰結:

- `if` / match 腕: 腕内の各行は腕の span、腕を閉じる `}` は `if`/match を括る construct の行を指す。
- 複数行式・ネスト lambda: 内側の行は内側の construct の span に pin され、外へ戻る行は pop された span を再び名指す。
- pipe chain: lowered な一時変数行は lowering が付けた stage span を `line()` 経由で持つので、stage ごとに正しい `.gom` 行へ pin される。

directive 数は増える（各行 1 個）。代償として「どの construct の先頭に再置きするか」の個別設計が不要になり、継続行の drift が構造上発生しない — 未 pin の生成行は存在しないので `N+k` の算術に曖昧さが残らない。

## SourceMap との一致

`diag.SourceMap` の anchor 表と `//line` directive は二系統だが、両者は `spliceLines` で同じ `marks` 表から組み立てられる（sentinel の rewrite と anchor の記録が同一操作）。今回の変更は sentinel の**出し方**だけを変え、rewrite/anchor 生成は触っていないため、構造上の一致は保たれる。検証: `internal/build/goposmap_test.go` の wave-13 drift fixture — emitted `//line` が正しい `.gom` 行を指し、`ResolveMatch`/`GoToolDiagnostics` が single answer を返す。残る ambiguous は drift 由来ではなく、同一 `.gom` 行を複数の別 span（例: `pair 5` 呼び出しと `(a, b)` パターン）が要求する真の曖昧性のみ。

## 影響

- `testdata/golden` 広範更新 — 差分は生成コードの `//line` のみ。
- stdlib 生成物（`gom test stdlib/<pkg>`）も再生成。
- `MatchDrift` 分類は残置（map 未登録や旧生成物への fold でまだ使う）。gaps 台帳の drift 項目はこの変更で消滅した旨を追記済み。
