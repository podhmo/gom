# Wave 21 orchestration prompt drafts

State as of 2026-11-18: Wave 20 all merged into main via #51
(lane 1, direct) + #55 (lanes 2-4 — chained PRs whose bases
stayed alive landed on their base branches; #55 pulled the
chain tip into main). This file drafts the prompts for the
next wave, following `2026-11-18-wave20-prompts.md`. Always
append the "Common instructions" block to each prompt.

## Execution model (unchanged from wave 19/20)

Quota constraints: **no child sessions** — one Devin session
executes every lane sequentially. Review is delegated to Devin
Review on each PR. podhmo merges.

- **One lane = one branch = one PR**, chained on the previous
  lane's branch while earlier PRs are unmerged. When merging a
  chained PR whose base is still an open lane branch, the base
  branch must be deleted or retargeted first — merging without
  retargeting lands the change on the base branch, not main
  (wave20 lesson, fixed via #55).
- **Ledger edits ride in the lane's own PR** (TODO / gaps /
  open-questions check-offs for that lane's work only).
- **LSP verification: no Emacs/Eglot** — in-process JSON-RPC
  tests asserting publishDiagnostics payloads are sufficient
  (podhmo ruling, wave-15).
- **Sequential order below.**

## Prompt-drafting rules (podhmo, 2026-11-18)

- **実行時の分岐をなくす** — prompts 内に「計測の結果なら (a)、
  そうでなければ (b)」型の実行時採択を残さない。採択が要る点は
  prompts 作成時点で podhmo に聞いて確定し、lane 本文は確定済みの
  形だけを書く（実装中の細かい設計決定は decision (draft) →
  検証の通常手順で記録する — それは分岐ではなく記録手順）。
- **LSP 由来の項目に専用 lane を割かない** — 1 並列の実行では
  LSP 系の独立 lane はコストが見合わない。LSP 作業は他 lane の
  直列サブタスクとして畳むか shelf に置く。
- **hygiene lane は恒例ではない** — これまでの「lane 4 = hygiene」
  は誤読の固定化。wave 締めの作業は lane ではなく、全 lane 完了
  （マージ）後にやる独立作業（もともと並列実行時代に
  オーケストレーターが担っていたもの）。台帳掃除・check-off
  sweep はその締め作業の中でやる。

## Orchestration notes (Wave 20 lessons)

- **staging に実ディレクトリの残滓を混ぜると tool が嘘をつく** —
  LSP の staged build が `*_test.go` sibling を拾い、再生成されない
  stale 生成テストで `go vet`（テストも型検査する）が落ちた。
  逆に生成 companion を除きすぎると `@test` 定義を呼ぶ手書き
  テストが undefined になる — staging には fresh な TestSource
  も含める。tool を足すとき「その tool が何を読むか」を
  staging のコピーポリシーと突き合わせる（wave20 lane3 review）。
- **宣言 sig と instantiated sig を取り違えない** — flat arity の
  位置決めは宣言された arrow 連鎖に対して行う。instantiated 型
  から逆算するとジェネリック代入で arity が広がって誤った
  adapter を emit する — `core.FuncRef.FlatParams` に lower が
  宣言 sig 由来の arity を記録し、codegen はそれを読む
  （wave20 #55 review BUG_0002）。
- **ledger は PR ごとに腐る** — 「残件: X はない」行は
  実装 PR の中で消す（wave19 から継続）。

## Lanes

### 1. core validator の `CtorApp`/`CtorRef` TypeArgs 検査 — `devin/wave21-ctor-targs`

```
[impl] implementation-gaps open gap（wave20 lane4 で
problems/2026-11-02-ctor-typeargs-unvalidated から台帳化）:
`internal/core/validate.go` が CtorApp/CtorRef の `TypeArgs`
を検査しない。**確定済み（podhmo, wave21 prompts 作成時）:
TypeArgs は optional-but-authoritative** — 存在するときは
`Typ` の `Con.Args` と完全一致を要求し、不一致・ADT 型
パラメータ arity との個数不一致は violation にする
（problems 文書の推奨案。Typ を正とする (b) は future work
として open-questions に残す）。codegen の `ctorTArgs`
フォールバックは TypeArgs 欠損時の経路として残し、validator
が一致を保証した上で使う。decision として記録し、validator
実装と spec/design の整合を取ってから状態を更新する。
Ownership: internal/core (validate.go), internal/lower
(TypeArgs を emit する側), internal/codegen (ctorTArgs),
docs/decisions, docs/implementation-gaps (gap 閉じ),
docs/open-questions (b を future work に記録).
```

### 2. `Result.to_option` / `Option.to_list` — stdlib layering — `devin/wave21-result-option`

```
[impl] implementation-gaps open gap（425 行目）+ shelf:
`Result.to_option` / `Option.to_list` が存在しない — Go の
循環参照禁止で stdlib の module 構成上置けない。**確定済み
（podhmo, wave21 prompts 作成時）: (a) list/option/result を
個別モジュール化** — `stdlib/list`, `stdlib/option`,
`stdlib/result` を別 package に分割し、option → result の
一方向依存で `to_option`/`to_list` を正当に配置する。分割
構成・`.gomi` の見え方・`internal/stdlib` registry の配線を
decision (draft) で記録し、spec/design を更新してから実装の
検証で状態を更新する。
Ownership: stdlib/, internal/stdlib, docs/spec (stdlib 節),
docs/decisions, docs/implementation-gaps (gap 閉じ).
```

### 3. design §5-6 独立 dir の並列コンパイル — `devin/wave21-parallel-build`

```
[impl] implementation-gaps open gap（wave20 lane3 で
「小さくない」と評価・要件記録済み）: dir グループの並列
コンパイル。**確定済み（podhmo, wave21 prompts 作成時）:
(a) dep グラフから topological にワーカー配置 + Loader 全体の
同期** — 依存先 .gomi の再生成が依存側の読み取りより先に
終わるよう dep 順にスケジュールし、`gomimport.Loader`
（`l.pkgs`/member table/`go/importer` fset）は共有ロックで
直列化する。`bc.Hit`/`depSink`/`bc.note`/`results[i]` の
dir 分割は素直に並列化できる。
Ownership: cmd/gom/cli.go (runBuild), internal/build,
internal/gomimport, docs/design §5-6,
docs/implementation-gaps (gap 閉じ).
```

### 4. `List` 由来の読み取り専用 View 型 — `devin/wave21-list-view`

```
[impl] problems/2026-11-04-shared-slice-view-type の保留
分: `List`（`type List[A] []A`、持ち主 Gom・不変）から作る
読み取り専用の部分ビュー型を足す。元が不変なので MoonBit の
`StringView` と同じ安全性 — 部分取り出し・添字アクセスが
コピーなし O(1)。**対象は List 由来のみ** — Go 由来の共有
View（別名で内容が変わり得るもの）は、既決定どおり
`Go.Slice` の読み取り専用操作で足りるので今回は足さない
（decision 2026-11-04-goconv-list-slice-copies）。
型名・操作面（`view`/`slice`/`get`/`length` 等の名前と
builtin 登録形）と W0701/W0702 mutable-sharing lint での扱い
（View は読み取り専用なので可変判定リストには入れない方向で
確認）は decision (draft) として起こし、実装検証で状態を
更新する。
Ownership: internal/check (builtin 型), internal/stdlib,
rt/, docs/spec (型・操作節), docs/decisions,
docs/problems (元ファイルの状態更新).
```

## Post-lane work（全 lane マージ後 — 締め作業）

lane ではなく、並列実行時代にオーケストレーターが担っていた
wave の締め作業。単独実行でも lane 完了後にまとめてやる:

- `docs/sketch/2026-11-*-wave21-round.md` の round 記録
- `skills/gom-agent-guide` の同期（View 型・並列ビルド・
  stdlib 分割のユーザー可視分）
- wave22 prompts 草案（採択が要る点は作成時点で podhmo に
  聞く — prompts-drafting rules）
- 台帳掃除（直列サブタスクとして必要分だけ）: problems/
  古い残件の台帳化判定、implementation-gaps/TODO/
  open-questions の check-off 漏れ sweep、wave20 review
  起因パターン（staging stale artifact・宣言 vs
  instantiated sig・ledger 腐り行）の grep

## Lane candidates left on the shelf

採用しなかった候補（順位づけは podhmo 次第で後 wave に送れる）:

- **LSP shared importer + member table 共有** — wave19 で
  経路設計済み（長寿命 `goimport.Loader` + exportcache
  fingerprint invalidation）。LSP 由来は専用 lane にしない
  新ルールにより、採用するなら他 lane の直列サブタスクとして
  畳む。
- **LSP completion/definition/references** — 同上。大規模なの
  で専用 wave 向き（LSP 系をまとめて扱う wave があれば）。
- **flat パラメータの部分適用 timing 差** — `f a` の発火が
  residual 到着まで遅延する残存差（decision + spec §1.3 に
  記録済み）。eval 側に residual partial を移植して divergence
  を消すかは将来の検討。
- **`TypeArgs` の (b) — Typ を canonical とする** — lane 1 で
  (a) optional-but-authoritative を採択。(b) へ移るかは
  future work として open-questions に記録。
