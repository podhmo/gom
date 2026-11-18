# Wave 21 orchestration prompt drafts

State as of 2026-11-18: Wave 20 lanes all up —
N-ary Go 結果 (#51), flat fn params (#52), `gom lsp -go` vet
包含 + §5-6 評価 (#53), hygiene sweep (#54). podhmo merges
(chained — bottom up). This file drafts the prompts for the
next wave, following `2026-11-18-wave20-prompts.md`. Always
append the "Common instructions" block to each prompt.

## Execution model (unchanged from wave 19/20)

Quota constraints: **no child sessions** — one Devin session
executes every lane sequentially. Review is delegated to Devin
Review on each PR. podhmo merges.

- **One lane = one branch = one PR**, chained on the previous
  lane's branch while earlier PRs are unmerged.
- **Ledger edits ride in the lane's own PR** (TODO / gaps /
  open-questions check-offs for that lane's work only).
- **LSP verification: no Emacs/Eglot** — in-process JSON-RPC
  tests asserting publishDiagnostics payloads are sufficient
  (podhmo ruling, wave-15).
- **Sequential order below.**

## Orchestration notes (Wave 20 lessons)

- **staging に実ディレクトリの残滓を混ぜると tool が嘘をつく** —
  LSP の staged build が `*_test.go` sibling を拾い、再生成されない
  stale 生成テストで `go vet`（テストも型検査する）が落ちた。
  tool を足すとき「その tool が何を読むか」を staging のコピー
  ポリシーと突き合わせる（wave20 lane3 review）。
- **宣言 sig と instantiated sig を取り違えない** — flat arity の
  位置決めは宣言された arrow 連鎖に対して行う。instantiated 型
  から逆算すると monomorphize 済みの arity にずれる
  （BUG_0002/0003 — `declParamTypes` が `l.declTypes` →
  `Env.FuncType` → raw `GoSig.Params` の順で宣言 sig を引く経路
  を新設した）。新しい lookup を足すときは production の
  `lower.Env` 配線サイト（`compileOptions` — CLI/LSP 共通）を
  全部辿る。
- **ledger は PR ごとに腐る** — 「残件: X はない」行は
  実装 PR の中で消す（wave19 から継続）。

## Lanes

### 1. core validator の `CtorApp`/`CtorRef` TypeArgs 検査 — `devin/wave21-ctor-targs`

```
[impl] implementation-gaps open gap（wave20 lane4 で
problems/2026-11-02-ctor-typeargs-unvalidated から台帳化）:
`internal/core/validate.go` が CtorApp/CtorRef の `TypeArgs`
を検査しない — ADT 型パラメータ arity との個数一致と `Typ`
との整合を検査し、不正形は violation にする。TypeArgs と
`Typ` の `Con.Args` が食い違うときどちらが正かの canonical
規則もここで確定（codegen の `ctorTArgs` フォールバックを
その規則に揃える）。先に「両方あるときの正」を decision
(draft) として起こし、validator の実装で検証してから状態を
更新する。
Ownership: internal/core (validate.go), internal/lower
(TypeArgs を emit する側), internal/codegen (ctorTArgs),
docs/decisions, docs/implementation-gaps (gap 閉じ).
```

### 2. `Result.to_option` / `Option.to_list` — stdlib layering — `devin/wave21-result-option`

```
[decide+impl] implementation-gaps open gap（425 行目）+ shelf:
`Result.to_option` / `Option.to_list` が存在しない — Go の
循環参照禁止で stdlib の module 構成上置けない。採択が要る:

  (a) stdlib を別モジュール化（list/option/result を分割 or
      層を追加）して正当に配置
  (b) ユーザー側で書くものとして仕様化（組み込みにしない
      判断を decision で pin）
  (c) 現状維持（台帳だけ残す）
Ownership: stdlib/, internal/stdlib, docs/spec (stdlib 節),
docs/decisions, docs/implementation-gaps (gap 閉じ).
```

### 3. design §5-6 独立 dir の並列コンパイル — `devin/wave21-parallel-build`

```
[decide+impl] implementation-gaps open gap（wave20 lane3 で
「小さくない」と評価・要件記録済み）: dir グループの並列
コンパイル。要るもの 2 点:

  (a) dep 順序づけ — 依存先 .gomi の再生成と依存側の読み取り
      が競合する。dep グラフからの topological ワーカー配置か
      .gomi 書き込みロック。
  (b) `gomimport.Loader` の同期 — `l.pkgs`/member table/
      `go/importer` fset が compile ごとに mutate される。
      Loader 全体の read-lock 共有か dir ごとの分離（共有の
      利点を潰す）かを採択する。
  `bc.Hit`/`depSink`/`bc.note`/`results[i]` の dir 分割は素直に
  並列化できる — 真の設計費用は上記 2 点。採択はここで行う:

  (a) dep 順序 + Loader 共有ロック（実装案）
  (b) dir ごと Loader 分離
  (c) 見送り（台帳に残件明記して閉じる）
Ownership: cmd/gom/cli.go (runBuild), internal/build,
internal/gomimport, docs/design §5-6,
docs/implementation-gaps (gap 閉じ).
```

### 4. hygiene — `devin/wave21-hygiene`

```
[impl] 恒例の低重心掃除:

(a) problems/ 未消化項目の台帳化判定 — wave20 lane4 で
    ctor-typeargs-unvalidated・rune-char を登録済み。
    残りの古い problems（2026-11-01/02 分）を対象に
    implementation-gaps / open-questions への登録要否を
    1 件ずつ判定。
(b) wave20 review 起因のパターン sweep: 「staging への stale
    artifact 混入」「宣言 sig vs instantiated sig の取り違え」
    「ledger 腐り行」の同型残りを grep。
(c) `FuncType` hook と同様に production `lower.Env` サイト
    経由でしか効かない lookup を新設した wave20 — 同型の
    「テストでは Env 経由・production では nil-Env fallback」
    の経路格差が他にないか確認。
(d) implementation-gaps / TODO / open-questions の
    check-off 漏れ sweep（wave20 分: nary・flat・lsp-vet・
    hygiene 各 PR マージ後の閉じ忘れ確認）。
Ownership: docs/*, internal/lsp, internal/check.
```

## Lane candidates left on the shelf

採用しなかった候補（順位づけは podhmo 次第で後 wave に送れる）:

- **LSP shared importer + member table 共有** — wave19 で
  経路設計済み（長寿命 `goimport.Loader` + exportcache
  fingerprint invalidation）。podhmo 後送り指定を 2 wave 連続
  で確認済み — shelf 継続。実装時は per-change コスト計測
  （export parse+member table で数 ms オーダー）の
  before/after を pin。
- **共有スライス View 型** — `problems/shared-slice-view-type`
  のコピーしない部分ビュー。言語面の新構文・新メンバー。
- **LSP completion/definition/references** — v1 scope 外の
  機能群（TODO）。大規模なので専用 wave 向き。
- **flat パラメータの部分適用 timing 差** — `f a` の発火が
  residual 到着まで遅延する残存差（decision に記録済み）。
  eval/codegen の divergence を許容継続するか、residual
  partial を eval 側にも移植するかは将来の検討。
