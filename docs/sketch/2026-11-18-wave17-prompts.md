# Wave 17 orchestration prompt drafts

State as of 2026-11-18: Wave 16 lanes all merged —
go-any bugs (#29), func interop (#30), sourcemap re-pin (#31),
named-int arith (#32). podhmo merges (chained — bottom up).
This file drafts the prompts for the next wave, following
`2026-11-17-wave16-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Execution model (unchanged from wave 16)

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

## Orchestration notes (Wave 16 lessons)

- **Review findings were all real again** — every finding this
  wave (func-adapter arity/unit shapes, printer continuation
  pins, named-literal `T(lit)` emission) was a concrete bug.
  Keep the "verify a failing case first" step, but budget for
  most findings holding.
- **Emission-shape changes have a discard/minimum edge class**:
  wrapping literals in `T(lit)` broke two adjacent contexts at
  once (statement-position conversions are not calls; the sign
  must live inside `T(-min)`). When a literal's emission gains
  a wrapper, probe every position the bare lexeme could appear
  (statement, unary sign, switch operand).
- **AGENTS.md's sumtype gate also runs in CI** — a type switch
  without exhaustive arms or `default` fails `check-sumtype.sh`
  the same as the tests. Local `sh scripts/check-sumtype.sh` is
  in the gate set for a reason; run it before every push.
- **Don't edit on `main`** — lane work drifted onto `main`'s
  working tree once this wave after a branch switch for the
  merge; check `git branch --show-current` before editing.
- open-question 由来の lane（本 wave では 1, 2）の回答は
  podhmo が prompts 段階で決定すること — 分岐を残さない。
  decision doc には却下した案も理由つきで記録すること。

## Individual prompts

### 1. `Go.FuncN` の Gom 側からの適用 — `devin/wave17-func-apply`

```
[impl] wave-16 lane 2 で `Go.FuncN`（func 型の生ビュー）を
surface したが、残差として「Go から受け取った `Go.FuncN`
値を Gom で呼ぶ」が open-questions に残っている
（callback を返す API に必要）。

(1) 現状 pin: `Go.FuncN` 型の値を `f a b` と適用しようと
    すると今何になるかを fixture で記録（E0401 or
    非関数扱いの診断）。
(2) 設計を確定: (a) apply を admit — 生ビュー引数の呼び出しは
    Gom fn と同じく curried/uncurried のどちらで解釈するか
    （`Go.Func2 A B R` の `f a b` と `f a`）、Unit/2-結果
    shape（`func() error` → `Result Unit GoError` か素の
    GoError か）を含めてここで決める。(b) 変換 API のみ
    （`Go.fn f` で Gom fn へ変換してから呼ぶ）、(c) 現状維持
    — 却下なら理由つきで decision に記録。
(3) Implement per decision, additive。
(4) Tests: `Go.Func2` の全適用・部分適用、`func() R`、
    `func(A) (T, error)` の 2 結果、なり得ない型の適用は
    honest error、`gom build` まで。
Ownership: internal/check (callee typing, FuncN apply),
internal/lower/codegen (call emission, result-shape conv),
spec.md, docs/decisions, testdata/spec.
```

### 2. loader セッション cache — `devin/wave17-loader-cache`

```
[perf/impl] TODO 残件: `gom lsp`/`gom check` の per-change
コストは `go list -export` 支配の ≈48–50ms（wave12 計測）。
loader の pkg loading をセッション/プロセス内で cache する。

(1) 現状 pin: `gom check`/`build` 1 回あたりの `go list
    -export` 呼び出し数と時間を計測して記録（対象は
    goimport.Loader）。
(2) 設計を確定: cache key（pkg path + module 状態）、
    invalidation（ファイル mtime or `go list -deps` の
    fingerprint）、CLI の一回限りプロセスと LSP の長居
    プロセスで cache 戦略が同じか、をここで決める。
(3) Implement per decision。
(4) Tests: 2 回目の `gom check` が `go list` を再実行
    しない・依存ファイル更新で invalidation が効く・LSP
    per-change が下がる、を計測つきで確認。
Ownership: internal/goimport, internal/lsp (session reuse),
docs/design.md, TODO check-off.
```

### 3. 外部 module からの stdlib import — `devin/wave17-stdlib-import`

```
[decision+impl] open-questions 残件: メイン module 外の Gom
import が E0203 で拒否され、stdlib の Gom module（`import
"list"` 等）を外部 module から import できない（P3-6 発見、
`testdata/gocall` fixture は stdlib 依存を手書きで代替した）。

(1) 現状 pin: `testdata/gocall` 相当の外部 module から
    `import "list"` すると今何になるかを fixture で記録。
(2) 設計を確定: stdlib の Gom source を外部 module の
    loader が見えるか、生成 Go import にどう落とすか
    （生成物の import path と .gomi の公開形）、をここで
    決める。(a) stdlib module のみ公開、(b) 一般の外部
    Gom package も公開、(c) 現状維持 — 却下理由つきで
    decision に記録。
(3) Implement per decision, additive。
(4) Tests: 外部 module fixture で `import "list"` が
    check/build/test を通る、`testdata/gocall` の手書き
    代替を本経路に置き換える。
Ownership: internal/resolve, internal/goimport/gomimport,
internal/build, spec.md 7 章, docs/decisions, testdata/gocall.
```

### 4. hygiene — `devin/wave17-hygiene`

```
[impl] 低重心の台帳掃除。個別に PR 化するほどではないが、
wave-16 の着地で生じた follow-up を一括で潰す:

(a) gom-agent-guide の残る陳腐箇所を走査 — wave-16 で
    `references/go-interop.md` を同期済みだが、SKILL.md /
    language-fundamentals / project-layout の他の
    references に func interop・named-int 算術・新しい
    `gom test`/`dump` 経路と齟齬する記述が残っていないか
    全部読み直して直す（podhmo 報告: 「skill 古かった」 —
    #28 起因）。
(b) `host.NewClient` の除去可否を再確認 — wave-16 で
    callback (lane 2) と Duration 算術 (lane 4) が着地した。
    decision 2026-11-17-struct-construction (c) どおり
    factory は残る設計だが、CheckRedirect 供給と Timeout
    算術を Gom 側で書けるようになった今、host 側の特別
    扱いをどこまで縮められるかを検証し、コメントの記述を
    現状に合わせる（削除しないならその旨を decision に
    追記するか open-questions に残さない旨を明記）。
(c) implementation-gaps / TODO / open-questions の
    チェック漏れを sweep（wave-16 分）。
Ownership: skills/gom-agent-guide, examples/gist-cli, docs/*.
```

## Common instructions (append to every prompt)

```
Common rules (from AGENTS.md and wave-16 prompts):
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
