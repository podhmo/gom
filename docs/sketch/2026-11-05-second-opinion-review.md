# Second-opinion review: findings and dispositions

A second-opinion agent reviewed the codebase on 2026-11-05 and
reported three reproduced bugs plus architectural recommendations.
Each finding was re-verified on main (de37bbc) before filing.

## Verified reproductions (confirmed on main)

### 1. Function-type identity vs generated-Go representation — CONFIRMED, contract violation

```gom
fn make (x: Int) -> (Int -> Int) = ...
fn stage (f: Int -> Int -> Int) -> (Int -> Int) = f 20
-- `stage make` passes `gom check`/`build`, but the output mixes
   func make_(x int64) func(int64) int64   (curried emission)
   func stage(f func(int64, int64) int64)  (flattened emission)
```

`check` flattens arrow chains and treats the types as identical
(`internal/check/unify.go` `flattenFunc`); codegen picks the emitted
shape from the declaration's arity — so the same type has two Go
representations and no conversion at the boundary. This is the same
class of contract violation as the P2-1 init-cycle bug: check-ok ⇒
output must typecheck.

Design nuance the reviewer flagged and we confirmed matters: with a
side-effecting `make`, `f 20` must run `make`'s body *at the first
argument*. An adapter that defers the call until the remaining args
arrive changes semantics — the eta-wrapper must apply eagerly.

Filed: TODO bug "関数型の同一性と生成 Go の表現が一致しない" —
near P2-2's territory; fix = separate (semantic fn type) / (declared
arity) / (Go representation) and make representation conversion a
Core IR decision.

### 2. `==` accepted on named types containing functions — CONFIRMED, spec violation

`type Box = {f: Int -> Int}` + `x == x` type-checks and emits
`rt.Equal(x, x)`. spec 3.3 restricts `==` to function-free types.
`internal/check/unify.go` `hasFuncOrVar` walks `types.Con` arguments
but never expands a named type into its record fields / ADT
payloads. Needs substitution into the declared body plus cycle
handling, shared across `==`/`!=`/`assert_eq`.

Filed: TODO bug.

### 3. `rt.Equal` inherits `reflect.DeepEqual`'s pointer shortcut — CONFIRMED, semantics bug

`let n = math.NaN(); let xs = [n]` prints:

    n == n      → false
    xs == xs    → true   (!!)
    [n]==[n]    → false

`DeepEqual` skips elementwise comparison for the same backing slice,
so structural equality depends on sharing. The real fix is per-type
comparison generation (lists elementwise, records fieldwise, ADTs
tag+payload) driven by the same type information as the
comparability check — not a better generic comparator. Spec needs
explicit rules for NaN, nil-vs-empty, GoError, Ref.

Filed: TODO bug.

## Architectural recommendations → filed as

- **Defer call-finalization until after constraint solving**: keep
  unresolved apply info during inference, finalize method
  selection/calling-convention/type-args/arg-mapping in one pass,
  contract = everything resolved before lower. Already the shape of
  the deferred-receiver bug — appended to that TODO entry.
- **Go boundary copy depth**: `slices.Clone` is shallow; decide the
  guaranteed depth before P3 widens interop →
  `docs/problems/2026-11-05-go-boundary-copy-depth.md`.
- **Multi-file + .gomi vertical slice earlier**: same-package scope
  sharing is an open question affecting `.gomi`, type identity, init
  order, name collisions → TODO "V-6 複数ファイル + .gomi の縦断検証".
- **More combinations, not more tests**: V-3 (done, in review),
  V-4/V-5 filed earlier; new suggestion — side-effect ORDER logging
  tests (direct vs alias vs higher-order vs partial application),
  semantics-preserving-rewrite tests (add annotation, move decl to
  another file), spec-example status markers. Partially absorbed
  into V-3/V-4 scope; the rewrite-tests idea is new and cheap to
  fold into the compare harness later.
- **TODO grooming**: mark P2/P3 entries with 未着手/一部対応/利用可能
  status — adopted lightly (P2-1 ✅ annotations).

## Priority order the reviewer proposed (endorsed here)

1. Function-representation contract + side-effect timing.
2. Comparability check + structural equality generation.
3. Post-inference call finalization.
4. Multi-file / `.gomi` vertical slice.
5. Differential and combination testing (V-3..V-5 already queued).
