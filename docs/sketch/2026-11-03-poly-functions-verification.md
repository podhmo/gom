# P2-2 function values and type-variable scope verification

Date: 2026-11-03. Toolchain: Go 1.27.3 (darwin/arm64).

## Scope and existing implementation

The canonical curried function-value representation, declaration-arity
adapters, generic function instantiation, partial application and captured
closures already exist. This round adds regression coverage and closes the
reported package-scope type-variable leak. It does not change spec 3.3's
value restriction: only `fn` definitions, lambdas and the enumerated
syntactic values generalize. Bare function aliases do not generalize.
The previous TODO suggested generic alias thunks as a possible repair;
that would widen the current spec. This implementation instead diagnoses
unowned rigid variables with existing E0412.

## Prototype and results

The draft [decision](../decisions/2026-11-03-top-level-let-type-variable-scope.md)
was committed before implementation, then moved to prototype. The prototype
initially checked scope while visiting each let. A later generic use can
still solve the let's meta to another declaration's rigid variable, so the
adopted implementation checks after finalization instead.

- `testdata/spec/E0412_forward_alias`: the reported `fn f (x: a00) -> a01
  = { g x }; let g = f` now reports E0412 for a00 and a01 at g's binding.
- `E0412_forward_alias_tuple`: the same leak through tuple destructuring
  is rejected at the binding pattern.
- `E0412_late_alias_scope`: `fn id (x: a) -> a = x; let alias = id;
  fn use (x: b) -> b = alias x` catches a rigid variable introduced by a
  later declaration, which a declaration-local check misses.
- `testdata/golden/poly_functions` pins explicit Go instantiations,
  canonical nested adapters, partial application and a generic closure
  capturing a locally bound value of its enclosing declaration's type.
- `TestEvalMatchesCompiled/poly_functions` compares the evaluator and Go
  executable: polymorphic `id` at String and Int, generic `choose` as a
  partial application, a normal specialized alias, a generic capturing
  closure, and a concrete alias used before its binding all return
  `poly:2:40:3:captured:5`.
- Existing `TestEagerFnChunkTiming` still proves that the first declared
  arity chunk executes immediately, before the residual function is used.
- The `polyvar_let` fuzz seed remains active; its known-bug exemption is
  removed because clean rejection satisfies the compile contract.

Focused spec/golden, runtime comparison, eager-timing tests, and all tests
in `internal/check` and `internal/build` pass. Final required checks all
pass: `gofmt -l .` produces no output, `go vet ./...` exits successfully,
and `go test ./...` passes all packages (including executable tutorial
verification). The spec itself is unchanged.

## Remaining actionable bug: monomorphic cyclic aliases

This independent existing failure remains and is tracked as P2-2b. P2-2
therefore stays partially complete rather than claiming all function-value
programs work.

```gom
package a

fn f (x: Int) -> Int = g x
let g = f
```

`go run ./cmd/gom build /private/tmp/gom-poly-monomorphic-cycle.gom`
reports success and emits `func f(x int64) int64 { return g(x) }` plus
`var g func(int64) int64 = f`. Running
`go test /private/tmp/gom-poly-monomorphic-cycle.go` reports
`initialization cycle for g`, with g → f → g. This has no unbound type
variables and is not repaired by a type-variable-scope diagnostic.
Fixing the alias representation must preserve ordinary alias behavior,
partial application and the @tailrec reference graph. The diagnostic fix
in this round intentionally leaves that independent implementation work
for a separately reviewed task.

## Deliberately excluded work

Local let polymorphism remains forbidden by v1; mutual/polymorphic
inference across forward declarations requires a separate design.
Cross-package polymorphic values and `.gomi` remain under their existing
P4-1/V-6 tasks. Label semantics are P2-3 and generic output completion is
P2-5; neither is marked complete by this audit.

## Fresh review and implementation decision

A fresh `review_poly_functions` agent reviewed head `d4b278f` and found
no integration-blocking issue. The review confirmed the post-finalization
check, the tracking of monomorphic bindings, and the diagnostic division
between annotated and unannotated lets. It also verified the normal alias,
generic function, closure and partial-application regressions; gofmt,
vet, tests, diff checks, eager timing and evaluator/binary comparisons
passed during review.

As the implementation owner, I judge that no additional code change is
required for P2-2a. The separately recorded monomorphic alias cycle is
P2-2b and is being handled by a fresh implementation agent; it does not
change this scope-validation decision. This review follow-up changes only
this report, so the passing implementation checks are not rerun.
