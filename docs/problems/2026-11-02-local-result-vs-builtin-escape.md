# Local `type Result` is indistinguishable from the builtin at the escape boundary

- 状態: 未解決
- 発見: 2026-11-02, P1-8 review follow-up

## Situation

Escape nodes (`EscapeResult`, `EscapeCall`) and Result-producing
sites decide between "stdlib's `result.Result[T, E]`" and "a
file-local `type Result a e`" by *name only*:
`internal/core/validate.go`'s `resultType` accepts any
`*types.Con` with `Qual == "" && Name == "Result"`, and codegen's
`isResultCon` does the same.

If a user file declares `type Result a e = ...`, every `Result _ e`
it writes is that *local* type — yet `try`/`|?>` still validate
(validator sees a `Result`), and codegen must not emit
`result.Err[T, E]` for it (the stdlib type is a different type).

## Why it is a problem

- `internal/codegen` guards its own Result sites with
  `!g.localT["Result"]` (`ctorApp`, `ctorRef`, `testDecl`,
  `escapeErr` panics rather than emit the wrong package), but the
  *validator* has no such guard: `escapeTarget` returns the error
  type of a local `Result _ e` happily, so a `try` over a local
  Result type-checks as an escape and reaches codegen, which can
  only panic.
- The underlying gap is upstream: `types.Con{Qual: "", Name:
  "Result"}` cannot express "the builtin Result" vs "a local type
  named Result" once shadowed — the same ambiguity the `localT`
  checks paper over at each use site. This is an identification
  issue in resolve/types, not something codegen can fix alone.

## 関連

- `internal/core/validate.go` (`resultType`, `escapeTarget`)
- `internal/codegen` (`isResultCon`, `escapeErr`, `ctorApp`,
  `ctorRef`, `testDecl`) — the `localT` guards
- [decisions/generated-go-conventions](../decisions/2026-11-02-generated-go-conventions.md)

## 考えられる対応

- Give builtin types an identity that survives shadowing (e.g. a
  reserved `Qual` or a symbol back-reference on `types.Con`) so
  `resultType`/`isResultCon` test identity, not the string "Result".
- Alternatively have the resolver reject local declarations that
  collide with builtin-scope type names (`Result`, `Option`,
  `List`, ...), turning the ambiguity into a user diagnostic.
