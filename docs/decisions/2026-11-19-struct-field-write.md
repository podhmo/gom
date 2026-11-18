# Go struct の構築は `Ptr` 経由の field 書き込みを採用（composite literal は先延ばし）

- 状態: adopted
- 日付: 2026-11-19
- Supersedes: 2026-11-17-struct-construction
- 問い: named Go struct を Gom からどう構築するか — wave23 lane1 の採否

## 決定

(b) **`Ptr T` 経由の field 書き込み + ゼロ値構築を採用する。**
`ptr.Field = v` という代入構文と addressability 規則が新規面で、
ゼロ値の綴り `Ptr.new ()` は実装済み。field 代入は `Ptr T`
（T は named Go struct）経由に限定する — record 値や `Ref` への
field 代入は開かず、「ハンドル型経由の可変性」（`Ptr.set` /
`Go.Map.set` と同じ）の既存枠内に収める。

(a) composite literal `pkg.T { f = v }` は**要検討として先延ばし**。
採る場合の論点（partial init・unexported/embedded の規則、record
literal との見分け、`Ptr T` 期待位置では `&T{...}` を emit しないと
`Ptr.make` のコピーで copylocks を踏む点）は
[sketch](../sketch/2026-11-19-wave23-struct-construction.md) に残す。

(c') `@extern fn` は採否に関係なく残す（実装済みの逃生路 —
`experiments/w23/struct-construction/cextern/` で動作確認済み）。

## 影響

- (b) の実装で tutorial の host パッケージ（`NewClient` factory）を
  撤去する — host 根絶の条件は field 代入だった
- spec 8.3 節「field への代入とコンポジットリテラルによる構築は
  提供しない」の更新は (b) の実装と一緒に行う（代入構文・
  addressability・Ptr 限定の規則として書く）
- 実装 lane は wave24 以降（TODO に登録）
