# Local `type Result` is indistinguishable from the builtin at the escape boundary

- 状態: 解決（2026-11-02、宣言ベースの組み込み判定を採用 — [decisions/builtin-type-identity](../decisions/2026-11-02-builtin-type-identity.md)）
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
- `internal/lower` (`lower.go` `isResultCon`, `pat.go`) — a
  file-local `type Result` also takes the builtin dispatch: a
  `match` over it emits `switch r.Tag() { case
  result.ResultTagOk: ... r.Value }` against an interface that
  only declares `isResult()`, so the generated Go cannot compile
  (found while verifying fix-diff-bugs; the escape-boundary
  manifestation above is the same identification gap, one site
  earlier)
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

## Resolution

Adopted a third route — declaration-aware builtin identity:

- Local `type` declarations stay **legal** — `type Result`,
  `type Option`, `type List`, `type Int` are ordinary ADT/record
  declarations that shadow their builtin for the whole file.
- Every phase that asks "is this the builtin `X`?" consults the
  phase's own declaration index before the name:
  - check: `typeInfo.sym` on `tyCons` (`shadowedBuiltin`,
    `isBuiltinCon` in check.go)
  - lower: `typeD` (`declaredT`/`builtinName`/`isResultCon`/
    `isOptionCon` in lower.go)
  - core validator: `localT` collected from `core.TypeDecl`s
    (`conIs`/`resultType` in validate.go)
  - codegen: `localT` (existing guards)
  - eval: `interp.localT` collected from `check.TypeDecl`s
    (`VVariant.IsResult`, `resultTag`, `errIsGoError`)
- Constructs that *specifically need* the shadowed builtin report
  **E0415** (`shadowedErr`) — `try`/`|?>` and Result boundaries,
  builtin `Some`/`None`/`Ok`/`Err` signature lookups, list
  literals/patterns, `for`/`select`/spread element types, literals
  and numeric constraints, test-body shape, and foreign/extern/builtin
  signatures that mention the name (`sigOf`/`shadowedSigDiag`,
  deduplicated per (symbol, builtin) via `shadowKey`).
- A reserved `Qual` / Con back-reference was not needed: every
  phase already owns a declaration table; plumbing symbol identity
  into `types.Con` would duplicate what `typeInfo.sym`/`typeD`/
  `localT` already decide.
- Rejecting the declarations was rejected: shadowing is spec-
  sanctioned name resolution (decisions/name-resolution.md's
  scope model), and a file-local `type Result` is a useful ADT.

See `docs/decisions/2026-11-02-builtin-type-identity.md` for the
full rationale and the E0415 coverage table.
