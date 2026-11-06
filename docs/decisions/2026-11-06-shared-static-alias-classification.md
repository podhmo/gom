# Share the static-alias classification between lower and eval's init graph

- 状態: 採用（prototype で検証済み — [sketch](../sketch/2026-11-06-shared-static-alias-classification-verification.md)）
- 日付: 2026-11-06

## Context

[2026-11-06-eval-init-ready-order](2026-11-06-eval-init-ready-order.md)
§5 recorded a residual: a `check.LetDecl` that lower emits as a
ValueAlias `FuncDecl` is a var node in the evaluator's
initialization graph but a function node in core, and the
divergence is observable two ways — an effectful alias initializer
(`let f = { …; count }` splits hidden temp var + wrapper, so the
compiled caller waits on what the wrapper body reaches, not on f's
var node: `w,a,f-tmp` vs eval `w,f,a`) and a pure alias on a
function-path loop (`let f = g` + `fn g = { ignore (f); … }` gives
f a self-dependency through varNode — never ready, leftover-forced:
`z,w` vs compiled `w,z`). The recorded fix path was to lift
`internal/lower/alias.go`'s classification — `bindElems`,
transitive `static()`, `initNeedsEval`, the arity/thunks index —
somewhere both consumers see it, since it already runs entirely on
`check.Decl`/`check.Expr`.

## Decision

1. **The shared home is a new leaf package,
   `internal/staticalias`.** The classification depends only on
   `check`, `resolve` and `types`, so it sits below both consumers
   and the phase direction stays one-way (eval is a leaf, lower is
   mid-pipeline). `internal/check` would also be dependency-legal
   and was considered, but check is the checker-phase surface other
   work lands in; a dedicated package keeps the classification's
   contract — *which bound symbols become ValueAlias FuncDecls* —
   named and small.
2. **`Classify(decls, universe []check.Decl) *Index` splits the
   scan set from the function index.** `decls` is the set whose
   `let`s are alias candidates; `universe` supplies the local
   non-thunk-function index (declared arity + `check.FnDecl.Thunk`)
   the transitive `static()` root test consults. lower hands
   `f.Decls` and `f.Decls + env.Siblings`; eval hands each file's
   decls and the whole unit's decls. The split is load-bearing, not
   cosmetic: an alias chain does not cross a file boundary —
   `let h = f` where `f` is a *sibling file's* alias binding is a
   plain `var h = f` in the emitted Go (lower never sees the
   sibling's binding shape), so a classification scanned over the
   whole unit indiscriminately would mark `h` an effectful alias
   and diverge (`v,h,z` vs compiled `h,v,z`, pinned by
   `TestDiffInitReadyOrderAliasAcrossFiles`).
3. **eval mirrors the emitted node shapes, it does not re-derive
   them.** `buildInitGraph` reads the classification per file: an
   alias-bound symbol registers as a function node whose body is
   the peeled element the wrapper applies (refs propagate to
   referencers like a FnDecl body); a VarPat pure alias emits no
   var node; an effectful alias initializer keeps its hidden-temp
   var node at the declaration slot; non-alias bound symbols stay
   on the shared variable node (projections are pure).
4. **eval binds the wrapper eagerly, like a FnDecl.** `NewFiles`
   binds each alias symbol to a function value that evaluates the
   element — a pure `Var` reference — and applies the result, so a
   call through the alias from an earlier initializer never forces
   the hidden temp's effects. The eager bind is what makes (a)
   observable-correct: without it `f ()` inside `a`'s initializer
   would force `f`'s block early (the pre-lift `w,f,a`).

## Consequences

- `internal/lower/alias.go` is deleted; `File` consumes
  `Classify`'s three maps where `indexStaticAliases` wrote them —
  same classification, no lowering change.
- The classification now has two readers that must agree; the
  per-file scan boundary is the agreement surface — a future
  "alias chains across files" feature changes `Classify`'s
  contract for both.
- The §5 residual closes: both divergent shapes compare equal
  eval-vs-compiled in `internal/build/diff_initorder_test.go`, and
  the unit pin flips from `[z w]` to `[w z]`.

## Alternatives considered

- **Put the classification in `internal/check`.** Dependency-legal
  (eval and lower both already import check) and check-level
  pattern/expr facts live there naturally — rejected this wave:
  check is the checker-phase surface other lanes edit, and the
  classification answers a lowering question ("what shape does
  this decl emit"), not a typing one.
- **Duplicate the analysis inside eval.** Rejected in the original
  decision — a parallel re-implementation drifts from the lowering
  it must mirror; the lift exists precisely to avoid that.
- **Model the alias split without the eager wrapper bind** (graph
  shapes only). Fixes (b) but not (a): a read of `f` would still
  force the temp's initializer during an earlier var's init —
  half the residual.

## Prototype / verification

[docs/sketch/2026-11-06-shared-static-alias-classification-verification.md](../sketch/2026-11-06-shared-static-alias-classification-verification.md):
`TestInitReadyFrontier` pins flip to the compiled orders, the
`TestDiffInitReadyOrder{ValueAlias,AliasAcrossFiles}` probes compare
eval vs `go build` output byte-for-byte on both recorded shapes plus
aggregate and cross-file adversarial variants, and the generated Go
was inspected to confirm the emitted node shapes eval now models.
