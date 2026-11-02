# P2-1 let-polymorphism emission verification

Verification of the emission shape decided in
[decisions/let-polymorphism-emission](../decisions/2026-11-02-let-polymorphism-emission.md):
generalized top-level `let` bindings lower to generic Go functions —
lambda bindings directly, other syntactic values as niladic thunks —
with the per-use instantiation recorded as `TypeArgs` on the typed
AST and Core IR.

`go test ./...` green; `TestEvalMatchesCompiled/let_polymorphism`
runs one program through internal/eval and the compiled binary and
they agree; `go/types` typechecking of generated code passes under
the FuzzCompile oracle.

## What was verified

- `let id = fn x -> x` emits
  `func id[A any](x A) A { return x }`; `id 1` and `id "s"` in one
  file emit `id[int64](1)` / `id[string]("s")` — per-use
  `CallInfo.TypeArgs` (`testdata/golden/let_poly.gom`,
  `TestPolyIdentityUsedTwice`).
- `let e = []` emits `func e[A any]() list.List[A] { return
  list.List[A]{} }`; a reference emits `e[int64]()`. The evaluator
  registers the same decl and `forceThunk` applies the niladic
  closure on lookup, so eval and codegen agree.
- Declared generic signatures emit their own parameters:
  `fn len2 (xs: List a) -> Int` → `func len2[A any](xs
  list.List[A]) int64` (`FnDecl.TypeParams`). The fuzz bug
  "多相関数の型パラメータが生成 Go で宣言されない" is closed — its
  repros moved from knownBugs to ordinary seeds.
- Constructor instantiation uses the ADT's argument order, not the
  signature's occurrence order: `result.Err` is `e -> Result a e`
  but emits `result.Err[A, E]` (`ctorADTArgs`).
- Value restriction: `let y = f 1` stays a monomorphic `LetDecl`
  (`TestValueRestrictionApply`); `let r = Ref.new []` still reports
  E0407 on its unsolved meta.
- Local lets do not generalize (`TestLocalLetMonomorphic`); mutual
  recursion through declared signatures checks
  (`TestMutualRecursion`); a let referenced before its declaration
  stays monomorphic (`TestLetForwardRefStaysMonomorphic`).
- Inference failures are user diagnostics, not invariants: `fn a ()
  -> a = {}` is E0401 at check, `fn f(n:Int)->a=...` likewise — the
  E0901 knownBugs entries went stale and were removed.
- `let x : a = ()` (top-level or local) reports E0412 + the
  underlying E0401; `let x : List a = []` quantifies `a` as the
  binding's own type parameter.

## Findings worth noting

- **Type-variable scope chaining.** A `let` annotation inside a
  function body could not see the enclosing signature's variables:
  `fn f (xs: List a) = { let y : List a = xs; y }` minted a fresh
  `a` and reported "expected a, got a" plus E0412. `tscope` now
  chains to the scope of the signature whose body is being checked
  (`checker.curTScope`, set by `checkFnDecl`/`checkLambda` and by a
  `let` annotation around its own RHS), and `unboundVars` reports
  only variables the annotation's own scope minted (`sc.owned`).
  `let f : List a -> List a = fn xs -> { let y : List a = xs; y }`
  emits `func f[A any](xs list.List[A]) list.List[A]`.
- **Forward references stay monomorphic.** `c.vals` pre-registers a
  meta per let-bound symbol; the first `valOf` marks the symbol
  `referenced`, and a consumed binding cannot flip — the earlier
  use's constraints already committed to the meta.
- **Thunks are marked, not sniffed.** The `() -> T` shape alone
  cannot identify a thunk: a bodiless signature `let f : () ->
  List Int` declares the same shape, and `f` in value position is
  the *function*. `check.FnDecl.Thunk` now records the distinction —
  `checkLetDecl` sets it only on generalized value-lets — and both
  `valOf` (unwrap to `T`) and `lowerer.thunks` (`CallDirect`
  arity 0 vs `FuncRef`) consult the flag. The evaluator carries it
  as `VFunc.thunk`, so `forceThunk` fires only for generalized
  lets (`TestNiladicSigIsNotThunk`).
- **Module-name misuse is still a known bug.** `{fmt}` in
  expression/callee position reaches lowering as E0901 (the
  registered TODO "Go モジュール名の式・呼び出し位置での誤用が
  E0901 になる"); `fn a () -> a = {fmt}` additionally trips the
  Core residual invariant as a cascade of the same misuse. Neither
  is an inference failure — the fix belongs to that TODO.
- **`package main` + `fn main () -> Int` is invalid Go** — the
  fuzz seed caught that a non-`Unit` `main` is not an entry point
  (`FuncDecl.IsEntry` is the decided rule) and emits an ordinary
  `func main(...)`. A checker diagnostic for the shape belongs to
  the entry-point TODO, not to P2-1.

## Test coverage added

- `internal/check/check_hm_test.go` — 15 unit tests over
  generalization, occurs, value restriction, thunk shape,
  annotation-variable binding/unbinding and scope chaining,
  declared `fn` type params, forward references, mutual recursion.
- `testdata/spec/` — `ok_let_polymorphism`, `ok_let_value_thunk`,
  `ok_let_annotation_scope`, `E0401_self_application`,
  `E0401_let_nonvalue_mono`, `E0412_let_local_type_var`,
  `E0412_let_nonvalue`.
- `testdata/golden/let_poly.gom` — generated-shape review.
- `internal/build/compare_test.go` — `let_polymorphism`
  eval-vs-binary case.
- FuzzCompile seeds for the fixed repros (`fn a()->a={}`,
  `fn helper()->c=helper()`, the if/else-vs-type-var return).
