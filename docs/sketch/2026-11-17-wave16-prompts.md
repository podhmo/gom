# Wave 16 orchestration prompt drafts

State as of 2026-11-17: Wave 15 lanes out as PRs —
go-any tail (#22), structural generics (#23), struct
construction (#24), lsp-gotool (#25), round record (#26).
All unmerged; podhmo merges (chained — bottom up).
This file drafts the prompts for the next wave, following
`2026-11-17-wave15-prompts.md`. Always append the "Common
instructions" block to each prompt.

## Execution model (unchanged from wave 15)

Quota constraints: **no child sessions** — one Devin session
executes every lane sequentially. Review is delegated to Devin
Review on each PR. podhmo merges.

- **One lane = one branch = one PR**, chained on the previous
  lane's branch while earlier PRs are unmerged.
- **Ledger edits ride in the lane's own PR** (TODO / gaps /
  open-questions check-offs for that lane's work only).
- **LSP verification: no Emacs/Eglot** — podhmo ruled wave-15:
  when the wire protocol is unchanged, in-process JSON-RPC
  tests asserting publishDiagnostics payloads are sufficient.
- **Sequential order below.**

## Orchestration notes (Wave 15 lessons)

- **Review findings were all real** this wave — verify each as a
  concrete failing case before fixing, but expect most to hold.
- **Staging/mocking a real build context is its own bug class**:
  lane-4's temp-dir build had 4 fidelity defects (internal
  visibility, real-vs-gom same-module imports, embed assets,
  module-directive whitespace) plus a `../` embed escape on the
  re-review. Probes for this lane class must adversarially vary
  the *environment*, not just inputs.
- **"Has X" checks need the "is this the thing I meant" half**:
  `*.go` in a sibling dir doesn't mean "Go package" — it also
  matches a Gom package with handwritten helpers.
- The wave-15 stale-TODO item (`Go.Any` boundary残カバレッジ)
  predates its own lane-1 fixes — check ledger entries against
  what actually landed before trusting a task description.

## Lanes overview

| # | レーン | ブランチ | 種別 |
| --- | --- | --- | --- |
| 1 | `Go.Any` residual bugs + F1 | `devin/wave16-go-any-bugs` | impl |
| 2 | Gom fn → Go func interop | `devin/wave16-func-interop` | decision+impl |
| 3 | LSP loader session cache | `devin/wave16-lsp-cache` | decision+impl |
| 4 | SourceMap per-line re-pin | `devin/wave16-sourcemap-repin` | decision+impl |

## Individual prompts

### 1. `Go.Any` residual bugs + F1 — `devin/wave16-go-any-bugs`

```
[impl] Two recorded `Go.Any` bugs remain from the wave-13
re-review (TODO「Go.Any 値の直接呼び出し」「Go.Any との ==」,
docs/implementation-gaps.md), plus wave-15 lane 1's F1
(lambda-result want threading — verify what's actually left;
the wave-15 ledger only records F2/F3/P4 as resolved):

(a) `a : Go.Any` に対し `a "s"` — conformCallee's isGoAny
    early-accept emits `a("s")` → `cannot call non-function`
    on go build. A callee position must never accept Go.Any;
    find where the fallback leaks into callee checking and
    close it (E0401-class error), or decide the honest bound.
(b) `a == 42` — g.lit emits a bare `int` literal and rt.Equal
    is reflect.DeepEqual(int64, int) →恒偽. Either wrap as
    `any(int64(1))` via the operand's expected-type conform,
    or reject the mixed compare. Check every literal class.
(c) F1 — `let f : Int -> Go.Any = fn x -> 1`: checkLambda has
    no want channel, so `Int -> Go.Any` never reaches the
    body conform. First pin the current rejection form in a
    spec fixture, then thread want through only if the same
    channel `fn f (x:Int) -> Go.Any` uses exists.
Probe adversarial variants: `==`/`!=` both directions,
string/bool/nil literals, the call shape `a 1 "s"`,
`Go.Any` in record fields and list elements.
Ownership: internal/check (conformCallee, eq operand typing,
checkLambda), internal/codegen (emission only if (b) needs a
wrap), testdata/spec fixtures, TODO + gaps check-offs.
diag/codes.go append-only.
```

### 2. Gom fn → Go func interop — `devin/wave16-func-interop`

```
[decision+impl] Open question "Gom の関数値を Go の func 型
として渡す方法"（docs/open-questions.md、wave-15 lane 3 の
verified residual: `http.Client.CheckRedirect` — gist-cli
`host.NewClient` が host 側に残る唯一の理由。Duration 算術は
別件）。Decision first, podhmo 判定用の draft から。

(1) Survey: where does a func-typed parameter/field actually
    appear in the reachable stdlib surface (net/http,
    sort.SliceFunc 不在、signal.Notify 系、filepath.Walk,
    http.HandleFunc)? Which are func params vs struct fields?
    gist-cli の NewClient を removal condition の具体例に。
(2) Options: (a) codegen bridge — a Gom closure lowers to a Go
    func literal calling the closure (arity/arity-known call
    convention で包めるか。`fn` 値の Go 表現はどうなる),
    (b) typed adapter — `Go.func1`/`func2` 型だけ admit して
    Gom fn を明示変換（surface 最小、明示的）, (c) extern
    only — 現状維持を決定として記録 (callback 供給は Go
    adapter に残す。実害の大きさ次第で妥当)。
    各案で `fn x -> x + 1` を `http.HandlerFunc` /
    `func(int) int` に渡す emission を sketch する。callback
    側で返り値・error 変換（Result ↔ error）が要る場合の
    扱いも書く — callback は Gom→Go→Gom の双方向問題。
(3) Implement per decision, additive. Conformance point: a
    `Fn` type arg vs a `func(...)` Go param — which existing
    channel (param conform / boundary conversion) carries it.
(4) Tests: both directions (Gom fn passed INTO Go; Go calling
    back). gist-cli NewClient が removal condition を満たす
    なら example を移動して end-to-end で検証。
Ownership: internal/check (conform), internal/lower +
internal/codegen (closure → func literal emission),
internal/goimport (func-typed sig surface), docs/spec.md §8,
docs/decisions, examples/gist-cli.
```

### 3. LSP loader session cache — `devin/wave16-lsp-cache`

```
[decision+impl] TODO/deferred: `gom lsp` の per-change コスト
~48–50ms は `go list -export`（compileOptions が毎コンパイルに
fresh な goimport session を立てる）支配。wave-15 の `-go` で
全面コンパイル＋staged build ~205–210ms にこの 50ms が上乗せ
になっている。wave-11/12 で「loader session cache」として
deferred されていた件を取る。

(1) Decision draft: options (a) per-package-dir goimport
    session cache keyed by dir — invalidate on (i) any overlay
    change, (ii) any .gom/.gomi/.go disk mtime change in the
    package dir, (iii) go.mod/go.sum change; (b) TTL/idle
    eviction only; (c) 現状維持 (fresh-per-compile は常に
    correct — cache は staleness 面を持つ) を決定として記録。
    unsaved buffer とディスク更新の混在をどう invalidate
    するかを具体的に書く — overlay gather が open buffer で
    ディスクを shadow する現状の契約を壊さない。
(2) 検証: in-process JSON-RPC テストで cache hit 時の per-
    change コストを計測（TestGoToolCost 系の shape を真似る。
    Emacs/Eglot は不要 — protocol 不変）。
    重要: cached session が stale な .gomi/Go deps を返さない
    ことを、兄弟ファイルを disk に書き換えて didChange する
    テストで確認する（shadow された buffer ではなく disk
    側の変更を拾えるか）。
(3) `-go` 経路の staged build は cache 対象外のまま — この
    lane は loader のみ。
Ownership: internal/lsp (compileOptions/session 経路),
internal/gomimport + internal/goimport (cache 境界 —
additive、既存 API を変えない), docs/decisions, TODO
check-off.
```

### 4. SourceMap per-line re-pin — `devin/wave16-sourcemap-repin`

```
[decision+impl] open-questions ツール項 + gaps「SpanAt の
粒度差異」の残余: `//line` drift の行エイリアシングは wave13
の ResolveMatch (exact/drift/ambiguous) で fold 側に対応した
が、曖昧性そのものを消す emission 側の手が残っている —
生成内で行カウンタが drift する構造の先頭に per-line re-pin
directive（実際の .gom 行へのアンカー再置き）を出す案。

(1) Decision draft: (a) emission 変更 — codegen が構文要素
    ごとに `//line` を再置きし drift を消す（directive 数は
    増える。go tool が翻訳する行も .gom 真値になる）, (b)
    SourceMap 側の span→line 精緻化（emission 不変 — anchor
    の内側位置だけを記録。曖昧性は残るが fold は正しい),
    (c) 現状維持 — ambiguous fallback が実害小なら記録。
    `if`/match 腕・複数行式・ネスト lambda を具体例に各案の
    出力差分を sketch する。
