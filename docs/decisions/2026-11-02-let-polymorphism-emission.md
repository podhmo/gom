# Emission shape for let-polymorphic bindings (P2-1)

- 状態: 採用（[sketch: let-polymorphism-emission](../sketch/2026-11-02-let-polymorphism-emission.md) で検証）
- 日付: 2026-11-02

## Background

P2-1 adds Hindley–Milner let-polymorphism to the checker
(`let id = fn x -> x` gets `∀a. a -> a` and is usable at several
types). The value-restriction decision
([2026-11-01-value-restriction](2026-11-01-value-restriction.md))
already settled *when* a top-level binding generalizes: only when the
right-hand side is a function definition or a syntactic value. What
it did not settle is how a generalized binding is *emitted*: a
`let` whose type mentions generalized variables cannot become a plain
`var x T` because the generated Go would mention undeclared type
variables.

## Decision

A generalized top-level `let` is emitted as a **generic Go function
declaration**, reusing the type-parameter machinery the ADT and
`fn` paths already have:

- `let f = fn x -> e` (a lambda or function RHS, value restriction
  satisfied) becomes `func f[T...](params) ret` — an ordinary
  function declaration with the generalized variables as type
  parameters. The symbol's kind flips to `KindFunc`, and every
  reference instantiates it through `CallInfo.TypeArgs` /
  `FuncRef.TypeArgs`.
- `let x = <syntactic value>` with generalized variables (e.g.
  `let xs = [] : List a`) becomes a **thunk**: a niladic generic
  function `func x[T...]() T` whose body is the value. References
  call it: `x` reads as `x[T]()`. The thunk is lazy at run time and
  evaluates once per reference, which keeps the evaluator and the
  generated code in agreement (the evaluator binds the same
  declaration as a parameterless function and forces it per use).
- A `let` that was **already referenced** earlier in the file
  (forward or recursive reference through the pre-registered meta)
  cannot generalize — earlier uses recorded a monomorphic shape. It
  still flips to a function declaration when nothing remains to
  quantify (`rigidVars(sig) == ∅`, i.e. the earlier uses' shape *is*
  the declaration's): `var f = <func literal>` puts the body inside
  Go's initialization-dependency analysis, where every (mutually)
  recursive reference is an initialization cycle, while a `func`
  declaration has no initializer and escapes it. Applications
  recorded while the symbol was a variable (`CallUnknown`) are
  re-classified against the declared arity — saturated or
  over-applied becomes `CallDirect`, under-applied `CallPartial` —
  and bare `Var` references need no rewrite because lowering reads
  the symbol's kind. A referenced binding whose signature still
  mentions rigid variables (an annotation's own `a`, or an unsolved
  meta that survived) stays a `var` as before; the annotation's
  variables then report E0412/E0407. (Amended 2026-11-02 after the
  P2-1 review found the init-cycle failure — originally this bullet
  said every referenced `let` stays a `var`.)
- A `let` whose annotation names variables but cannot flip (a
  non-variable pattern, a non-value RHS, or an earlier use) reports
  **E0412 — unbound type variable**: `let f : a -> a = g` has no
  declaration to bind `a` to.
- Declared `fn` signatures quantify their own variables — the same
  `TypeParams` list — so `fn len2 (t: Tree a) -> Int` emits
  `func len2[A](t Tree[A]) int64`. Type variables inside a function
  body (`x : a` where `a` is the fn's parameter) resolve to the
  enclosing rigid variable: the checker's annotation scope chains
  to the scope of the signature being checked. A `let` annotation
  inside a body cannot *introduce* variables (local lets do not
  generalize) — a name the enclosing signature does not declare is
  E0412.

## Consequences

- The checker records generalized variables on `check.FnDecl.TypeParams`;
  `check.Var`/`check.MemberExpr` and `CallInfo` carry the per-use
  instantiation as `TypeArgs`. Lowering maps them straight onto
  `core.FuncDecl.TypeParams` and the existing `TypeArgs` fields of
  `CallDirect`/`FuncRef`/`CtorApp` — the same path generic ADTs and
  generic `fn` signatures already take.
- A polymorphic value binding is a function in generated code; the
  thunk call `x[T]()` is the concrete shape Core IR records, so
  codegen never decides the semantics.
- `let r = Ref.new []` (a non-value RHS) does not generalize:
  the unsolved element meta reports E0407, matching the spec.
- Recursive `let`-lambdas are monomorphic during their own body
  (the `referenced` mark fires), matching "no polymorphic recursion".
  They still emit `func` declarations — recursion through a `var`
  initializer is a compile-time initialization cycle in Go, so a
  monomorphic recursive `let` flipping to `KindFunc` is load-bearing,
  not cosmetic. Self-recursive calls re-classified to `CallDirect`
  are eligible for the tail-call loop rewrite exactly like declared
  `fn`s.

## Deferred to P2-2

- Local `let` polymorphism (relaxed value restriction or otherwise).
- Generalized bindings referenced before their declaration (out of
  scope: requires a two-pass or constraint-graph checker).
- Polymorphic recursion and mutual recursion.
- Monomorphization instead of Go generics — the generics route is
  what the existing ADT machinery already implements; specializing
  per-use monomorphs remains the alternative if a constraint type
  parameter becomes necessary.
- Cross-package generalized `let` references: same-package lowering
  covers the thunk call; `.gomi` members already lack a lowering
  path for `var` values today.
