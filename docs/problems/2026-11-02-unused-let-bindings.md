# Unused `let` bindings: error, or silently elided?

- 状態: 未解決

## 状況

Go rejects `n := f()` when `n` is never read. `internal/codegen`
therefore emits an unused named `let x = e` as `_ = e` (evaluated,
not bound) — same rule it applies to type-switch guards, case
payloads, `select` receive binds, and `for` element binds
(`walk.go`'s symbol-use walk, `emit.go`'s `body`).

Whether `let x = e` with no later use of `x` is *valid Gom at all*
is undecided: spec.md says nothing about unused-variable
diagnostics. The Core IR validator accepts it (it checks
well-formedness, not lint).

## なぜ問題か

- If unused `let` should be an error (Go-style), that diagnostic
  belongs to `internal/check` — codegen's elision then merely covers
  IR the checker would have rejected anyway, and hand-built fixtures
  like codegen's own (which bind `p`, `tp`, `i`, `j`, `i64` without
  use, mirroring `internal/core/core_test.go`) stay compilable.
- If unused `let` is legal, the elision is the only thing keeping
  generated output compilable — and it silently drops a binding the
  user wrote, which reads oddly in generated code (`_ = e` where the
  source says `let x = e`).

Either way codegen's behavior is defensible, but the *diagnostic*
question — is `let x = e; y` (x unused) an `E0xxx` error? — has no
answer in spec.md today.

## 関連

- [decisions/generated-go-conventions](../decisions/2026-11-02-generated-go-conventions.md)
  — the elision rule itself;
- [sketch: P1-8 verification](../sketch/2026-11-02-p1-8-codegen-verification.md);
- spec.md — no unused-variable rule exists yet.

## 考えられる対応

- Decide in spec whether unused `let`/param binds are an error, a
  warning, or legal; if an error, file a TODO for `internal/check`
  and keep codegen's elision as defense-in-depth.
