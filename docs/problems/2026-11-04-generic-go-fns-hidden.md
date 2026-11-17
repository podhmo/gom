# Spec promises callable generic Go functions; the loader hides them all

- 状態: 解消（P3-3 で `any`/`comparable` 制約のジェネリック関数は呼び出し可能になった。wave 15 lane 2 で構造制約 `~[]E`/`~map[K]V` と基本型 union も公開 — spec §8.1.6/§8.3/§8.5 を更新。残差はメソッドセット制約・交叉・型パラメータ入り union の E0506 と `iter.Seq` など結果側の未対応）
- 日付: 2026-11-04

## Resolution (P3-3)

`mapSignature` now classifies each type parameter's constraint:
`any` (`Empty`) and `comparable` (`IsComparable`) map onto
`check.Signature.TypeParams`/`GoConstraints` in declared order and
the callee is callable; `instantiateSig` instantiates by declared
order so `CallInfo.TypeArgs` lines up with the emitted
`pkg.F[T1, T2]`. `comparable` arguments are validated after
inference against the generated representation and report E0508
(`goReprComparable`). Other constraints — method sets, unions,
`~` approximations — still hide the symbol, now with a dedicated
E0506 (`resolve.Symbol.HiddenCode`) whose message renders the
constraint, instead of the catch-all "generic Go functions are not
supported yet" E0501.

Verified in
[docs/sketch/2026-11-04-p3-3-go-collections-verification.md](../sketch/2026-11-04-p3-3-go-collections-verification.md);
the constraint scope is decided in
[docs/decisions/2026-11-04-go-collections-and-generics.md](../decisions/2026-11-04-go-collections-and-generics.md).

## Remaining concern

The literal spec 8.5 bullet is now implemented, but it makes the
spec 8.1.6 example wrong: `slices.Contains`, `slices.Index`,
`maps.Equal` and most of `slices`/`maps` carry structural
`S ~[]E` / `M ~map[K]V` constraints and stay hidden (E0506). Either
spec 8.1.6's example needs replacing (e.g. `cmp.Or`, which is
`comparable` and *is* callable) or the callable set needs widening
to structural constraints — a spec amendment, recorded in the
decision file and the implementation-gaps ledger.

Generic *named types* and instantiated generics in signature
positions remain unrepresentable, as before.

## Original report

Spec 8.5 says Go generic functions whose constraints are `any` or
`comparable` are callable in v1. The `import go` loader
(`internal/goimport/mapping.go` `mapSignature`) rejected *every*
signature that carries type parameters — `sig.TypeParams().Len() > 0`
or `RecvTypeParams` — with "generic Go functions are not supported
yet", so such members landed in the package table hidden and
reference sites got E0501.
