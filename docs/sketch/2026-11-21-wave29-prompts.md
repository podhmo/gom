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

### A. single-field record を Go の defined type として emit するか — `devin/wave29-newtype-emit`

```
[investigate → impl] `type UserId = {value: Int}` は今日
`type UserId struct { Value int64 }` を emit する。Go の newtype
慣用形は `type UserId int64` — 現状は struct ラップ 1 段ぶん
重く、boundary で Go 側へ `UserId` を渡しても struct のまま
見える。選択肢: (a) 単一フィールド record は Go defined type
`type X F` として emit（`.x` アクセス・`with` 更新・`==`/Show/
boundary repr・golden を全部見直す。ABI 変更なので v1 前のうちに
やるなら今）、(b) record 表現は struct のまま、newtype 宣言の
別スペル（`type X newtype T` 等）を足す — wave28 lane E の
「識別可能な綴り」がそのまま使える、(c) 現状維持。
struct→defined-type で `x : X` の値 repr・`==`、Go 呼出し境界、
`.gomi` 上の見え方が変わる範囲を測ってから採否を判断する。
spec §4/§8、design 2.1、golden に影響。
Ownership: internal/codegen, internal/lower, internal/check,
docs/spec.md, docs/design.md, testdata/golden, decision.
```

### B. record 構築の pun と 非ラベル付き構築 — `devin/wave29-record-pun`

```
[investigate → impl] record literal `{x = x, y = y}` は pun を
認めない — `{x, y}` は E0101（検証済み）。pattern 側の
destructure と non-labeled 構築の対称も調べる。選択肢:
(a) `{x, y}` = `{x = x, y = y}` の pun を literal と pattern の
両方に入れる（expr では field と同名の変数、pattern では同名
束縛 — Go/MoonBit/TS と同じ形）、(b) pun は pattern のみ
（literal 側は曖昧さを残さない）、(c) 何もしない。
parser の record-literal/record-pattern 両経路に同じ規則を置く。
採否を決めて spec の式一覧と pattern 節に記載、fixture は
ok/err 両方。Ownership: internal/syntax, docs/spec.md,
testdata/spec.
```

### C. record からラベル引数への spread `f ~..opts` — `devin/wave29-record-spread-call`

```
[investigate → impl] open-questions の「record→ラベル引数の
spread」。labeled-args は defaults-record パターン（
`{base with x = v}` + `f ~x:`）で代替する方針に収束済みだが、
record 値をそのまま `f ~..opts` のようにラベル引数へ展開する
呼び出し面がない。選択肢: (a) `~..expr` — expr の record
fields を label 名で引数へ対応づける（型チェックは
フィールド名照合。位置引数との混在ルールが要る）、
(b) 宣言位置の `~r` ではなく apply 位置で `f r` が record を
解く sugar（型が分かる位置だけ）、(c) 見送り — 呼び出し側が
`f ~x: r.x ~y: r.y` と書けば済む。
grammar と apply の conform 判定の両方に触るので、(a) なら
tree-sitter 再生成も。採否判断して進める。
Ownership: internal/syntax, internal/check, docs/spec.md,
decision.（grammar.ebnf を触る場合は tree-sitter regen —
`npm exec --yes --package=tree-sitter-cli@0.25.10 -- tree-sitter
generate`、0.27/0.26 は GLIBC 不足でこの VM では動かない）
```

### D. Gom record が Go interface を実装する宣言 — `devin/wave29-go-iface-impl`

```
[adopt → impl] open-questions 筆頭: Gom の型が Go の interface
を実装できるようにするか。wave25 lane D の調査で record 型は
実現可能と判明（host ファイルでは今日でも書ける —
docs/sketch/2026-11-19-wave25-go-iface-impl.md）。採用するなら:
record がメソッド集合を持つ宣言面（`type R = {..} impl io.Reader`
系のスペル or attribute）、method value の呼び出し規約、
.gomi での見せ方、extern 側からの要求（Go 側 param の
interface に record を渡したときの wrap）。podhmo の採否が
未決なので、この lane は「採用するならこの面」の設計を
decision まで固める調査に留めてもよい。
Ownership: internal/check, internal/codegen, docs/spec.md,
decision + sketch.
```

### E. `type X = T` の silent-variant 防止 — `devin/wave29-alias-misparse-guard`

```
[investigate → impl] wave28 lane E で確認した罠:
`type A = Int`・`type L = List Int` は UIdent 先頭の型式が
variant-list へ落ちて、単一バリアントの sum として**静かに**
受理される。alias 採用自体は defer としたが、誤読防止は別途
できる。選択肢: (a) バリアント名が既存の型名（builtin や
宣言済み型）に一致するとき lint/警告（E0101 系ではなく
「alias 意図か」の警告 — ただし `type Colour = Red` の正当
バリアントを誤爆させない閾値が要る）、(b) 単一バリアントで
`|` なしの decl に一律 note 診断、(c) `type X := T` 等の
識別可能な alias 綴りを採用して `type X = T` の UIdent 単独は
variant のまま（構文追加、wave28 lane E の将来案そのもの）、
(d) 何もしない — gaps 台帳登録は済んでいる。
(a)/(b) は lint 設計、(c) は grammar に触る。採否を判断して
進める。Ownership: internal/check/lint.go or internal/syntax,
docs/spec.md, testdata/spec, decision.
```

## Notes

- lane C/E が grammar を触る場合: tree-sitter-cli は
  0.27.1/0.26.3 がこの VM では GLIBC 不足で動かない。
  `npm exec --yes --package=tree-sitter-cli@0.25.10 -- tree-sitter
  generate` で再生成（ABI 15 は不変）。
- wave28 の 5 PR が base チェーンで未マージ。wave29 lane 開始時は
  最終 lane の head（`devin/wave28-type-alias`）から分岐するか、
  podhmo のマージ後に main から分岐する。
- SC チェーン（#93/#94/#96）と他セッションの PR が残っている
  場合は lane の file ownership が被らないよう確認。
