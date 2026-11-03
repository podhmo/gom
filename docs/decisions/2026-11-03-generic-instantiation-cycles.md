# Recursive generic calls must not grow a type argument (P2-5)

- 状態: 採用（[sketch: p2-5-generic-emission-verification](../sketch/2026-11-03-p2-5-generic-emission-verification.md) で検証）
- 日付: 2026-11-03

## Background

P2-5 emits top-level polymorphic functions and polymorphic ADTs as Go
generics: `fn wdepth (w: W a)` becomes `func wdepth[A any](w W[A])
int64`. A generic function may recursively call itself or other
generic functions; each call instantiates through
`CallInfo.TypeArgs`. Two recursive shapes exist:

- **uniform** — `fn f (x: a) = ... f y ...` where the call
  instantiates `f` at the same `a` (or a renaming of it). Go emits
  `f[A](...)`; the instantiation set is finite and valid.
- **growing** — the call instantiates a parameter under a type
  constructor: `fn wdepth (w: W a)` calling `wdepth inner` at
  `W (List a)`, or `f x = f [x]` at `List a`. The emitted Go —
  `wdepth[list.List[A]](...)` inside `func wdepth[A any]` — is
  rejected by `go build` with `instantiation cycle: A instantiated
  as ...List[A]`, because the set of required instantiations is
  unbounded (`A → List[A] → List[List[A]] → ...`).

`gom check` accepted both shapes, breaking the contract that an
accepted program compiles to Go. The `@tailrec` variant is worse:
tail-call lowering rewrites `f [x]` into `x = list.List[A]{x}`,
which is not even a call — Go reports an ordinary type mismatch
instead of an instantiation cycle, but the program is rejected all
the same.

## Decision

Non-uniform polymorphic recursion is rejected at check time with a
new diagnostic **E0416**, alongside the other v1 restrictions of
spec section 3.3 (local `let` generalization, `==` on type
variables).

The check builds a *parameter-flow graph* per file:

- Nodes are the `(declaration, type-parameter index)` pairs of the
  file-local generic `FnDecl`s (declared `fn`s, generalized
  lambda-`let`s, and generalized value-thunks — they all carry
  `TypeParams`).
- Inside the body of a generic `f`, every call site — `Apply`
  (`CallDirect`/`CallPartial`), `Var`, or `MemberExpr` — that
  instantiates a file-local generic `g` contributes an edge
  `f.a → g.b` for each rigid variable `a` of `f` occurring in the
  type argument `T` for `g`'s parameter `b`:
  - `T` is exactly `a` → a **renaming** edge;
  - `T` contains `a` under a constructor (`List a`, `(a, Int)`,
    `a -> Int`, ...) → a **growing** edge;
  - `T` contains no variable of `f` → no edge (a closed
    instantiation is a single finite node).
- A growing edge that lies on a cycle — its target can reach its
  source back — makes the required instantiation set unbounded. The
  call site carrying it reports E0416.

Renaming-only cycles are accepted: `f[A,B] → g[B,A] → f[A,B]` is a
permutation, exactly the shape Go compiles. Constructor applications
(`CtorApp`/`CtorRef`, `KindCtor` callees) contribute no edges —
instantiating the *type* `W (List a)` is legal Go inside a generic
declaration; only function instantiation cycles fail.

## Consequences

- E0416 does not by itself make tail-call conversion safe.
  `markTailCalls` rewrites a self-call into a `TailCall` — a loop
  that reloads the same parameter registers — which is well-typed
  only for the **identity** self-instantiation (`f[A,B] → f[A,B]`).
  Non-identity but still type-preserving instantiations this check
  accepts — a permutation `f[B,A]` (`f y x`), a contraction
  `f[A,A]` (`f x x`), a rotation `f[C,A,B]` — are legal Go as
  ordinary recursion but produce `x, y = y, x` shape mismatches in
  the loop. `markTailCalls` therefore additionally requires the
  call's `TypeArgs` to be the declaration's own `TypeParams` in
  order; non-identity self-calls stay `CallDirect` (amended
  2026-11-03 — the original text claimed type-preserving sufficed,
  which is false; the parameter registers keep the enclosing
  instantiation's types).
- Polymorphic recursion in the "let-polymorphism" sense (a `let`
  used at a type richer than its binding's) is unaffected — the
  rejected programs are precisely those Go cannot emit.
- v1 does not support growing polymorphic recursion. A future design
  could opt into whole-program monomorphization for such functions;
  it is recorded as future work, not a defect of this check.
