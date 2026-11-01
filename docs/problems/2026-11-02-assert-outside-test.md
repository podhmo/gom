# `assert`/`assert_eq` always emit `rt.Assert*(t, ...)` — `t` is undefined outside tests

- 状態: 未解決（2026-11-04 追記: lowering が `test` ブロック外の
  `assert`/`assert_eq` を E0901 "may only be called inside a test
  block" と診断する暫定措置を入れた — 壊れた Go は出ないが、
  `assert` が普通のコードで使えるかという意味論の決定は未解決のまま）
- 発見: 2026-11-02, P1-8 review follow-up

## Situation

`callBuiltin` (`internal/codegen/expr.go`) emits
`rt.AssertEq(t, actual, expected)` / `rt.Assert(t, cond)` for the
`assert_eq`/`assert` builtins — unconditionally. The `t` refers to
the `*testing.T` parameter that only exists inside generated test
functions (`testDecl`).

resolve registers `assert`, `assert_eq`, `todo`, `not` as
`KindBuiltin` in the builtin scope for *every* file, so a
`CallBuiltin{Sym: assert}` sitting in a plain `fn` produces
`t`-undefined code at the Go build — the panic/codegen path is not
reached, the output simply does not compile.

## Why it is a problem

- The constraint "assertions are only usable inside `test` blocks"
  is a semantic rule and so belongs to `internal/check` (or the
  lowering that builds `CallBuiltin`), matching how escapes are
  constrained to Result boundaries by the checker — codegen is
  deliberately not the place that decides it.
- Until that constraint exists somewhere upstream, codegen's fixed
  table is a landmine: hand-built or mis-lowered IR compiles into
  broken Go with no diagnostic.
- spec.md section 12 describes assertions as test syntax; whether
  `assert` in ordinary code is a diagnostic is unwritten (related:
  problems/unused-let-bindings, same class of missing lint rules).
- `@test fn` helpers hit the same gap: they lower as ordinary
  functions with no `t` parameter, so an `assert` inside a
  `@test fn` reports E0901 today — test helpers cannot assert
  until the `gom test` design decides how `t` reaches them.

## 関連

- `internal/codegen/expr.go` (`callBuiltin`)
- `internal/resolve/scope.go` (`builtinScope` — registers the
  builtins unconditionally)
- spec.md section 12 (assertions)

## 考えられる対応

- Enforce "assertions only inside `test`" in `internal/check` and
  keep codegen's `t`-emitting table as-is.
- Or lower `assert`/`assert_eq` differently outside tests (e.g.
  panic on failure), if spec wants them usable in ordinary code.
