# spec.md code-example audit

Date: 2026-11-03. Go: 1.27 linux/amd64, gom built from this worktree
(`go run ./cmd/gom`).

## Purpose and scope

TODO Bugs entry: "spec の例のうち現状コンパイルできないものを整理する".
Every fenced `.gom`-looking block in `docs/spec.md` (14 blocks) was
extracted and run through `gom check`; passing ones were also run through
`gom build`. Most spec blocks are fragments, so each was embedded verbatim
in a `package main` file with the smallest stubs for ambient names the
fragment references (`Job`, `Money`, `is_zero`, …). Fragments that the
spec itself marks as errors were checked for the *expected* diagnostic.
Scratch files lived in `/tmp/spec-ex` (not committed); the diagnostics
below reproduce with `gom check` on the same snippets.

## Result table

| spec block | § | check | build | failing constructs / why |
| --- | --- | --- | --- | --- |
| opening `package shapes` | 1 | FAIL | — | `List.map`, `List.sum` — no stdlib members yet (P4-8). Everything else (ADT, `match`, `|>`, `fmt.Println` mid-block discard position) fine |
| pipes `let total/task/handle` | 1.4 | FAIL | — | `List.filter`/`map`/`sum` (P4-8), `Task.spawn` (P5-1). `try` inside the `Task.spawn` lambda itself is fine |
| newline rules `let total/page` | 1.5 | FAIL | — | `List.map`/`List.sum` (P4-8). Multi-line `(render_page …)` form works |
| labeled args `transfer` | 2 | PASS* | PASS* | Order-free labels, `~to` punning all work. The line spec marks `// エラー` errors (E0401) as specified. *Needs `Money` declared as an ADT (e.g. `JPY`) — Gom has no type alias, so `~amount: 100` requires a `Money`-typed literal like `(JPY 100)` |
| sum types `Option`/`Expr` | 3.2 | PASS | PASS | `type Option a` shadows the builtin per 3.1 — clean |
| `eval` non-exhaustive match | 4 | PASS† | — | †Error demo: emits exactly the documented E0301 — same `missing_patterns` list and `arms with guards do not count toward exhaustiveness` note as the spec's sample output |
| `.gomi` `shapes` | 7 | PASS | — | `.gomi` parses and checks alone; interface-vs-impl verification is P4-1 |
| `parse_sum` + `report` | 8.1 | FAIL | — | `strings.Join(xs, …)` today wants `Go.Slice String`, not `List String` — `[]T`↔`List T` boundary copy is P3-3. `List.length` is P4-8. `try strconv.Atoi`, `ignore`, `buf.WriteString`/`buf.String` method calls all work — the stale note "ignore 未実装" no longer applies |
| `_` placeholders | 8.2 | PASS | PASS | `strings.Repeat(_, 3)`, `strconv.ParseInt(_, 10, 64)` compile and build |
| `extern go` blocks | 8.4 | FAIL | — | `extern` parses and trivial externs pass (`os.Getenv`, `io.Copy` w/o `Ptr`), but `Option (Ptr Flag)` fails with a bogus `` `Ptr` takes 3 type argument(s), got 0 `` — the `*T`→`Option (Ptr T)` override machinery is P3-5. Also `Result Int64` vs 8.3 table (see gaps) |
| `load_config`/`parse_port` | 9.2 | FAIL | — | `os.ReadFile` → `[]byte`: `byte/uint8` is spec-excluded in v1 → E0501 (self-consistent spec limitation, but makes the example non-compilable). `Result Int64` annotation contradicts 8.3 (`ParseInt` → `Result Int GoError`): change it to `Int` and the whole `|?>` chain incl. `_` placeholder passes |
| concurrency block | 10 | FAIL | — | 12 member errors: `Chan.*` (P5-1), `List.length`/`range`/`map` + `Option.get` (P4-8). Below them, `ctx.Done()` → `<-chan struct{}` also fails: `struct{}` has no mapping (E0501) — not in 8.3's v1 exclusion list → spec gap. `select`, `for x in ch`, `go {}`, `RecvChan`/`SendChan` params, `time.After`/`time.Second`/`context.Context` all work |
| `@test`/`test` blocks | 11 | PASS | PASS | `@test`, `test`, `assert_eq`, `try` in a `Result`-typed test body all check and build (`gom test` runner is P4-3) |

Of the 14 untagged fenced blocks, 13 were audited: PASS = 6 (4 build,
1 check-only `.gomi`, 1 intended-error demo), FAIL = 7 on missing
features. The remaining untagged block is the §4 E0301 diagnostic
rendering; §12 additionally has 4 `json`-tagged blocks not counted here.

## Side findings

- **`ignore` is implemented** — the TODO entry's "8.1 `report` 例
  (`ignore` 未実装)" note is stale; `ignore` works in all positions.
- **`int64` → `Int`**: `internal/goimport/mapping.go` `basicType` maps
  Go `int64` to `Int` per spec 8.3 and the go-types-mapping decision.
  The §9.2 `parse_port` and §8.4 `io.Copy` examples declaring
  `Result Int64 GoError` therefore contradict the spec's own table —
  logged in `docs/implementation-gaps.md` (extern may intend an
  override; §9.2 has no extern).
- **`struct{}` unmapped**: `ctx.Done()` (`<-chan struct{}`) hits
  "unsupported Go type struct{}" in `mapType`'s default branch. Spec
  neither maps `struct{}` nor lists it as v1-unsupported — logged in
  `docs/implementation-gaps.md`.
- **Extern `Option (Ptr Flag)` arity diagnostic**: reports
  `` `Ptr` takes 3 type argument(s), got 0 `` — a nonsense message from
  the unimplemented extern conversion path (nothing takes 3 type args).
  Recorded as a TODO Bugs entry tied to P3-5.
- **Phase gating hides errors**: a resolve-phase diagnostic (e.g. the
  E0201 "exposes no members yet" from `List.length`) suppresses all
  check-phase diagnostics file-wide — `strings.Join`'s E0401 in §8.1 is
  invisible until `List.length` is removed. This is the documented
  "later phases only run on error-free input" behavior, not a bug, but
  it means a failing example can hide more failures than shown.
- **`type Money = Int` trap**: the audit's first `transfer` stub used
  `type Money = Int`, which declares a nullary *variant* `Int` — there
  is no type-alias syntax, so `~amount: 100` needs an ADT-typed value.
  Consistent with the skill guide's "not aliases" trap.

## Spec annotations added

Parenthetical notes after each failing block (spec's existing note
style): §1 opening, §1.4, §1.5, §8.1, §8.4, §9.2, §10 — each naming the
missing feature and TODO id (P3-3, P3-5, P4-8, P5-1) or pointing to the
implementation-gaps entries. Passing examples (§2, §3.2, §4, §7, §8.2,
§11) were left unannotated.
