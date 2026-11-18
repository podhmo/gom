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

採択（podhmo, 2026-11-18）: (a) + (b) — `Array` builder + `to_list`
と intrinsic/extern-impl の両立。(c) lower 特殊化は future work。

```
[decision+impl — 採択済: (a)+(b), (c) future work] problems
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

### 3. `.gomi` の多相 `let` — `devin/wave18-gomi-polylet`

採択: **(a) 許す**（podhmo、2026-11-18）

```
[decision+impl — 採択は podhmo] open-questions 残件:
`.gomi` の `let` メンバーに多相型（`let empty : List a`）を
許すか。v1 は E0602 で型変数を拒否する bounded 形。

採択候補:
(a) 許す — member table に thunk 表現が要る。実装側の
    generalized let は `func X[T]()` を emit するが、
    `.gomi` 経由の member 参照 `p.X` は呼出なしの値参照 —
    check/lower の thunk oracle を import 越しに届ける
    機構が本題
(b) v1 bounded 形を維持して decision で閉じる —
    stdlib に多相 let が要る実例が乏しければこれで確定
    させて open-questions から外す

(1) 現状 pin: `.gomi` に `let empty : List a` を書くと
    今 E0602 で落ちることを fixture で記録。
(2) (a) の実装コストを読み見: thunk oracle が今どこに
    あるか（check の generalized-let 判定、lower の
    `func X[T]()` emission、member 参照側の即時適用の
    要否）、`.gomi` 経由で何を追加伝搬するか。stdlib の
    `.gomi` に多相 let が欲しい実例があるか走査（例:
    `list.empty`/`option.none` 相当）。
(3) 採択を確定して decision に記録（却下案も理由つき）。
    (b) なら open-questions の項目を採択結果つきで閉じて
    本 lane は終了（fixture の pin だけ残す）。
(4) (a) なら implement: `.gomi` member table の thunk
    表現、`p.X` 参照の instantiation、codegen の
    `X[T]()` 呼出。spec/.gomi 仕様の文言更新。
    Tests: cross-package で `p.empty : List a` を両方の
    concrete 型に instantiate、`empty` を部分適用しない
    値として渡す、stdlib fixture。
Ownership: internal/check (thunk oracle), internal/lower/
codegen (emission), internal/gomimport (.gomi member
table), docs/spec.md .gomi 節, docs/decisions,
testdata/spec, open-questions check-off.
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

結果（2026-11-18 実行分）:
- (a) 採用 — `lintAliases`/`lintCallee` が `let x = <symbol>`
  の値エイリアス（多段含む）を辿り、W0701・W0501・W0502 が
  alias 経由でも発火。W0501/W0502 のゲートは `CallGo` から
  解決 callee の `IsGo` へ（ローカル関数の誤爆回避）。
  W0702 のクロージャ捕捉は別問題として scope-out 維持
  （decision: 2026-11-18-lint-callee-alias.md — 2026-11-04
  の alias scope-out を supersede）。
- (b) 採用 — E0407/E0409/E0801 は確認の結果すでに Display
  済み。sibling の `.String()` サイト（E0508・E0413・E0416・
  W0501・W0502・W0701・W0702）を `types.Display`+`metaNamer`
  に掃き出し。E0417 の `judgeT` は `litRange` 由来の具象で
  安全なため未変更。
- (c) 半分採用 — `gom build -vet` を新設（decision:
  2026-11-18-build-vet-flag.md）。`gom run` は実行形の設計が
  要るため open-questions に移管。
- (d) 実施 — LSP member table 共有（open-questions/TODO 両方
  記載済み）・`Go.fnN`（spec §8 記載済み）の wave-17 残件は
  登録済みを確認。新規の未登録漏れはなし。
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
