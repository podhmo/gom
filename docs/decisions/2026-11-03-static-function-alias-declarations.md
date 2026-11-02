# Static function aliases use named canonical function wrappers

- 状態: 採用
- 日付: 2026-11-03

## Problem

`fn f (x: Int) -> Int = g x; let g = f` is accepted Gom, but
`var g func(int64) int64 = f` creates a Go initialization cycle:
the static dependency analysis follows `f` back to `g`.

## Proposal

Lower a top-level variable bound directly to a statically known local
function, including a chain of such aliases, to an arity-one named
canonical wrapper. Core `FuncDecl.ValueAlias` explicitly retains the
variable symbol and its function-value calling convention. Codegen
prints that declaration without deciding whether an initializer is pure.

The wrapper consumes only the first arrow and immediately applies that
argument to the original function value. Functions returning functions
therefore keep the execution time of their first declared chunk.

Only references to local, non-thunk function declarations (or alias
chains rooted in those declarations) qualify. Computed values, partial
applications, receiver selection and effectful initializers remain eager
`VarDecl`s. No initializer is moved wholesale to Go `init`, which would
otherwise disrupt dependencies between package values.

## Verification plan

Prototype direct and chained cyclic aliases, noncyclic aliases, higher
order use, multi-arity partial application and first-chunk side effects.
Keep `@tailrec` rejection of mutual recursion through aliases. Add Core
validation and generated Go golden tests, then compare evaluator and
compiled execution. Record results under `docs/sketch/` before adoption.

## Verification result

The prototype passed direct/chained cyclic aliases, higher-order application,
multi-arity partial application, exact eager-effect timing, Core validation,
and the existing `@tailrec` alias-cycle checks. See
[verification report](../sketch/2026-11-03-static-function-alias-verification.md).
Aggregate-projected function values are a separate reproduced task, P2-2c.
