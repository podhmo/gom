# P2-5 generic emission gap audit and instantiation-cycle check

Date: 2026-11-03. Toolchain: Go 1.27.3 (linux/amd64).

## Scope and existing implementation

P2-5 — "emit top-level polymorphic functions and polymorphic ADTs
with Go type parameters" — was already substantially implemented:

- `check.FnDecl.TypeParams` / `check.TypeDecl.TypeParams` carry the
  quantified variables; `lower` copies them onto `core.FuncDecl` /
  `core.TypeDecl`; `codegen` emits `func f[A any](...)`,
  `type Pair[A, B any] interface{ isPair() (A, B) }`, generic
  variant structs with marker methods whose signatures mention every
  parameter (including phantom ones — `Phantom[A]` emits
  `isPhantom() A`), generic records and aliases.
- `CallInfo.TypeArgs`, `Var.TypeArgs`, `MemberExpr.TypeArgs` record
  the per-use instantiation; `codegen` emits explicit `f[T](...)`
  for calls, `FuncRef`/`CtorRef` adapters, partial applications, and
  generic thunks (`let none = None` → `func none[A any]()
  option.Option[A]`).
- `testdata/golden/poly_functions` and `variant_named_field` pin
  the emission; `TestEvalMatchesCompiled/poly_functions` compares
  the evaluator and the Go executable.

The audit therefore searched for paths where an accepted program
still produces Go the compiler rejects.

## Finding: non-uniform polymorphic recursion

`type W a = | WLeaf a | WNode (W (List a))` with

```
fn wdepth (w: W a) -> Int =
  match w { WLeaf x -> 0; WNode inner -> wdepth inner + 1 }
```

type-checks, emits `wdepth[list.List[A]](tmp4)` inside
`func wdepth[A any](w W[A]) int64`, and `go build` reports
`instantiation cycle: A instantiated as github.com/podhmo/gom/
stdlib/list.List[A]` — the required instantiation set
`A → List[A] → List[List[A]] → …` is unbounded. The `@tailrec`
variant `fn f (x: a) -> Int = f [x]` is worse: tail-call lowering
rewrites the call into `x = list.List[A]{x}`, a plain Go type
error rather than an instantiation cycle. Both violate the
"check-accepted ⇒ generated-Go compiles" contract.

Go itself accepts only *renaming* recursive instantiations
(`f[A,B] → g[B,A] → f[A,B]` is a finite permutation). And Go
accepts the non-uniform *type* recursion — `type W_WNode[A any]
struct{ V0 W[list.List[A]] }` compiles — so the restriction belongs
to function call type arguments only.

## Prototype and results

The draft [decision](../decisions/2026-11-03-generic-instantiation-cycles.md)
implements a parameter-flow graph in `check` (new diagnostic
**E0416**): inside each file-local generic `FnDecl`, every
`Apply`/`Var`/`MemberExpr` instantiation of another file-local
generic contributes an edge per caller variable in its type
arguments — renaming when the argument is exactly the variable,
growing when it sits under a constructor. A growing edge that lies
on a cycle is reported at the call site.

Verification matrix (`gom check` on one-file programs):

| program | expected | result |
| --- | --- | --- |
| `wdepth` (self at `W (List a)`) | E0416 | E0416 at `wdepth inner` |
| `@tailrec fn f (x: a) = f [x]` | E0416 | E0416 at `f [x]` |
| `f (x:a) = g [x]`; `g (x: List b) = f x` (mutual) | E0416 | E0416 at `f x` |
| `{ let g = f; g [x] }` (growth through local alias) | E0416 | E0416 at `f` in `let g = f` |
| `depth` on `Tree a` (uniform recursion) | accept | accept |
| `swap`/`swap2` mutual renaming cycle | accept | accept |
| `use (x:a) -> List a = id [x]` (growth, no cycle) | accept | accept |
| `{ let g = id; g x }` (uniform local alias) | accept | accept |

Each call site reports once: the `Apply` records the edge and the
`Var`/`MemberExpr` head of the call is skipped, so `wdepth` does not
double-report at the callee span.

Regression coverage added:

- `testdata/spec/E0416_self_apply.gom` — the `wdepth` case.
- `testdata/spec/E0416_mutual.gom` — the `f`/`g` mutual case.
- `testdata/golden/poly_adts.gom` — pins two-parameter and phantom
  parameter ADTs, a generic record, the non-uniform `W` type
  (type-level recursion is legal), a generic thunk, and uniform
  generic recursion on `Tree a`.
- `TestEvalMatchesCompiled/poly_adts` — evaluator and compiled
  binary agree on `1:one:two:2:42:1` for construction, field
  access, and matching across two instantiations of `Pair`.

## Remaining notes

- Cross-package polymorphic declarations still need `.gomi`
  (separate P-item). Within one file the check is complete: any
  foreign callee is a sink and cannot close a cycle.
- Whole-program monomorphization could one day support growing
  recursion; recorded as future work in the decision, not a defect.

## Follow-up (same-day review)

The review found a second emission gap E0416 does not cover:
`markTailCalls` rewrote *every* saturated self-call into a `TailCall`
loop, but the loop's parameter reload is only well-typed for the
identity instantiation. Non-identity self-calls the checker accepts —
`permute y x` (`f[B,A]` permutation), `contract x x` (`f[A,A]`
contraction), `rotate z x y` (`f[C,A,B]` rotation) — produced
`x, y = y, x` shape mismatches. Fixed in `internal/lower/expr.go`:
`markTailCalls` now requires `identityInstantiation(e.TypeArgs,
d.TypeParams)`; non-identity self-calls stay `CallDirect` and Go
emits ordinary recursion (`permute[B, A](y, x)` — verified `go
build` accepts the finite {A,B}/{B,A} instantiation set). An
`@tailrec` declaration whose self-call is non-identity surfaces
E0902 like any residual self-call.

Pinned by `testdata/golden/tailcall_instantiation.golden.go`
(identity → `for` loop; permutation/contraction → `CallDirect`) and
`testdata/spec/E0902_tailrec_instantiation.gom` (`@tailrec` +
permutation → E0902).
