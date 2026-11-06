# Eval initialization: Go's ready-in-declaration-order frontier

- 状態: 採用（prototype で検証済み — [sketch](../sketch/2026-11-06-eval-init-ready-order-verification.md)）
- 日付: 2026-11-06

## Context

The evaluator drove top-level `let` initialization lazily: `Run`
walked declarations in order and `evalVar` forced a let's lazy
initializer on first read. That reproduces Go's *dependency*
ordering — an initializer never runs before what it reads — but not
the *tie-break* among independent variables: Go's package
initialization picks, at each step, the earliest-declared variable
whose dependencies have all initialized (spec 5.1). When an
earlier-declared `let` depends on a later one and an independent
ready `let` sits between them, lazy forcing runs the dependency
inside the dependent's evaluation — after the ready var should have
run. `TestDecl`s shared the gap: eval ran a `test` block at its
declaration position while the compiled `go test` binary starts test
functions only after package init completes.

The divergence was recorded as a constraint in
[2026-11-06-eval-package-unit](2026-11-06-eval-package-unit.md) §6 and
in the gaps ledger, with the fix path noted: port
`lower.CheckInitCycles`' refset/vardep analysis onto `check.Decl`s so
the evaluator can compute the same ready-set frontier Go uses. That
port is this decision. The order is observable only through
initializer side effects (host calls whose results are
state-dependent can also shift bound values); it does not change what
evaluates — only when.

## Decision

1. **The graph is ported, not lifted.** `internal/eval/initorder.go`
   recomputes refset/vardep over `check.Decl`s with the same rules
   lower applies to `core.Decl`s: a variable node per top-level `let`
   (the initializer is one step — the projection VarDecls the pattern
   lowers through are pure), a function node per `check.FnDecl` —
   including value-let thunks — that references through its body
   propagate transitively to referencers' dependency sets.
   `check.WalkExpr` supplies the lexical traversal, covering nested
   lambdas, inner lets, `go` bodies, match guards and select arms —
   the shapes Go counts lexically. References are `check.Var`,
   `check.MemberExpr` and `check.Apply`'s `Call.Callee`, filtered to
   the unit's own declaration symbols.
2. **`Run` is three phases.** (a) the ready frontier — repeatedly
   force the earliest-declared `let` whose vardep set has fully
   initialized; (b) `test` blocks in declaration order — the compiled
   binary's test entry points start after package init; (c) `main`.
   Laziness is preserved inside each forcing step (dependency reads
   still resolve through `evalVar`) and for `Expr`/`Apply` entry
   points that never run the frontier.
3. **Cyclic leftovers keep the lazy diagnostic.** A var on (or
   downstream of) a dependency cycle is never ready; checked programs
   cannot contain one (the compiled unit reports E0907). Hand-built
   units that stall the frontier are forced in declaration order
   afterwards, so the existing re-entrant-read E9003 still reports
   them — after every initializable var has run.
4. **`let _ = e` and multi-binding patterns occupy one slot.** A
   pattern binding no symbols has no `letInit` yet still lowers to a
   package var — it is a frontier node driven by `Run` alone; a
   destructure's bound names share one node (one initializer, one
   step).
5. **Known residual: `let`s lower emits as ValueAlias FuncDecls.**
   A static function alias
   ([2026-11-03-static-function-alias-declarations](2026-11-03-static-function-alias-declarations.md),
   extended to aggregate elements by
   [2026-11-05-aggregate-function-bindings](2026-11-05-aggregate-function-bindings.md))
   is a `check.LetDecl` here but a function node in core — the two
   node universes disagree, and the divergence is observable two
   ways. An effectful alias initializer (`let f = { …; count }`)
   splits into a hidden temp var *plus* the wrapper: references to
   `f` become eval var dependencies where Go traverses a function
   body, so `f`'s effects init before (Go: possibly after) a caller
   of `f` declared earlier. A *pure* alias (`let f = g`) normally
   lands the same — its frontier slot is invisible and its
   transitive vardep set matches — unless the function path loops
   back to the alias: `fn g () = { ignore (f); … }` references the
   var node `f`, giving `f` a self-dependency — never ready, forced
   by the leftover sweep — while core's wrapper emits no vardep and
   the caller initializes in place (compiled `w,z`; eval `z,w`,
   verified in the sketch). Closing it needs lower's alias
   classification — `bindElems`, transitive `static()`, thunk/arity
   checks — visible at check level: `internal/lower/alias.go`
   already operates entirely on `check.Decl`/`check.Expr`, so the
   lift is close to a file move plus an arity/thunks index, not a
   re-port. Recorded in the gaps ledger rather than duplicated into
   eval, where it would drift from the lowering it must mirror.

## Consequences

- `Evaluator` gains `initOrder`; `NewFiles` builds the graph once.
  `ensureTopLet`/`forceTopLet`/`evalVar` are unchanged — the lazy
  mechanism is the substrate the frontier drives.
- Observable order changes exactly where Go's pick differs from
  forcing order: independent vars between a var and its dependency
  now initialize first; test blocks run after all init. Existing
  dependency-correct orderings (a var after what it reads) are
  unchanged.
- The residual class shrinks to static function aliases — `let`s
  lower emits as ValueAlias FuncDecls — an exotic shape. Everything
  else the ledger described is closed.

## Alternatives considered

- **Keep lazy forcing, reorder nothing.** Leaves the ledger's
  divergence class open; rejected — the port is small and the harness
  is precisely for catching this.
- **Lift lower's analysis into a shared package.** The two
  computations operate on different node universes (`core.Decl`/`core.Expr`
  vs `check.Decl`/`check.Expr`); a shared lift would need abstraction
  over both or would refactor `lower` mid-flight — flagged instead:
  the analysis itself stays deliberately parallel, keyed to the same
  spec-5.1 rules.
- **Model the alias split in eval** (bind wrappers eagerly, effects-
  only init nodes). Needs lower's alias classification at check
  level; duplicating it invites silent drift — deferred to the
  follow-up that lifts the classification somewhere shared.

## Prototype / verification

Verified:
[docs/sketch/2026-11-06-eval-init-ready-order-verification.md](../sketch/2026-11-06-eval-init-ready-order-verification.md).
`TestInitReadyFrontier` (internal/eval) pins the frontier picks —
interleaved ready vars, fn/lambda-mediated deps, multi-file order,
wildcard and destructure lets, tests after init, cycle leftovers —
and `TestDiffInitReadyOrder{,Files}` (internal/build) compares
eval-vs-compiled stdout byte-for-byte on real sources.
