# Variadic Go calls: ArgToParam cannot represent the repeated tail

- 状態: 未決

## 状況

`CallInfo.ArgToParam` maps each applied argument to *one* declared
parameter index, requires distinct entries, and collapses to `nil`
both for the identity mapping and for anything unrepresentable
(internal/check/apply.go `normalizeMapping`, `checkGoApply`).

For a variadic Go call like `f(1, 2, 3)` against
`f(x int, rest ...int)`, arguments 1 and 2 both fill parameter slot 1.
The checker currently drops the mapping to `nil` because repeated
indices are unrepresentable — so `ArgToParam == nil` for a variadic
call means "see `Variadic` + `Arity`", not "identity".

Separately, the variadic element type is read *structurally*:
`sliceElemOf` assumes the last parameter is `Go.Slice e` (or `List e`)
and takes `e` as the element each extra argument must have. An unusual
imported signature that does not have that shape degrades to a fresh
meta.

## なぜ問題か

- Lowering must not rediscover calling conventions from names or
  shapes — that is the whole point of `CallInfo`. Today the rule for
  a variadic call is implicit: "when `Variadic` is set, arguments at
  index ≥ Arity-1 repeat the last parameter's element type". That is
  still derivable, but it is one more convention a consumer must know.
- `nil` ArgToParam is overloaded: identity vs. unrepresentable. A
  consumer that blindly treats nil as identity maps a variadic tail
  argument to a wrong (out-of-range or mismatched) parameter slot.
- The structural `Go.Slice e` assumption is a guess about how P1-9
  will import `...T`; if goimport models variadics differently (a
  dedicated `Go` ref, an `Ellipsis` flag on `FuncParam`), the check
  moves.

## 考えられる対応

- Record the mapping differently for variadics: e.g. keep per-arg
  indices but allow repetition (relax `Validate` for `CallGo` when
  `Variadic`), so `ArgToParam = [0, 1, 1, 1]`; or document that for
  `Variadic` calls arg `i` maps to `min(i, Arity-1)` and drop
  `ArgToParam` entirely for them.
- Give `Signature` a first-class variadic marker instead of inferring
  from the parameter type's shape (partly done: `Signature.Variadic`
  exists; the element-type read is the remaining guess).
- Revisit after P1-9 fixes how `...T` enters `types.FuncParam`.

## 関連

- internal/check/apply.go `checkGoApply`, `sliceElemOf`,
  `normalizeMapping`, `CallInfo.Validate`
- internal/check/env.go `Signature.Variadic`
- TODO P1-9 (goimport), docs/spec.md section 8.1.4
