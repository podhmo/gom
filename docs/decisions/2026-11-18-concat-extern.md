# concat を @extern buffer loop 化する

- 状態: 採用（wave19 lane 3 で実装。根拠: [sketch](../sketch/2026-11-18-concat-buffer-impl.md)）
- 日付: 2026-11-18
- Supersedes: [cons-buffer-array-extern-impl](2026-11-18-cons-buffer-array-extern-impl.md) のうち「`concat` は `.gom` のまま（変更理由がない）」の部分

## 決まったこと

- `stdlib/list` の `fn concat` を `@extern` 宣言に切り替え、hand-written `stdlib/list/impl.go` の `Concat[A]` が buffer loop で実装する
- 実装は総長を先に数えて `make(List[A], 0, n)` で 1 回だけ prealloc し、各 inner slice を `append(out, xs...)` でコピーする — O(1) スタック・O(n) 時間・1 alloc
- 呼び出し側・`.gomi`・挙動は無変更（`@extern` の規則は [cons-buffer-array-extern-impl](2026-11-18-cons-buffer-array-extern-impl.md) と同じ）

## 経緯

cons-buffer decision では「変更理由がない」として `concat` を `.gom` のまま残したが、その時点の `fn concat` 実装は `rev_append` 経由の連鎖で再帰深度 O(k)（k = リスト数）を持ち、wave19 lane 1 の cons-recursion 特殊化も適用できない形だった。深い入れ子リストで goroutine スタックを食う実害があったため、`@extern` 化して buffer loop に置き換えた。ベンチマーク: k=512 で 1.51ms → 21µs（~72×）。
