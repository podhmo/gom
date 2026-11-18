# Wave 23 orchestration prompt drafts

State as of 2026-11-19: wave22 の4 lane（#61-#64）を PR 済み・
チェーンでマージ待ち。このファイルは wave23 の prompts 草案。
podhmo 方針: **wave23 は全 lane 調査専用**とし、結果を見て
wave24 で採否・実装方針を決める。実行モデルは wave22 と同じ
（単独実行・Devin Review レビュー・podhmo がマージ）。

保留中の podhmo 判定（wave23 の lane にはしない）:
- TypeArgs (b) 採否 — wave22 lane4 の棚卸し済み
  （[sketch](2026-11-19-ctor-targs-b-inventory.md)）。採用なら
  実装 lane は wave24 以降の候補。
- LSP 全削除の意向 — lane2 の棚卸し結果を受けて決める。

## Lanes

### 1. Go struct 構築機構の比較設計 — `devin/wave23-struct-construction`（調査）

A テーマ（examples/gist-cli の host パッケージ根絶）の主 lane。
`host.NewClient` が残る唯一の理由は `http.Client{Timeout: ...,
CheckRedirect: ...}` の構築 — 他のブロッカー（named int 算術・
Go.func2 経路）は解消済み。構築機構の案を設計して比較する。

```
[investigate] Go struct 値の構築機構の案を設計・比較する
（decision 2026-11-17-struct-construction の再検討 — 当時の
採用 (c) factory-extern の前提だった Duration 算術・func-typed
field 供給の障害は解消済み）。案:
(a) named Go struct 型に限定したコンポジットリテラル
    `http.Client{Timeout: ..., CheckRedirect: ...}` —
    構文・型検査・lower/codegen の新面。unexported/embedded/
    partial init の規則設計が要る。
(b) フィールド書き込み + ゼロ値構築 —
    `client.Timeout = ...` 代入面（addressability 規則）と
    ゼロ値の綴り（new 相当）の2機能。
(c') `@extern fn` impl.go 機構のユーザーモジュール開放 —
    Go コードの置き場所が変わるだけで host 消滅とは言いにくい。
比較は機構列挙ではなく、先に評価軸を立ててから当てはめる:
- どういう記述が嬉しいか — `NewClient` 相当を Gom で
  書くときのゴールの記述例を先に置く（record literal
  との見た目の距離も含めて）。
- その時の制約は何か — partial init・unexported フィールド・
  embedded・addressability・生成コードの Go 慣習。
- 何を守りたいか — 値の不変性・Go 相互運用の一貫性・
  spec の面の小ささ・「読めてデバッグできる生成物」原則。
その上で各案の言語面（spec 変更の大きさ）、実装規模、
生成コードの読み味を比較表にまとめ、推奨案を draft
decision として書く。採否は podhmo（wave24）。
Ownership: docs/sketch, docs/decisions（draft のみ）,
docs/spec.md・docs/design.md（変更はしない — 採否後）.
```

### 2. LSP 全削除の影響棚卸し — `devin/wave23-lsp-removal`（調査）

podhmo の意向メモ「LSP の機能全部消しても良い気がする」を受けた
B テーマ（削減）の準備調査。削除するなら何が吹き飛ぶかを全部
洗い出す。

```
[investigate] `gom lsp` 全削除の影響範囲を棚卸しする。
対象: cmd/gom の lsp サブコマンド・internal/lsp 全体・
lsp 用テスト・docs（spec/design/decisions/open-questions の
LSP 項）・TODO.md の LSP 拡張項。あわせて「LSP 以外が
LSP 機構に依存していないか」を確認する（anchor 表・
gotool position inverse・`gom check -go` 等の共有部品が
lsp 以外の経路からも使われているなら残す面を分離）。
削除順序と Supersede 対象の decisions 一覧を sketch にまとめる。
削除可否の決定は podhmo。
Ownership: cmd/gom, internal/lsp, docs/sketch,
docs/open-questions・TODO.md（調査結果の記録のみ）.
```

### 3. 型クラス vs 明示的辞書渡し — `devin/wave23-typeclass-vs-dict`（調査）

D テーマ（v2 設計）の先取り1件。open-questions 最大の未決項で、
v1 の stdlib 設計（`Array.sort` 等の比較器渡し）にも絡む。

```
[investigate] open-questions「型クラスの代替」の設計空間を
棚卸しする。現状の等値・比較（生成比較器経路・View 等値）を
起点に、(1) v1 相当の明示的辞書渡し（`Eq a` record を引数で
渡す）(2) 型クラス風の暗黙辞書 (3) 現状維持（個別引数）の
各案で、言語面・推論・.gomi 表現・生成コードへの影響を
整理する。v1 の記述可能範囲（HOF の境界・deriving 相当の
需要）のユースケースを examples から拾って判定材料にする。
sketch に設計空間と推奨の置き場所を書く。採否は podhmo。
Ownership: docs/sketch, docs/decisions（draft のみ）,
docs/open-questions（該当項の更新）.
```

### 4. cons 非適用形の蓄積 quadratic — `devin/wave23-noncons-accum`（調査）

wave22 からの持ち越し。
`docs/problems/2026-11-05-stdlib-cons-built-combinators-quadratic.md`
残件: cons-recursion 特殊化で match-cons 蓄積は buffer loop 化済み
だが `concat` 系の非 cons 形は残ると記録。

```
[investigate] cons-recursion 特殊化（decision
2026-11-18-cons-recursion-specialization）が救えない蓄積形を
棚卸し: パターン別（連鎖 append、fold 内 concat、相互再帰の
蓄積）にどこまで残っているかを実測つきで整理し、救済案
（hybrid repr、より広い特殊化、Array 指向の idiom 誘導）の
費用対効果を sketch にまとめる。実装判断は podhmo。
Ownership: internal/lower (consrec), stdlib/list,
docs/sketch, docs/problems（該当項の更新）.
```

## Post-lane work（lane 全完了後 — 締め作業）

```
[close-out] wave23 round 記録（docs/sketch/*-wave23-round.md）、
gom-agent-guide 同期（調査結果でユーザー向け記述が変わる分のみ）、
wave24 prompts 草案（lane1-3 の採否結果を反映）、台帳
（open-questions / implementation-gaps / TODO.md）の掃除を
最終 lane のブランチに載せる。
```
