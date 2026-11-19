# Wave 25 orchestration prompt drafts

State as of 2026-11-19: wave24 の 4 lane（#71–#74）を PR 済み・
チェーンでマージ待ち。実装 wave — 採択済み lane を全消化した。
このファイルは wave25 の prompts 草案。実行モデルは wave24 と
同じ前提（単独実行・Devin Review・podhmo がマージ）。
lane 順は下からチェーンする。

## 候補（採否は podhmo が lane ごとに決める）

### A. `Ref a` メンバー実装 — `devin/wave25-ref-members`

```
[impl] `Ref a` は型として存在するが `Ref.*` はまだ E0201
"exposes no members yet"（SKILL.md 記載どおり）。可変参照の
最小 API（Ref.new / get / set または set 相当の代入形）を
設計して実装。value restriction・可変性 lint（W0701/W0702 の
builtin リスト）は既に `Ref` を織り込み済み — 実装は stdlib
builtin モジュールか言語構文かの選択から。spec 7 章の Ref 項と
open-questions の可変性項に繋げる。
Ownership: internal/resolve (builtin scope), internal/stdlib か
syntax/check（代入形の場合）, stdlib/ か internal/,
testdata/spec, docs/spec.md, skills/gom-agent-guide.
```

### B. composite literal (a) の再検討 — `devin/wave25-composite-literal`

```
[investigate] wave23 lane1 で先延ばしされた (a) Go struct の
composite literal（`http.Client{Timeout: ...}` 形）。field
書き込み（wave24 lane B）で `Ptr.new ()` + 逐一代入は動くが、
ネスト struct・多数フィールド・`[]T` 要素では記述が回りくどい。
要否の判断材料: gist-cli 等の実用例での記述差、CheckRedirect
のような func field・embedded struct のカバー率、value
restriction/可変性との整合。実装するなら syntax/check/codegen
+ spec 8.3 節更新。
Ownership: docs 調査 →（採用なら）syntax/resolve/check/lower/
codegen, testdata/spec, testdata/golden, docs/spec.md.
```

### C. deriving 相当の調査 — `devin/wave25-deriving`

```
[investigate] ad-hoc-poly-dict-passing の残件: ユーザー型の
Eq/Ord/Show 辞書をコンパイラが合成する仕組みの要否。
wave24 lane D で `cmp` モジュールができたため、「手で合成する
コンストラクタ関数」は `pair_ord` 形で書けるが、レコードごとの
手作業は残る。コンパイラ mint（gomEqualN/gomShowN と同じ生成
経路）で型ごとの `Eq`/`Ord` を mint する方式と、.gomi/公開面・
多相フィールド（a の辞書をどう渡すか）の設計空間を整理。
Ownership: docs 調査 →（採用なら）internal/check/codegen,
stdlib/cmp, docs/decisions, open-questions 閉じ.
```

### D. Gom 型が Go interface を実装するかの調査 — `devin/wave25-go-iface-impl`

```
[investigate] open-questions Go 相互運用の残件: Gom の型が Go の
interface を実装できるようにするか（v1 では不可）。できるなら
宣言形（`impl io.Writer for T` 相当？ 属性？）と、method 値の
生成（レシーバが T か Ptr T か）、.gomi への現れ方を調査。
io.Writer/io.Reader/fmt.Stringer/error を最小セットに据えて
実現可能性を見る。
Ownership: docs 調査 →（採用なら）resolve/check/codegen,
docs/spec.md, docs/decisions.
```

## 申し送り

- CI は push-to-main / PR-to-main synchronize でしか走らない —
  チェーン PR は Devin Review のみ。マージは podhmo が
  retarget → `gh pr merge --merge` を下から順に。
- wave24 ラウンド記録: `2026-11-19-wave24-round.md` — select 遅延
  解決の裸メタ条件、FieldGet atom の評価順、decisionsdoc の
  Supersedes 1 行制約、golden harness の import 非対応を申し送り。
