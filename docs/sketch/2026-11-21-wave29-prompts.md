# Wave 29 orchestration prompt drafts

State as of 2026-11-21: wave28 の 5 lane（#98–#102、named func /
Array.make init / 予約語 E0106 / shared-Go-Ptr lint / type alias
調査）は全て PR 化済み、Devin Review 反復中。podhmo マージ待ち。

このファイルは wave29 の prompts 草案。テーマは record — wave28
lane E の調査で「Go の new type として使える nominal 面は
single-field record が担う」ことが確認されたので、その経路の
完成度を上げる lane + 残件の record 系ギャップを固める。
lane 順は下からチェーンする前提。

## Lanes

### A. single-field record を Go の defined type として emit — `devin/wave29-newtype-emit` — **採択済み**（2026-11-21 チャット: (a)、範囲が大きければ切替え可）

```
[impl] `type UserId = {value: Int}` は今日
`type UserId struct { Value int64 }` を emit する。Go の newtype
慣用形 `type UserId int64` に変える — 単一フィールド record は
Go defined type `type X F` として emit する。
`.x` アクセス・`with` 更新・`==`/Show/boundary repr・golden を
全部見直す。ABI 変更なので v1 前のうちにやる。
**切替え条項（podhmo 了承済み）**: 調査で変更範囲が大きすぎると
判明した場合は、newtype 宣言の別スペル案か現状維持への切替えを
podhmo に相談してから進める。
spec §4/§8、design 2.1、golden に影響。
Ownership: internal/codegen, internal/lower, internal/check,
docs/spec.md, docs/design.md, testdata/golden, decision.
```

### B. record 構築の pun — `devin/wave29-record-pun` — **採択済み**（2026-11-21 チャット: (a) 両方）

```
[impl] record literal `{x = x, y = y}` は pun を認めない —
`{x, y}` は E0101（検証済み）。`{x, y}` = `{x = x, y = y}` の
pun を literal と pattern の両方に入れる（expr では field と
同名の変数、pattern では同名束縛 — Go/MoonBit/TS と同じ形）。
parser の record-literal/record-pattern 両経路に同じ規則を置く。
spec の式一覧と pattern 節に記載、fixture は ok/err 両方。
Ownership: internal/syntax, docs/spec.md, testdata/spec.
```

### C. record からラベル引数への spread `f ~..opts` — `devin/wave29-record-spread-call` — **採択済み**（2026-11-21 チャット: (a)）

```
[impl] open-questions の「record→ラベル引数の spread」。
labeled-args は defaults-record パターン（`{base with x = v}` +
`f ~x:`）で代替する方針に収束済みだが、record 値をそのまま
ラベル引数へ展開する呼び出し面がない。`f ~..opts` —
expr の record fields を label 名で引数へ対応づける spread を
実装する（型チェックはフィールド名照合。位置引数との混在
ルールを決める — 実装時 decision 記録）。
grammar と apply の conform 判定の両方に触るので
tree-sitter 再生成も（`npm exec --yes --package=
tree-sitter-cli@0.25.10 -- tree-sitter generate`、
0.27/0.26 は GLIBC 不足でこの VM では動かない）。
Ownership: internal/syntax, internal/check, docs/spec.md,
decision.
```

### D. Gom record が Go interface を実装する宣言 — `devin/wave29-go-iface-impl` — **採択済み**（2026-11-21 チャット: 設計調査のみ）

```
[investigate] open-questions 筆頭: Gom の型が Go の interface
を実装できるようにするか。wave25 lane D の調査で record 型は
実現可能と判明（host ファイルでは今日でも書ける —
docs/sketch/2026-11-19-wave25-go-iface-impl.md）。
この lane は**実装しない** — 「採用するならこの面」の設計を
decision まで固める調査に留める: record がメソッド集合を持つ
宣言面（`type R = {..} impl io.Reader` 系のスペル or
attribute）、method value の呼び出し規約、.gomi での見せ方、
extern 側からの要求（Go 側 param の interface に record を
渡したときの wrap）。lane A (a) が着地した世界線
（単一フィールド record = defined type）での設計として書く。
Ownership: docs/decisions, docs/sketch, docs/open-questions,
docs/spec.md（設計確定分のみ）。
```

### E. `type alias X = T` の追加 — `devin/wave29-type-alias` — **採択済み**（2026-11-21 チャット）

