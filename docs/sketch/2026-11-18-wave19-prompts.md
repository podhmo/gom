# Wave 19 orchestration prompt drafts

State as of 2026-11-18: Wave 18 lanes all up —
Array/`@extern fn` + stdlib buffer 化 (#40), incremental
build cache (#41), `.gomi` 多相 let (#42), hygiene sweep
(#43). podhmo merges (chained — bottom up). This file drafts
the prompts for the next wave, following
`2026-11-18-wave18-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Execution model (unchanged from wave 18)

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

## Orchestration notes (Wave 18 lessons)

- **型の canonical 形不変条件が review の主戦場** —
  `Func.Ret` は `*Func` を持てない（`types.Fn` が flatten
  する）という不変条件が、thunk mint `() -> (a -> a)` を
  壊した。内部型を直接構築するコードは canonical 形の前提を
  置かない（`types.Fn`/`Tup` などのコンストラクタを通す）。
- **「部分一致の型 switch」は sumtype 検査に掛かる** —
  lint/走査で 2 case だけ見る switch は、残りを no-op の
  列挙 case にして `default: panic("unreachable")` を保つ
  （WalkExpr 流儀）。
- **警告系の誤爆制御は「判定の根」を同じ oracle でやる** —
  callee 同一性 lint の `CallGo` ゲートを resolved callee の
  `IsGo` に変えたらローカル関数 `takes` が誤爆した。
  分類フラグでなく由来 oracle で判定する。
- **open 決定は prompt 時採択** — lane 1 (a,b+c deferred)、
  lane 3 (a) は podhmo が採択した。本 wave も同型の
  「採択は podhmo」を残す。

## Lanes

### 1. lower の cons-recursion 特殊化 — `devin/wave19-cons-spec`

```
[decide+impl] wave18 lane1 で後送りにした (c)。stdlib の
combinator は @extern fn で buffer 化済みだが、ユーザーの
自前 cons 蓄積ループは O(n²) のまま沈黙している:

  (a) match-cons 再帰形（`map`/`filter`/`append` 様の
      acc-渡し再帰）を lower が検出し、buffer loop を emit
      する自動変換。検出形状の保守境界（どの形を認めるか、
      認めた形の等価性をどう保証するか）と取りこぼし時の
      沈黙を design に明記。将来の問題点:
      stdlib-cons-built-combinators-quadratic / open-questions
      の「lower の cons-recursion 特殊化」項。
  (b) 再び後送り — 検出ポリシーの設計だけを
      docs/sketch/ に残して閉じる。
Ownership: internal/lower, internal/codegen, rt, docs/design,
testdata/golden. 採択は podhmo。
```

### 2. `gom run` — `devin/wave19-gom-run`

```
[decide+impl] wave18 lane4 で open-questions に移管した残件。
`go run` 相当の実行経路（design.md 2.2 の go tool 実起動）:

  (a) `gom run` subcommand を新設 — compile → temp dir への
      書き出し → `go run` or `go build`+実行。要設計:
      引数の受け渡し（`--` 以降か）、workdir、`package
      main`/`fn main` の前提、生成物の置き場（temp dir を
      消すか残すか）、失敗時の診断 fold。
  (b) `gom build -o <dir>` + `go run <dir>` の組み合わせを
      文書化して閉じる — subcommand 化の価値が薄ければ
      こちら。
Ownership: cmd/gom, internal/build, docs/spec・decisions.
採択は podhmo。
```

### 3. `concat` の O(k) スタック解消 — `devin/wave19-concat`

```
[impl] open-questions「`concat` の O(k) スタック」— 右畳み
込み化で O(1) スタックの fold を O(k) 再帰に替えた（深さ =
部分リスト数）。`Array` builder と `@extern fn` がある今、
buffer 経由のより良い形状に置き換える余地:

  concat の実装を rt 側（`@extern fn` で buffer loop）か
  lower 側に移し、再帰深度を O(1) スタックに戻すか、
  部分リスト数が小さい実用域では現状で良いと pin するか
  を判定して対応。ベンチの pin は wave18 lane1 と同じ
  計測形で。
Ownership: stdlib/list, rt, internal/stdlib,
docs/open-questions check-off, testdata/spec.
```

### 4. hygiene — `devin/wave19-hygiene`

```
[impl] 低重心の台帳掃除:

(a) LSP member table 共有の設計検討: `*types.Named` identity
    が importer 単位のため session 間で member table を
    共有できない本筋の制約 — shared importer 経路の実現性を
    読み、open-questions の記述を更新するか設計案を立てる
    （重ければ後送り記録）。
(b) W0702 クロージャ捕捉 scope-out の再評価:
    `let f = fn () -> r; go { f () }` — 値の可変性が型に
    現れない問題。lintAliases の callee 追跡後の世界で
    何ができるか再判定。
(c) wave18 review 起因の follow-up sweep: Findings の
    類型化（canonical 形不変条件・部分 switch・oracle
    分岐）で同型の残りがないか grep sweep。
(d) implementation-gaps / TODO / open-questions の
    チェック漏れ sweep（wave-18 分: `Go.fnN`・P4-7・
    `.gomi` poly let の各 PR マージ後の閉じ忘れ確認）。
Ownership: internal/check/lint, internal/lsp, docs/*.
```

## Lane candidates left on the shelf

採用しなかった候補（順位づけは podhmo 次第で後 wave に送れる）:

- **curried HOF の flat 二面公開 / 呼出側 uncurry** — 生成
  コード特殊化 open-questions の残り半分（`Fold` の要素ごと
  クロージャ確保）。wave18 の buffer 化とは別レイヤー。
- **共有スライス View 型** — `problems/shared-slice-view-type`
  のコピーしない部分ビュー。言語面の新構文・新メンバー。
- **`Result.to_option` / `Option.to_list`** — Go 循環参照禁止
  （stdlib layering 制約）。別モジュール化が要るかの検討。
- **N-ary Go 結果 (3+)** — spec に上限明記済み、対応自体は
  open-questions。
- **generic-go-fns-hidden / ctor-typeargs-unvalidated /
  rune-char-correspondence など problems/ の未消化項目** —
  台帳化の要否判定から。
- **LSP definition/references・incremental sync** — completion
  より大きい単位。member table 共有が進んでから。
- **`gom lsp -vet`** — wave18 で `gom build -vet` は着地。
  LSP 側の vet 起動は lsp-gotool-invocation の opt-in 機構を
  拡げる話で別項。

## Common instructions (append to every prompt)

```
Common rules (from AGENTS.md and wave-18 prompts):
- Commit WIP early and often; one lane = one branch = one PR.
- Ledger edits (TODO/gaps/open-questions check-offs) ride in
  the lane's own PR; do not carry unrelated ledger changes.
- Diagnostics codes are append-only; never reuse. Type switches
  keep `default: panic("unreachable")`. Partial-match switches
  list the remaining variants as no-op cases.
- Gates before done: `gofmt -l .` empty, `go vet ./...`,
  `go test ./...`, `sh scripts/check-sumtype.sh`.
- Spec-visible changes update docs/spec.md; implementation
  divergences go to docs/implementation-gaps.md; decided items
  get docs/decisions/YYYY-MM-DD-<slug>.md with `- 状態:` and
  `- 日付:` bullets, then `go generate ./internal/tool/decisionsdoc`.
- Internal types go through the canonical constructors
  (types.Fn/Tup) — never build a Func whose Ret is a Func.
- Review findings go through devin_review_manage triage:
  fix → resolved + commit_sha, not-a-bug → rejected,
  undecidable → escalated, then end_triage.
- podhmo merges; do not merge PRs yourself.
```
