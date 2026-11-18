# concat の buffer 実装 (wave19 lane 3)

- 日付: 2026-11-18
- 対象: `List.concat` — open-questions「`concat` の O(k) スタック」

## 判定

`(a) @extern fn で buffer loop に移す` を採用 — 右畳み込み（`append xs (concat_go rest)`）はコピー量こそ O(n) だったが部分リスト数ぶんのスタックを積み（O(k) 深さ）、深い list-of-lists で破綻し得た。`@extern fn` + impl.go で「総長を先に数えて prealloc、1 回ずつ append」の形に置き換え、O(1) スタック・O(n) 時間・1 alloc。

pin する選択肢 (b) は不採用 — 実用域で耐えるとしても O(k) スタックは言語ランタイムの脆弱性として残るため。

## 計測（`go test -bench BenchmarkConcat`、8 要素の部分リスト k 個）

| k | before (再帰右畳み込み) | after (buffer) |
| --- | --- | --- |
| 64 | 40,683 ns/op | 2,248 ns/op (~18×) |
| 512 | 1,510,676 ns/op | 20,937 ns/op (~72×) |

スタック深さは O(k) → O(1)。

## 変更

- `stdlib/list/list.gom`: `fn concat` を `@extern` 化、`concat_go` 削除
- `stdlib/list/impl.go`: `Concat` — `n += len(xs)` で総長を先取りして `make`、内側リストを順に append
- `stdlib/list/list.go` / `list_test.go`: `gom test stdlib/list` で再生成（生成物）
