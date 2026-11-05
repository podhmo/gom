# Sketch: numeric arithmetic is per-operation machine arithmetic — verification log

Verification report for
[decision: numeric-arithmetic-per-operation](../decisions/2026-11-05-numeric-arithmetic-per-operation.md)
— resolving the registered `knownDivergences` entries where a
literal-only numeric expression emitted a Go constant expression and
Go's exact-arithmetic fold answered differently than the evaluator's
per-operation `float64`/`int64` semantics
([problem](../problems/2026-11-02-float-const-fold-arithmetic.md)).

## Mechanism

`internal/lower` (`binaryExpr`, `eqExpr`): when both lowered operands
of a fold-sensitive operation are `core.Lit`, the left operand is
bound to a fresh temporary (`bindAtom`). A variable is never a Go
constant, so the emitted operation evaluates at run time — each
literal rounds once to its machine type at the bind, exactly the
evaluator's reading. `litFoldDiverges` limits this to the op/type
pairs whose fold can diverge:

- `Float`: `+ - * / < <= > >= == !=` — exact-rational folding rounds
  once instead of once per operation, and merges literals a single
  float64 also merges
- `Int`/`Int32`/`Int64`: `+ - * /` — the folded value can be a
  constant overflow the emitted Go type rejects outright (a
  divergence worse than an ulp: the program did not build)
- not `%` (its exact result always fits), not integer comparisons
  (exact = machine), not non-numeric folds (same value)

Only `core.Lit` operands can emit Go constants: `Unary`, `Binary`
and `Conv` are not atoms, so `asAtom` already routes them through
temporaries — `-x`, nested `a + b` subtrees and `float64(3)`
materializations are non-constant without extra work. Transparent
ascription lowers through to the literal, matching `check.constFold`'s
constant-expression domain.

## Evidence

The prototype below generated, for `fn f () -> Float = 869.32 *
38.29`:

```go
tmp1 := 869.32
return tmp1 * 38.29
```

and executed the two registered divergence programs end to end
(`go run` on the generated file):

| seed | program | eval (before & after) | compiled before | compiled after |
| --- | --- | --- | --- | --- |
| 52 | `"${(869.32)*(38.29)}hello hello"` | `33286.262800000004hello hello` | `33286.2628hello hello` | `33286.262800000004hello hello` |
| 93 | `"lit${(24e1)/(959.07)}v="` | `lit0.2502424223466483v=` | `lit0.25024242234664834v=` | `lit0.2502424223466483v=` |

A scratch module exercising the edges printed, identical to the
evaluator's answers:

- `x * 38.29 + 1.5` (x = 869.32): `33287.762800000004` — per-op
- `9223372036854775807 + 1`: `-9223372036854775808` — wraps; the
  folded constant `9223372036854775808` used to fail `go build`
- `0.1 + 0.2 == 0.3` and `<= 0.3`: `false`, `false` — exact folding
  would answer `false` / `true` (exact `0.1 + 0.2` is `0.3`)
- `1.0 / 3.0 * 3.0`: `1` — same as the evaluator's own per-op result

`internal/build/compare_test.go` gained the `numeric_literal_arith`
case pinning all of the above (`want:
33286.262800000004/0.2502424223466483/false/false/-9223372036854775808/33286.262800000004`,
including a let-bound-variable spelling giving the same product),
and the two `knownDivergences` registrations were retired — the map
is empty.

Golden blast radius: `float_int_literal.golden.go`
(`float64(1) / float64(2)` binds `tmp1`) and `task_spawn.golden.go`
(`6 * 7`, `1 + 2` under a `go`/lambda) — the only literal-only
numeric operations in the suite. No other generated output changed.

## Residuals / non-goals

- References to Go constants (`math.Pi`) also emit Go constants, but
  the evaluator cannot evaluate Go symbols at all
  (`CodeUnsupported`), so no clean-execution divergence can form —
  out of scope.
- The `E0414` constant-zero-divisor diagnostic is unchanged: it is
  a legality rule over `check.constFold`'s source-level domain,
  independent of value semantics — rejected programs never reach
  lowering, so whether the emitted code is constant there is moot.
- `check.constFold`'s doc comment was reworded to describe its
  domain as a source-level legality notion — the emitted surface
  for `Unary`/`Binary`/`Conv` operands was always non-constant and
  is unchanged.

Verified with `go test ./internal/build` (`TestEvalMatchesCompiled`,
`TestKnownDivergences`) and `go test ./...` on the worktree.
