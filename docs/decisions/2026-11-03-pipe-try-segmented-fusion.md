# 明示的 `try` を含む `|?>` チェーンの分割融合

- 状態: 採用（評価器対生成 Go の比較・生成コード検査で確認。[検証レポート](../sketch/2026-11-03-pipe-try-segmented-fusion.md)）
- 日付: 2026-11-03
- Supersedes: [result-pipe-local-boundary](2026-11-03-result-pipe-local-boundary.md) のうち「明示的な `try` を含むパイプは決定木の Ok/Err 分岐として lower する」という方針

## 背景

P2-4a は `try` を含まない `|?>` チェーンを一つの局所境界クロージャに融合したが、`try` を含むチェーンは try の脱出先（[try-boundary](2026-11-01-try-boundary.md): 最も内側の関数境界）を守るため、レベルごとに Ok/Err 分岐とラップ検査用クロージャを作る保守的経路のままだった（TODO P2-4b）。その経路ではステップが増えるごとに中間 `Result` 値と即時適用クロージャが比例して残った。

## 決定

- `try` の脱出先の決まり方は変えない。`hasUnguardedTry` はラムダの中に降りない walk で「ユーザー関数を脱出する try」（unguarded try）を検出する。ラムダ内の `try` はラムダ自身が境界なので、どこに移動しても宛先が変わらず融合を妨げない。`go { ... }` は境界ではないので遮蔽しない
- チェーンを unguarded try を含まないステップの連続区間（セグメント）に分ける。各区間は従来どおり一つの局所境界クロージャに融合し、Go の `(T,error)` 呼び出しは `EscapeCall`、任意の `Result` は `EscapeResult` で展開し、区間内では成功 payload のみを渡して中間 `Result` を作らない
- unguarded try を含むステップは、その適用をユーザー関数側の列（トップレベルの列、またはセグメント間の match アームの lets）で評価し、`try` の `EscapeResult` の宛先をユーザー関数のままにする
- 区間境界では `Result` を Ok/Err の決定木で検査して payload を次に渡し、失敗は残りのステップをすべてスキップする。GoError チェーンで `%w` ラップが要るときは、その検査を次セグメント入口の `EscapeResult`（Wrap=失敗したステップ名）にまとめる。非 GoError ではラップ不要なので裸の決定木でよい
- codegen に意味の判断は追加しない。新しい Core IR ノードも追加しない（境界情報は既存の `EscapeResult`/`EscapeCall` の `To` で明示する）

## 残るもの

- セグメント出口では `Ok` を一度構築し直し、次の決定木がそれを分解する。旧経路はレベルごとに物質化していたので単純に減るが、unguarded try を挟むチェーンではセグメント数だけの物質化が残る。さらに畳めるかは意味が変わらない範囲で別途検討する
- `try` の有無は `check` 済み AST の構造 walk で決める。`check.Lambda` だけが遮蔽物なので、`test` ブロックのような他の関数境界は式レベルに現れず、この walk で扱う対象に入らない
