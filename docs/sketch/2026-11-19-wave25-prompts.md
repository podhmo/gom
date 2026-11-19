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

### B. composite literal (a) — リテラル構文か、推論強化で代替可能か — `devin/wave25-composite-literal`

```
[investigate] wave23 lane1 で先延ばしされた (a) Go struct の
composite literal（`http.Client{Timeout: ...}` 形）の要否を、
「初期化ブロック + Ruby の tap 相当」で代替できるかの観点で
再判定する。wave24 lane B で `Ptr.new ()` + 逐一代入、自由
ブロックで囲む形（gist-cli bce91d6）は動く。残る回りくどさは
型記述の重複 — 現状は `let c : Ptr T` をブロック内で再宣言
する必要がある。

代替案 `tap`: `fn tap (f: a -> Unit) (x: a) -> a = { f x; x }`
（pipe は値を最終引数に置くため引数順に注意）で
`Ptr.new () |> tap init` は型検査を通る — ライブラリ関数で
書ける。しかし 2026-11-19 実測で 3 つの推論の壁:
  - `fn` のパラメータにアノテーションが書けない
    （`fn (c: T)` は E0101）— 型を書く場所が束ねる側の
    `let init : Ptr http.Client -> Unit` に移るだけ
  - `let` アノテーション付き `fn` 本体の内部では入れ子
    lambda（`Go.func2 (fn _req _via -> …)`）が E0407 —
    ブロック直下では通る期待型伝播が `fn` 本体で弱い
    （implementation-gaps に記録）
  - 上記が直れば `Ptr.new () |> tap (fn c -> {…})` で
    型記述ゼロの初期化が可能 — その場合 composite literal
    は不要という結論になり得る

判断材料: 推論強化（lambda パラメータへの期待型伝播・
アノテーション構文）のコスト vs literal 構文のコスト、
ネスト struct・多数フィールド・`[]T` 要素での記述差、
value restriction/可変性との整合。実装するなら
syntax/check/codegen + spec 8.3 節更新。
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
