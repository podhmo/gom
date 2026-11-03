# Aggregate-bound function values become named wrappers; residual initialization cycles are rejected

- 状態: 採用（P2-2c の実装・検証: [検証レポート](../sketch/2026-11-05-aggregate-function-bindings-verification.md)）
- 日付: 2026-11-05

## Problem

`fn count (n: Int) -> Int = if n == 0 { 0 } else { again (n - 1) + 1 }; let (again, _) = (count, 0)`
type-checks and `gom build` emits Go, but the generated code cannot
compile: the hidden tuple `var tmp = Tuple2{count, 0}` refers to
`count`, `count`'s body refers to the projection `var again =
tmp.V0`, and `again` refers back to `tmp` — a Go initialization
cycle. P2-2b fixed the same shape for `let g = f` (a `VarPat`
right-hand side) with a named canonical wrapper; projections through
aggregate initializers were explicitly left out
([verification](../sketch/2026-11-03-static-function-alias-verification.md)).

Go's rule (spec: package initialization) is what this lowering must
match: a package variable `x` depends on every variable referenced
by its initializer, where references are followed transitively
through the bodies of functions and function literals the
initializer mentions. Two consequences measured on the real
toolchain:

- A wrapper `func again(p) { return tmp.V0(p) }` does **not** break
  the cycle — `tmp` still reaches `count`, whose body reaches
  `again`, whose body reaches `tmp`. Only a wrapper that calls the
  *element's root function* — `func again(p) { return count(p) }` —
  cuts the variable edge, because function-to-function references
  are not initialization dependencies.
- A self-reference through a function literal is also a cycle
  (`var f = func() { f() }` — "f refers to itself"), so a cycle
  cannot be hidden inside a thunk either.

## Proposal

**Representation, decided in Core IR (design.md section 2.1
extended).** A variable bound by a top-level `let` pattern whose
bound type is a function type lowers to a canonical arity-one
`FuncDecl` (`ValueAlias`, the P2-2b shape) when its *element
expression* — the sub-expression of the initializer that the
binding's projection reads — is a pure reference rooted in a local,
non-thunk function declaration or in another such binding. The
wrapper body applies that element: `let (again, _) = (count, 0)`
emits `var tmp = Tuple2{count, 0}` (unchanged — the aggregate keeps
its eager initializer, its effects and their order) plus
`func again(p int64) int64 { return count(p) }`.

Element tracing composes only through shapes whose structure is
statically knowable — the same purity discipline as P2-2b:

- `VarPat` and `AsPat` record the current expression;
- `TuplePat` × tuple literal, `RecordPat` × record literal (fields
  matched by symbol), `ListPat` prefix elements × list literal, and
  `CtorPat` × a saturated constructor application of the same
  constructor descend into matching sub-expressions;
- non-converting `Ascribe` wrappers and block tails are transparent;
- anything else (a `Var`, a call, an `if`, an update literal, an
  or-pattern alternative) stops the trace — those bindings keep the
  eager projection `VarDecl` they have today.

Callers are untouched: the bound symbol keeps `KindVar` and its
`Var` / `CallFunc` conventions, so call sites, value positions and
the `@tailrec` reference graph see the same shape as P2-2b aliases.
For a tested pattern (`let Box f = e`), the destructure match is
still emitted — discarding its bound-vars tuple when every bound
variable became a wrapper — because an invalid aggregate must panic
at initialization, not when the wrapper is first called.

The same rule applies to a plain `let g = e` whose `e` carries
evaluation — i.e. the trace peeled a block to reach the element
(`let g = { mark (); count }`): the initializer keeps a hidden
eager `VarDecl`, the role the destructure's shared temporary plays,
so its statements still run at package initialization. The
alternative — declining the alias unless `e` is itself a bare
reference — would keep the block eager too, but it re-diagnoses
even a trivially transparent `let g = { count }` as E0907 whenever
`count` refers back, and splits the VarPat path from the
destructure one for no semantic gain. (Review amendment: the first
cut emitted the `FuncDecl` alone for a VarPat binding and silently
dropped the block's statements — a real evaluation-order
regression, caught and pinned by `TestAggregateAliasVarBlockRuns`.)

**Error boundary — E0907.** Whatever is left is genuinely
unorderable: Go cannot express "this variable is needed only by a
function body", so a package-variable cycle that survives the
wrapper rule has no valid initialization order. Such programs
previously passed `gom build` and failed `go build` with
machine-generated temporary names (`initialization cycle for
tmp4`). Lowering now detects the residual cycle on the Core IR it
produced — variable-to-variable edges projected through function
declarations, the same reading Go applies — and reports E0907
naming the user-visible bindings on the cycle. Rejected shapes
include:

- a cycle through a non-function binding
  (`fn g = x + n; let (f, x) = (g, 41)`),
- a cycle through an element the trace cannot reach
  (`let (f, _) = (mk (), 0)` where `mk`'s body mentions `f`),
- a cycle through a second-order alias
  (`let pair = (count, 0); let (a, b) = pair`),
- a cycle through a function literal stored in an aggregate
  (`let (g, _) = (fn n -> f (n - 1), 0)` — a lambda is never a
  traceable element, so the binding stays a variable on the cycle;
  note a *self*-referencing lambda `fn x -> f x` cannot even be
  written — it fails type inference first — but the check covers
  the shape regardless).

The analysis is file-local like `@tailrec`'s R4 (Go forbids import
cycles, so no package-variable dependency can cross packages) — and
per-file: Go analyzes initialization per package, so once V-6 lands
a cycle spanning two same-package files would still reach
`go build` undiagnosed until the graph is built over all of the
package's Core files.

## Verification plan

- Lower-level tests: destructured tuple/record/single-variant-constructor
  binds become `ValueAlias` FuncDecls; untraceable and non-function
  binds stay `VarDecl`s; each residual shape above reports E0907.
- Golden output for the repro and the aggregate shapes; execution
  regressions (evaluator vs compiled binary) for allowed cycles —
  including that aggregate side effects still run eagerly at package
  initialization — and spec fixtures (`testdata/spec/`) asserting
  E0907 for the rejected shapes.
- Record results under `docs/sketch/` before adoption — done in
  [verification](../sketch/2026-11-05-aggregate-function-bindings-verification.md),
  which also records the `gom check` visibility boundary (lowering
  diagnostics, like the E09xx family, surface on `gom build` only)
  and the second-order-alias limitation.
