# P2-4b 明示的 `try` を含む `|?>` チェーンの分割融合の検証

- 日付: 2026-11-03
- Go: 1.27.0 linux/amd64
- 決定: [pipe-try-segmented-fusion](../decisions/2026-11-03-pipe-try-segmented-fusion.md)
- 先行: [result-pipe-local-boundary](../decisions/2026-11-03-result-pipe-local-boundary.md) / [sketch](2026-11-03-result-pipe-local-boundary.md)

## 発見と実装

P2-4a の保守的経路（`pipeTryValue`）は `try` を含むチェーンを「inflow の Ok/Err 分岐 → ステップ適用 → ラップ検査用クロージャ」をレベルごとに組む形で、意味は正しいが中間 `Result` とクロージャがレベル数だけ残った。

新しい経路（`pipeTryMixed` / `pipeRest` / `pipeSegment` / `pipeFeed`）は、チェーンを `hasUnguardedTry` で段分けする。`check.Lambda` だけが `try` の遮蔽物なので、walk はラムダ本体に降りない（`check.WalkExpr` の木と同じ形、Lambda で打ち切り）。`go`・`for`・`match` アームは境界ではないので降りる — そこに `try` があれば unguarded で、チェック済み AST では `go` 越えの try は E0702 で既に拒否されているが、手組み AST でも保守的側に倒れる。

- guarded 区間: 一つの局所境界クロージャに融合（`EscapeCall`/`EscapeResult`、区間内は payload のみ、末尾で一度だけ `Ok`）
- unguarded ステップ: 適用をユーザー関数側の列で評価 → `try` の `EscapeResult.To` はユーザー関数のまま
- 区間境界: `pipeFeed` の決定木が Ok payload を渡し、Err はチェーン型で再構築（残りをスキップ）、invalid タグは DPanic
- GoError ラップ: unguarded ステップの失敗は次セグメント入口の `EscapeResult{Wrap: <ステップ名>}` でラップ — ラップは失敗したステップ名のまま

## 回帰テスト

- `TestEvalMatchesCompiled/pipe_try_segments`（新規、16 ケース）: guarded-unguarded-guarded / 先頭・末尾の unguarded / 連続 unguarded / ラムダ遮蔽 try / inflow 内 try / Err inflow でステップ引数の try 自体を実行しないことを、評価器と生成 Go の同一出力で確認（`mid`/`first`/`last`/`two`/`lam`/`inflow`/`skip`）。
- `TestEvalMatchesCompiled/pipe_explicit_try_boundary`（既存）: そのまま通過 — ユーザー関数境界、Err 入力での skip、ラムダ内 try のラムダ境界。
- `TestPipeTryMixed` / `TestPipeTryShieldedTryFuses`（新規、lower 構造テスト）: セグメント内の `EscapeResult.To` がセグメント Func、unguarded try の `To` がユーザー関数、ラムダ内 try の `To` がラムダ Func、各 Wrap 名（`"f"`/`"step"`/`"h"`）、feed の Err アーム再構築と DPanic default を検査。
- `TestPipeTry` / `TestPipeTryBoundMeta`（既存）: 融合経路の 3-let 形は不変。

## 生成コード検査（`/tmp` 上の実験ファイル → `gom build` → `go run`）

`n |?> keep |?> bump (try r) |?> keep`（GoError チェーン）で:

- 先頭セグメントの `strconv.Atoi` は `(v, err)` 直受けの `EscapeCall`（`rt.ResultFromGo` なし、`int64` 変換付き）
- `try r` はユーザー関数本体の `switch` で `return result.Err(r.Error)` — ラップなしで元エラーのまま脱出
- 後続 `bump`/`keep` は次セグメント内で `fmt.Errorf("bump: %w", ...)` / `"keep: %w"` のラップ付き検査
- 出力: `ok 11 / err a1 / ok 22 / err i2 / ok 35 / err strconv.Atoi: strconv.Atoi: parsing "bad": invalid syntax / err g5` — try 脱出は非ラップ、パイプ失敗は 1 度だけラップ、Err は残りをスキップ

match アーム内の `try`（`bump (match r { ... Err e -> try (Err e) })`）も unguarded 扱いになり、ユーザー関数から脱出した。

## golden 差分

`testdata/golden/result_pipe.golden.go` のみ（非 GoError チェーンの `explicit`）。旧: 末尾の `|?>` レベルを包む IIFE。新: 同じ位置に素の `switch tmp14.Tag()`（Ok→payload、Err→再構築、default→`rt.InvalidVariant`）。セグメント境界で不要になったクロージャの除去であり、意味は同じ。panic メッセージの型名は `"Result"`（決定木の DPanic 規約、`result.Result` ではなく bare 名 — 既存の match 由来 DPanic と同じ表記）。

## 残課題

- セグメント境界では `Ok` を一度構築して直後の決定木が分解する。unguarded try を挟むチェーンではこの物質化が残る（旧経路は全レベルで物質化していたので単純に減少）。
- `pipeFeed` の Err アームは `Err e` を再構築する — 同一値の再タグ付けだが、ADT としては新しい値。エラー値の同一性（`errors.Is`/`As`、元エラー）は payload をそのまま運ぶので保存される（`TestPipeGoErrorChain` が Go 側から検証済み）。
