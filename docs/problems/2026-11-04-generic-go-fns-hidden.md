# Spec promises callable generic Go functions; the loader hides them all

- 状態: 未解決
- 日付: 2026-11-04

## Situation

Spec 8.5 says Go generic functions whose constraints are `any` or
`comparable` are callable in v1. The `import go` loader
(`internal/goimport/mapping.go` `mapSignature`) rejects *every*
signature that carries type parameters — `sig.TypeParams().Len() > 0`
or `RecvTypeParams` — with "generic Go functions are not supported
yet", so such members land in the package table hidden and reference
sites get E0501.

The same gap applies to generic named types and instantiated
generics (`mapping.go` `mapType`), which spec 8.5's "制約付きの型
パラメータを持つジェネリック関数など" phrasing half-admits but the
callable-generics bullet still contradicts.

## Why it matters

- Users reading spec 8.5 will try `slices.Contains`, `maps.Keys`,
  `cmp.Compare` and friends and get "not exposed" diagnostics whose
  reason ("generic Go functions are not supported yet") reads like a
  bug, not a documented limitation.
- Lowering instantiation (`cmp.Compare[int]`) needs a call-site type
  argument story the check/lower contract does not have yet — this
  is why the loader takes the safe path. The spec bullet was written
  before that boundary was pinned down.

## Plausible directions

- Decide the instantiation surface (`cmp.Compare[Int]` type args on
  a Go member? `extern` specialization only?) and implement, or
- amend spec 8.5 to "generic Go functions are hidden (E0501) in v1"
  and move the `any`/`comparable` allowance to Future work.
