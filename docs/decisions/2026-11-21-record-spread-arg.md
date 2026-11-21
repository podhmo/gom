# record の spread 引数 `f ~..opts`

- 状態: 採用
- 日付: 2026-11-21
- 関連: spec §2.9、decisions/2026-11-06-label-args-into-returned-functions.md

## 決定

`f ~..e` を record spread とする: `e` の型は record でなければならず、宣言された各フィールドが名前で `~name` パラメータに対応づく（`move 0 ~..opts` ≡ `move 0 ~dx: opts.dx ~dy: opts.dy`）。

## 位置引数との混在ルール

- 位置引数はこれまでどおり位置パラメータだけを埋める（宣言順、使用済み・ラベル付きスロットを飛ばす）。
- spread のフィールドはラベル付きパラメータだけを埋める — `paramByLabel` と同じ照合で、位置パラメータと同名のフィールドは対応しない（E0406）。
- 個別の `~label:` との併用も可 — 同じスロットが二度埋まれば E0405。
- spread の綴り `~..` は `~` + `..` の 2 トークン。値は単純式 1 つ（`~label:` と同じ規則）で、`~..mk ()` は `~..mk` と別引数 `()` に切れる — 複合式は `~..(mk ())` と括る。
- record 以外の値（List、タプル、Int 等）は E0418。callee の型が未解決（meta）または flat パラメータが 0 個なら E0408 を 1 回だけ報告する（フィールドごとには繰り返さない）。

## 実装の要点

- `syntax.Arg.Spread` を追加し、check の `spreadRecordArgs` がフィールドごとに `MemberExpr`（共有する `Recv`）を持つラベル引数を生成する。resolve/lower/codegen/eval に新しいノードは要らない。
- 展開された `MemberExpr` は同じ checked `Recv` を共有するので、lower の `recvShare` が最初の使用位置で一度だけ束縛する — record の効果は 1 回だけ評価される（`move 0 ~..(mk ())` → `tmp := mk(); move(0, tmp.Dx, tmp.Dy)`）。
- `~..` は Go の variadic spread `f(..xs)`（`..` 単独）とは別物 — Go 呼び出しでは引かない。

## 検証

- `testdata/spec/ok_record_spread.gom`（混在・部分適用・評価 1 回の generated code 確認）、`E0418_spread_not_record.gom`、`E0406_spread_unknown_label.gom`
- `tree-sitter-gom` corpus「record spread argument」
