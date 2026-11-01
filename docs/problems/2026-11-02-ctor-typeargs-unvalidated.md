# The validator never checks `CtorApp`/`CtorRef` `TypeArgs`

- 状態: 未解決
- 発見: 2026-11-02, P1-8 review follow-up

## Situation

`CtorApp.TypeArgs` and `CtorRef.TypeArgs` carry "the instantiation
of the ADT's type parameters" (internal/core/call.go, expr.go), but
`internal/core/validate.go` only checks the callee kind and
argument count for `CtorApp` (and only the symbol kind + function
type for `CtorRef`) — `TypeArgs` is never validated: not its count
against the ADT's type-parameter arity, not its consistency with
the node's result type.

## Why it is a problem

- codegen trusts `TypeArgs` when present and falls back to the
  result type's `Con.Args` when absent (`ctorTArgs`). A malformed
  node — wrong count, or args inconsistent with `Typ` — emits a
  wrong instantiation rather than a violation.
- The fallback also papers over the real question: which of
  `TypeArgs` or `Typ`'s own arguments is authoritative when both
  are present and disagree? Lowering should be able to rely on one
  canonical source.

## 関連

- `internal/core/validate.go` (CtorApp case ~line 591, CtorRef
  ~line 450)
- `internal/codegen/expr.go` (`ctorTArgs`, `ctorGoType`,
  `ctorApp`, `ctorRef`)

## 考えられる対応

- Have the validator check `TypeArgs` arity against the owning
  ADT's type parameters (needs ctor→type-owner resolution, which
  the validator does not currently track).
- Or declare `TypeArgs` optional-but-authoritative: when present
  it must equal `Typ`'s `Con.Args` — a cheap consistency check
  without owner lookup.