(2) 注意: SourceMap は diag/ の anchor 表と //line directive
    の二系統 — 両者を同じ真値に揃えないと fold が二答になる
    （directive を直したら SourceMap 生成も直すこと）。
(3) 検証: drift する fixture（波 13 の ambiguous ケース）
    で emitted //line が正しい .gom 行を指し、GoPosIndex/
    GoToolDiagnostics が single answer を返すこと。
    golden 更新は広範になる想定 — 差分は生成コードの //line
    のみであることを確認。
Ownership: internal/codegen (emission), internal/diag
(SourceMap anchor 生成), internal/build/gopos.go (fold が
両系統を読む場合), testdata/golden.
```

## Backlog — wave-17+ 候補（順不同）

- 外部 module からの Gom import 解禁（gom-package-imports
  問題の残り 4 — self-import が module-derived になった今、
  `.gomi` 経由の boundary を再検討）。
- `.gomi` let メンバー多相（thunk oracle を import 越しに
  届ける — 大きめの decision）。
- 実装がインターフェースより一般的（多相 impl vs 単相 .gomi）。
- W0701/W0702 alias/capture 経路の補足（小さめ bug lane —
  gaps 647 行目）。
- `Result.to_option`/`Option.to_list`（import cycle 由来の
  構造的制約 — reject 決定を書くだけの小 lane）。
- `--strict` 的なエラーフィルタ（書きかけコード用途 —
  decision のみの小 lane）。
- P4-7 incremental build cache（P4-1 依存、TODO open）。
- D-3 README 十分性判定 / D-4 tutorial go.mod 整理
  （プロジェクト終盤 — ユーザー指定のタイミング）。
- LSP completion/definition/references・incremental sync・
  `$/cancelRequest`・didSave/workspace（TODO open）。

## Common instructions (append to every prompt)

```
Before finishing run: gofmt -l . (no output), go vet ./...,
go test ./..., AND sh scripts/check-sumtype.sh (CI gate —
`make check` additionally covers staticcheck+experiments; run
if cheap). Partial probes on `//sumtype:decl` unions are
if-assert chains, never partial type switches. Commit WIP early
and often — commits are the only recovery point. Decisions made
during implementation go to docs/decisions as 状態: draft;
verify with a prototype, write results to docs/sketch, then
update status. Language-rule changes add testdata/spec
fixtures, generated-code changes update testdata/golden,
Go-interop changes add tests in both directions. Concerns go
to docs/problems; spec/implementation/doc discrepancies are
recorded in docs/implementation-gaps.md, never resolved by
silently rewriting the spec. In-code comments are normative
text too — verify them against the mechanism they describe.
Ledger edits (TODO.md / open-questions / implementation-gaps
check-offs) ride in the lane's own PR this wave — mark only
what the lane closed; the round record reconciles at wave end.
Read skills/gom-agent-guide before writing .gom/.gomi files.
Verify every normative doc claim against the code it describes —
doc overclaims have been caught six waves running. Fixtures
carrying raw bytes (CRLF, non-UTF-8) need a .gitattributes
-text mark at creation time. Probe adversarial variants of the
gaps you're fixing, not just the recorded shape — review found
worse failure classes by probing three waves running.
LSP lanes: verify in-process over JSON-RPC; real Emacs/Eglot is
NOT required when the protocol surface is unchanged (podhmo,
wave-15).
```
