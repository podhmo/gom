# Wave 18 orchestration prompt drafts

State as of 2026-11-18: Wave 17 lanes all up —
Go.fnN adapter (#35), go list -export file cache (#36),
external-module / stdlib-short import (#37), hygiene (#38).
podhmo merges (chained — bottom up). This file drafts the
prompts for the next wave, following
`2026-11-18-wave17-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Execution model (unchanged from wave 17)

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

## Orchestration notes (Wave 17 lessons)

- **Review findings were all real again** — every finding this
  wave (decision-doc adoption/rejection swap, vendor/GOFLAGS
  interplay, `import l "list"` alias loss, GOENV-invisible
  cache key) was a concrete bug. Budget for most findings
  holding.
- **Toolchain-adjacent behavior is the review surface**:
  `go list -m`, vendor dirs, GOFLAGS, `go env -w`, go.work —
  any loader/cache path must be probed against the Go tool's
  own rules (last `-mod` wins, GOENV file folding), not just
  process env. Fixtures should simulate the environment
  (GOFLAGS, vendored stale .gomi), not only happy paths.
- **Subprocess-counting tests must count deltas or specific
  commands**: adding `go env` to the wrapped gobin shifted
  every absolute count; the dep-edit pin only holds because it
  asserts the second load's delta, not a total.
- **Doc drift is a finding class**: a decision doc recorded
  the opposite of the shipped plan after the implementation
  pivoted. When the plan changes mid-lane, update the decision
  doc in the same commit as the code, not at the end.
- **Boundary semantics need a runtime pin, not only types**:
  `(nil, nil)` → `Ok None` was only covered by the extern
  view; the fnN adapter path needed its own e2e subcommand.
  errwalk-style realworld fixtures are the cheap place for it.
- **Don't edit on `main`** — check `git branch --show-current`
  before editing.

## Individual prompts

### 1. cons 構築 combinator の buffer 化 — `devin/wave18-cons-buffer`

```
[decision+impl — 採択は podhmo] problems
2026-11-05-stdlib-cons-built-combinators-quadratic: slice 表現
List の構築側 `[x, ..rest]` が O(n) で、`Map`/`Filter`/`Append`/
`Rev`/`Range` が O(n²)（Map n=2048 ≈5.9ms/17.7MB、線形の ~55x）。
ユーザーが cons 蓄積するコードも同じ壁に当たる。

採択候補（problems file より）:
(a) `Array` builder + `to_list` — P0-3 sketch の本命。
    `Array` の member surface（alloc/index-write/freeze/
    不変への引き渡し）の設計が要る
(b) intrinsic / extern-impl — `.gomi` マーカーで一部 stdlib
    decl を hand-written Go helper に繋ぐ
(c) lower の cons-recursion パターン特殊化 — ユーザー側の
    自前ループにも効く反面、pattern 検出の保守面が重い

(1) 採択を確定して decision に記録（却下案も理由つき）。
(2) 現状 pin: 現行の O(n²) を benchmark fixture で記録
    （sketch 2026-11-05-p5-3-generated-code-perf の再計測
    でよい）。
(3) Implement per decision — `Map`/`Filter`/`Append`/`Rev`/
    `Range` を buffer 経路へ。ユーザー側ループまで届くかは
    採択案次第（(c) なら届く）。
(4) Tests: 線形時間の再計測、golden 更新、spec/stdlib 文言。
    `Fold` の要素ごとクロージャ確保も同じ problems file の
    対象だが別 lane 候補（本 lane に畳むかは採択案と相談）。
Ownership: internal/lower or internal/stdlib or new builder
surface, stdlib/*, docs/decisions, docs/spec.md,
testdata/golden, problems file の状態更新.
```

### 2. インクリメンタルビルドのキャッシュ（P4-7） — `devin/wave18-incremental`

```
[impl] TODO P4-7: design.md 5 章のキャッシュキー設計を実装
する。`.gomi` を型検査の境界として既に使っているので、
残るのは「自分のソースか依存先のインターフェースのハッシュ
が変わったときだけ再コンパイル」の機構。

(1) 現状 pin: multi-package な fixture（例: testdata/multifile
    や外部 module 経由の gocall）で `gom build` の全量再生成
    コストを計測。
(2) 設計を確定: `.gomi` の正規化構文木ハッシュ（コメント・
    書式を除く）、コンパイラ/codegen 方式バージョン、rt/stdlib
    ABI、extern 規則、Go バージョン/ビルド条件を key に含める
    （design.md 5 章の一覧）。生成 `.go` が同一内容なら
    書き換えない規則（Go build cache に委ねる）も実装。
    @test 定義はハッシュに含めない。
(3) Implement per decision — additive、opt-out 環境変数付き
    （wave17 の NOCACHE 先例）。
(4) Tests: 依存パッケージの実装のみ編集で downstream が
    スキップ、`.gomi` 意味変更（ctor 追加等）で再生成、
    同一生成物の mtime 不変。
Ownership: internal/build, internal/gomimport (member table
hash 入力), docs/design.md §5 check-off, TODO P4-7 check-off,
docs/decisions.
```

### 3. LSP completion — `devin/wave18-lsp-completion`

```
[impl] TODO 残件のうち最も小さい実用面: `textDocument/
completion`。definition/references・incremental sync・
`$/cancelRequest`・didSave/workspace は別 lane 候補として
open-questions に残す（本 lane に畳まない — wave の粒度を
保つため）。

(1) 現状 pin: 現在の server が completion を unimplemented
    で返すか何もしないかを JSON-RPC fixture で記録。
(2) 設計: 候補ソースを scope（lexer/parser 文脈で何が
    完成対象か — 識別子、module member `p.`, Go member
    `fmt.`）と出し分けの最小ルールを decision に記録。
    trigger characters（`.`）と incomplete ident 位置の
    扱い。
(3) Implement: internal/lsp に completion handler。compile
    unit は package（wave12 着地済み）なので scope は
    resolve.Resolved から辿れるはず — 未 parse 可能な
    partial input の回復（入力途中の構文）をどこまで
    許すかは decision の論点。
(4) Tests: in-process JSON-RPC で completion payload を pin
    — `fmt.` → exported members、ローカル let/module
    member、prefix filter。Emacs/Eglot は不要（podhmo ruling）。
    per-change コストに completion が乗ることを計測コメント
    で記録。
Ownership: internal/lsp, internal/resolve (scope oracle),
docs/design.md, TODO 該当行の部分 check-off.
```

### 4. hygiene — `devin/wave18-hygiene`

```
[impl] 低重心の台帳掃除。wave-17 の着地と review で生じた
follow-up を一括で潰す:

(a) W0701/W0702 lint の alias 経由捕捉: `let mk = Chan.make`
    は callee シンボル一致を外れて警告を逃げる（gaps 650）。
    value-alias 経由の lookup を辿るか、scope-out を維持
    するかを判定して対応。
(b) E0407/E0409/E0801 の message/data["type"] が quiet meta
    の raw mint ID を出しうる（gaps — E0401 側は metaNamer
    済み）。同じ Display 経路で採番するか確認。
(c) `gom run`・`go vet` 実起動経路（gaps 306 残件）— `gom
    build -go`/`gom lsp -go` の先例どおり opt-in なら
    採用可能か判定。設計が重ければ open-questions に
    書き残して終わる。
(d) implementation-gaps / TODO / open-questions の
    チェック漏れ sweep（wave-17 分: LSP member table 共有の
    残件表記、`Go.fnN` の ledger 化 等）。
Ownership: internal/check/lint, internal/build, docs/*.
```

## Lane candidates left on the shelf

採用しなかった候補（順位づけは podhmo 次第で後 wave に送れる）:

- **LSP member table 共有** — `go list -export` の parse と
  member table 構築が per-change 残る（数 ms）。`*types.Named`
  identity の制約で shared importer 経路の検討が要る。
- **`.gomi` の多相 `let`** — member table の thunk 表現が前提。
  v1 bounded 形（E0602）は維持の判断もあり。
- **LSP definition/references・incremental sync** — completion
  より大きい単位。completion の scope oracle ができてから。
- **N-ary Go 結果 (3+)** — spec に上限を明記済み、対応自体は
  open-questions。
- **`Result.to_option` / `Option.to_list`** — Go 循環参照禁止の
  layering 制約（gaps 409）。stdlib 側の別モジュール化が要るか
  の検討。

## Common instructions (append to every prompt)

```
Common rules (from AGENTS.md and wave-17 prompts):
- Commit WIP early and often; one lane = one branch = one PR.
- Decisions the prompt leaves open go to
  docs/decisions/YYYY-MM-DD-<slug>.md (状態: draft) first,
  validate with a prototype, then update the state and record
  the result in docs/sketch/. 却下した案も理由つきで記録する。
- Ledger edits (TODO / implementation-gaps / open-questions)
  ride in the lane's own PR.
- Spec-visible changes update docs/spec.md; generated-code
  changes regenerate testdata/golden; found mismatches go to
  docs/implementation-gaps.md rather than silent spec rewrites.
- Gates before every commit: `gofmt -l .` empty,
  `go vet ./...`, `go test ./...`,
  `sh scripts/check-sumtype.sh`.
- diag/codes.go is append-only; internal AST switches keep
  `default: panic("unreachable")` (or exhaustive arms) —
  check-sumtype runs in CI too.
- codegen makes no semantic decisions; typing/conversion
  plans are settled in Core IR.
- Reply and write docs/comments in Japanese prose with
  English code terms; PR descriptions follow the repo
  template.
- Verify the checkout before editing:
  `git branch --show-current`.
```
