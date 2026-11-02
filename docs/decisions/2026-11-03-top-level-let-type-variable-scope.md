# Top-level monomorphic lets cannot capture declaration type variables

- 状態: 採用
- 日付: 2026-11-03

## Background

The v1 value restriction only generalizes a top-level lambda or a syntactic
value. A bare function alias, `let g = f`, is monomorphic. When an earlier
generic declaration uses `g`, unification can bind `g`'s preregistered meta
to that declaration's rigid variables. Emitting the alias as a package
variable would then mention type parameters outside their declaration and
can also create a Go initialization cycle.

## Decision

After all declarations and finalization have solved inference variables,
reject any rigid variable in an ordinary top-level `LetDecl` binding type
with E0412. Checking only while visiting the binding misses later generic
uses that solve its metas. A successfully generalized binding already
returned a `FnDecl`, so the check only affects declarations that have no
quantifier to own those variables. An explicitly annotated binding keeps
the existing annotation diagnostic. Local lets retain access to their
containing function's type variables and are unaffected.

Do not make function references generalize implicitly: spec 3.3 only
permits `fn` definitions, lambdas and the listed syntactic values. An
ordinary alias may still specialize at a concrete type; an unconstrained
alias continues to report E0407. Forward polymorphic aliases require a
separate inference design and are outside this implementation.

## Verification plan

Compile the reported f → g → f reproducer and a destructuring variant;
require E0412 and no output, rather than unbound Go type parameters.
Exercise polymorphic functions at multiple types, partial application,
nested captured closures, concrete function aliases and declaration-chunk
evaluation timing with generated Go and evaluator comparison. Preserve
local access to an enclosing rigid type variable. Record results in
`docs/sketch/` before adopting this decision.

## Verified

[Prototype results and remaining scope](../sketch/2026-11-03-poly-functions-verification.md)
record the negative spec fixtures, generated golden, evaluator/binary
comparison, and the separate monomorphic-alias initialization-cycle bug.
The change preserves the existing v1 value restriction.
