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
| 2 | Gom fn → Go func interop（(b) adapter 採択済） | `devin/wave16-func-interop` | impl |
| 3 | SourceMap per-line re-pin（(a) emission 採択済） | `devin/wave16-sourcemap-repin` | impl |
| 4 | Go named-int 算術（Duration、方針 (a)） | `devin/wave16-named-int-arith` | decision+impl |

LSP 系は優先度低下（podhmo 指示 2026-11-17 — エージェント
経由の開発では `gom lsp` を使わず CLI で足りるため）。
loader cache と completion 系は backlog へ。

open-question 由来の lane（2, 3, 4）の回答は podhmo が
prompts 段階で決定済み（下記）。分岐を残さないよう各 prompt は
「この案を実装する」形に固定。decision doc には却下した案も
理由つきで記録すること（podhmo 指示 — 後の wave で「なぜ
あれを選ばなかったか」を辿れるようにする）。

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
[impl — 採択済: (b) typed adapter]

podhmo 決定（2026-11-17）: **(b) typed adapter 採用**。
`Go.func1`/`Go.func2` のような明示変換面だけ admit して、
Gom fn 値を `func(...) T` に変換する。codegen bridge (a) は
future work として decision doc に却下理由つきで記録し、
extern only (c) も同様に却下理由を残すこと。

問い: Gom の関数値を Go の func 型として渡す方法
（docs/open-questions.md）。wave-15 lane 3 の verified
residual: `http.Client.CheckRedirect` field がこの問いに
依存し、gist-cli の `host.NewClient` が host 側に残る唯一の
理由 — 本 lane + lane 4（Duration 算術）で removal
condition が揃う。

(1) Survey — 到達可能な stdlib 面で func-typed param/field が
    どこに出るか (net/http, signal.Notify, filepath.Walk,
    http.HandleFunc, sort.SliceFunc 等)。params vs fields の
    分類付きで adapter の arity をどこまで用意するか決める。
(2) 設計: adapter の面（`Go.func1`～`Go.funcN`? 名前・arity
    上限）、curried Gom fn の wrap 形状（Go 側から
    一括呼び出し → Gom の 1 引数ずつ適用への橋）、
    `Result` ↔ `error` の返り値変換（`Try`/`try` との整合。
    callback は Gom→Go→Gom の双方向問題 — Go 側から Gom
    関数がどう呼ばれるかも sketch する）、conformance
    point（`Fn` 型 arg が `func(...)` Go param に当たるとき
    param conform / boundary conversion のどちらが担うか）。
(3) Implement, additive. `Go.func*` は adapter 専用の特殊面
    か、普通の Go-boundary 変換で足りるかを調査して決める。
(4) Tests: both directions (Gom fn passed INTO Go; Go calling
    back). gist-cli `host.NewClient` が removal condition を
    満たすなら main.gom へ移動して end-to-end で検証
    （Duration 側は lane 4 待ちのため、本 lane では
    CheckRedirect 単体で検証）。
Ownership: internal/check (conform), internal/lower +
internal/codegen (adapter の bridge emission),
internal/goimport (func-typed sig surface), docs/spec.md §8,
docs/decisions, examples/gist-cli.
```

### 3. SourceMap per-line re-pin — `devin/wave16-sourcemap-repin`

```
[impl — 採択済: (a) emission 変更]

podhmo 決定（2026-11-17）: **(a) emission 変更採用** —
codegen が構文要素ごとに `//line` を再置きし drift そのもの
を消す。(b) fold 側精緻化は「調整の繰り返しになる」として
却下、(c) 現状維持も却下 — ともに decision doc に理由つき
で記録すること。

背景: open-questions ツール項 + gaps「SpanAt の粒度差異」
の残余。`//line` drift の行エイリアシングは wave-13 の
ResolveMatch (exact/drift/ambiguous) で fold 側に対応したが、
曖昧性そのものは残っている。生成内で行カウンタが drift
する構造の先頭に re-pin directive（実際の .gom 行への
anchor 再置き）を出す。

(1) 設計: re-pin を出す構造の境界を決める — `if`/match 腕・
    複数行式・ネスト lambda・pipe chain の具体例で「どの
    construct の先頭に再置きするか」の規則を書く。directive
    数は増える。go tool が翻訳する行も .gom 真値になる。
(2) 注意: SourceMap は diag/ の anchor 表と //line directive
    の二系統 — 両者を同じ真値に揃えないと fold が二答になる
    （directive を直したら SourceMap 生成も直すこと）。
(3) 検証: drift する fixture（wave-13 の ambiguous ケース）
    で emitted //line が正しい .gom 行を指し、GoPosIndex/
    GoToolDiagnostics が single answer を返すこと。
    golden 更新は広範になる想定 — 差分は生成コードの //line
    のみであることを確認。
Ownership: internal/codegen (emission), internal/diag
(SourceMap anchor 生成), internal/build/gopos.go (fold が
両系統を読む場合), testdata/golden.
```

### 4. Go named-int 算術（Duration） — `devin/wave16-named-int-arith`

```
[decision+impl — 基本方針 (a)、調査で確定]

podhmo 決定（2026-11-17）: **(a) 方向で良いが、現状を pin
してから確定** — 「意外ときれいに整合するはず」との見立て
を検証する。named-int 算術を admit: 両 operand が同じ named
int 型なら演算可・結果もその型。untyped literal は operand
の named 型へ conform（`30 * time.Second` が `Duration` を
生む。Go の規則と同型）。

問い: Go の named 整数型（`time.Duration` = int64 等）の
値を Gom の算術でどう扱うか。`30 * time.Second` が綴れない
— `time.Second` は int64 の typed 定数だが、Gom の Int 算術は
Duration を生まず、Int × Duration は operand 型が揃わない
（wave-15 lane 3 verified residual — `NewClient` removal
condition の残り半分。struct 構築再検討の前提）。

(1) 現状 pin: `time.Second` が goimport でどう surface
    されるか（named type? Int 互換?）、`30 * time.Second` が
    今何になるか（E05xx? literal conform の欠落?）を fixture
    で記録。
(2) 設計を確定: (a) がきれいに整合するなら採用。整合しない
    場合のフォールバック — (b) literal のみ admit（非対称）、
    (c) 変換 API のみ、(d) 現状維持 — は却下理由つきで
    decision に記録。sized int（int64 等）を同じ枠に入れる
    か別問いに分離するかもここで決める。
(3) Implement per decision, additive。
(4) Tests: `30 * time.Second` が `Duration` を生む・
    `deadline - now`（`time.Until`/`Sub` の返り値）が算術に
    載る・named×Int 変数・named×literal・named 同士の
    3 パターン・混在型は honest error・`gom build` まで。
Ownership: internal/check (arith operand typing, literal
conform), internal/goimport (named type surface), spec.md §8,
docs/decisions, testdata/spec.
```

## Backlog — wave-17+ 候補（順不同）

- Go struct 構築の再検討 — wave-15 lane 3 は (c)
  constructor-extern 採用だが、残障壁は callback interop
  （wave-16 lane 2 = adapter 採択済）+ Duration 算術
  （wave-16 lane 4）の 2 件のみ。両 lane が着地すれば
  コンポジットリテラル構文 (a) を再評価する価値がある
- LSP loader session cache — per-change コスト ~48–50ms の
  `go list -export` 支配部分。優先度低下（podhmo、
  2026-11-17: エージェント経由の開発では `gom lsp` を
  使わない）。人間のエディタ用途が戻ったら取る。案の骨格:
  (a) per-package-dir cache + overlay/disk mtime 無効化、
  (b) TTL/idle eviction のみ、(c) 現状維持
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
