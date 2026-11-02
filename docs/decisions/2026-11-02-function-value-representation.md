# Function value representation: canonical curried form

- 状態: 採用
- 日付: 2026-11-02
- Supersedes: [関数値の表現](2026-11-01-function-representation.md) のうち関数値の Go 表現（flat `func(T1, ..., Tn) R`）とそれに伴う呼び出し規約

## Context

`types.Equal` flattens arrow chains, so `A -> B -> R` and
`A -> (B -> R)` are the same semantic type — one semantic type,
written two ways. Under the P0-2 representation the *generated* Go
shape of a function value was chosen by the callee's declared arity,
not by the type: `make` emitted `func(x int64) func(int64) int64`
while `stage`'s parameter was `func(int64, int64) int64`. The
checker accepted `stage make` and the generated Go then failed to
type-check — a violation of the compiler's core contract that every
accepted program produces type-checking output
([sketch: second-opinion review, finding 1](../sketch/2026-11-05-second-opinion-review.md)).

Flat values cannot repair this: a declaration
`fn make (x) -> (Int -> Int)` must keep its written arity so its
body — including side effects — runs when the *first* argument
arrives (`func make_(x) func(y)`), but the semantic type
`Int -> Int -> Int` spelled flat needs `func(x, y)`. One type, two
obligatory shapes, and no adapter can stand between them — an
adapter that waits for all arguments before calling `make_` defers
`make`'s body past the first application, changing observable
evaluation order.

## 決まったこと

- A function **value** of type `T1 -> ... -> Tn -> R` is exactly one
  Go func per arrow: `func(T1) func(T2) ... R`. The representation
  is a pure function of the *flattened* arrow chain, so every
  spelling `types.Equal` identifies emits the identical Go type and
  no conversion ever stands between them.
- A function **declaration** still emits at its declared arity:
  `fn f (a) (b) = e` is `func f_(a, b) R`. The first call
  `f(x, y)` runs the body eagerly, as before.
- A bare `FuncRef` is legal only at declared arity 1 — `func f_(a)
  R'` already *is* the canonical chain. Any larger declared arity
  adapts through a nested closure
  `func(p0) func(p1) ... { f_(p0, p1, ...) }`: each arrow takes one
  argument and the declared chunk fires when its last parameter
  arrives, preserving the eager body timing.
- `CallFunc` applies exactly one argument per step — `f a b` is
  `f(a)(b)`, matching the evaluator's one-argument apply. A partial
  application needs no extra Closure: it is simply a value of the
  residual arrow type.
- A `Closure` binds at most one parameter per level; an n-arrow
  value nests n levels. `func() R` remains the zero-argument value.
- The P0-2 measurements against currying (intermediate allocations
  ~43x, higher-order loops ~87x, `Uncurry` adapters for Go APIs) are
  accepted costs. They compared working representations on
  performance; the flat choice has since been shown to break the
  check-ok ⇒ generated-code-typechecks contract, which outranks
  them. Allocation pressure is also bounded differently here:
  saturated direct calls to declarations never touch the chain, and
  only *value* applications chain one call per arrow — the path the
  evaluator already takes.
- Labels are still erased; Go parameter order is declaration order.
- Package-external function values are canonical too, so a `.gomi`
  signature keeps spelling the semantic type and the consumer's
  lowering adapts as needed (cross-package function values are still
  limited for other reasons; see TODO).
- The adapter's firing `CallDirect` carries `Adapter: true` so
  downstream passes can tell the synthesized firing call from a
  user-written call site — `@tailrec`'s requirement pass reads it as
  the R3 value escape (E0903, no `go` exemption) rather than a call
  counted by R1/R2. Only the synthesized adapter is flagged;
  hand-written eta (`fn a b -> f a b`) stays an ordinary CallDirect
  and is reported by R2 as before.
- A lambda whose flat signature is longer than its written parameter
  list (`fn a b -> fn c -> ...`, `fn a b -> add2`, `fn a b -> add2
  a`) still lowers one level per written parameter; the body's own
  lowering supplies the remaining arrows. The degenerate single-level
  path is only for a non-function type, a parameter-less lambda, or
  more written parameters than the signature supplies.

## Verified

Implemented and checked on this branch:

- `internal/build/compare_test.go` — `fn_value_repr` compiles and
  runs the motivating `stage make` (evaluator and binary agree);
  `fn_value_boundaries` exercises records, variant payloads,
  multi-arity references (eta adapter) and partial application.
- `internal/build/eager_test.go` — `TestEagerFnChunkTiming` proves
  `make`'s body fires during `stage`'s call on both sides: the
  evaluator reports the `todo` panic and the binary panics before
  printing "after".
- `internal/build/compare_test.go` — `lambda_returns_function`
  compiles and runs the longer-flat-signature lambdas
  (`fn a b -> fn c -> a + b + c`, `fn a b -> add2`,
  `fn a b -> add2 a`); evaluator and binary agree.
- `internal/lower/tailrec_test.go` `TestTailRecFuncRef` /
  `TestTailRecAdapterSpawn` and
  `testdata/spec/E0903_tailrec_let_alias_multi_arity` — a multi-arity
  `let g = f` reports E0903 at the reference (inside `go` included),
  not E0902, and does not count as a call for R1/R2.
- `internal/core/validate.go` — the invariants are structural:
  `FuncRef` requires declared arity 1, `CallFunc` requires exactly
  one argument, `Closure` binds at most one parameter.