```
[impl] wave28 lane E の defer 推薦を podhmo 判定で覆して採用
（decision 2026-11-21-type-alias-adopt が Supersedes）。
`type alias X = T` を新構文として追加し、`type X = …` は
単一 decl / record / sum のいずれでも「新しい型の導入」で
一貫させる。alias は透明な別名（Go の `type X = T`）。
下流は揃っている（`AliasBody`/`resolveAlias`/`kindAlias`/
codegen `talias` = `type Name[A] = T` emit）— 必要なのは
`parseTypeBody` からの到達経路と `alias` の字句面（予約語化か
`type` 直後の contextual keyword か — 既存コードの `alias`
識別子への影響を測って実装時 decision 記録）。spec §8.3 の
「`type T = any`」記述を `type alias` に合わせて修正、
fixture ok/err、grammar.ebnf を触るなら tree-sitter regen
（0.25.10 固定）。`type X = T` が variant に落ちる罠は残る —
lint ガードの要否は lane 中に評価して decision に書く。
Ownership: internal/syntax, internal/resolve, internal/check,
docs/spec.md, testdata/spec, decision.
```

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- wave28 の 5 PR（#98–#102）は base チェーンで未マージ。
  wave29 lane 開始時は最終 lane の head
  （`devin/wave28-type-alias`）から分岐するか、podhmo の
  マージ後に main から分岐する。
- wave の回し方そのものは `docs/ja/wave-process.md` に規範
  文書として切り出した（prompts 間の申し送り継承だと
  wave24 で項目が脱落した実績があるため）。以下はその要点:
- prompts-drafting ルール（wave21 制定、wave24 以降の prompts
  から本文が脱落していたので復元）: 採択が要る点は prompts
  作成時点で podhmo に聞いて確定し、lane 本文に「計測結果なら
  (a)、そうでなければ (b)」型の実行時採択を残さない。実装中の
  細かい設計決定は decision (draft) → 検証の通常手順で記録する。
- wave の締め作業（wave21–23 に明示、wave24 以降脱落）: lane
  ではなく全 lane 完了後にやる独立作業 — round 記録、
  `skills/gom-agent-guide` の同期（ユーザー可視分）、次 wave
  prompts 草案、台帳（open-questions / implementation-gaps /
  TODO.md）の掃除。
- lane C/E が grammar を触る場合: tree-sitter-cli は
  0.27.1/0.26.3 がこの VM では GLIBC 不足で動かない。
  `npm exec --yes --package=tree-sitter-cli@0.25.10 -- tree-sitter
  generate` で再生成（ABI 15 は不変、wave27 round）。
- wave28 ラウンド記録: `2026-11-21-wave28-round.md` —
  `lower.Env` の任意インターフェースは stdlib の `typeOracle`
  ラップで落ちるので第一級フックが要る、
  `goimport.Loader` のロック済み/無ロックペア（member ロード中は
  無ロック側でないと自己デッドロック）、構文回復の parity は
  baseline worktree との decl-count 比較で検証、を申し送り。
- decision ファイルの書式: 冒頭に `- 状態:`・`- 日付:` の
  bullet が必須（`TestDecisionsReadmeInSync`）。README 再生成は
  `go generate ./internal/tool/decisionsdoc`。
- spec fixture の再生: `go test ./internal/build -update -run
  TestSpec`。golden の再生: 同コマンド -run TestGolden。
  gocall は `cmd/gom/gocall_test.go` のパッケージ列挙に登録。
- gomtest のディレクトリ case は bare `expected.json` /
  `golden.go` / `case.json` のどれかが無いと各ファイル個別
  ケースになる — 複数ファイルを 1 ケースにするなら
  `case.json {}` を置く（wave27 round）。
- scratch チェック環境: `/tmp/gomctx2`（go.mod `module tmp2` +
  `replace github.com/podhmo/gom => ~/repos/gom`）。
  `.gom` は `package` 節から始める。`gom build -o dir/` は
  dir を `/` で終わらせる。
- コミットメッセージにバッククォート/`$()` を入れない —
  `-m` を複数回に分けて書く（shell が喰う）。
- 採択されなかった候補の sketch/experiments は記録として残す
  （decision から却下理由つきで参照、wave24 round）。
- SC チェーン（#93/#94/#96）と他セッションの PR が残っている
  場合は lane の file ownership が被らないよう確認。
