# Verification: eval ready-in-declaration-order initialization

- 日付: 2026-11-06
- 対象: [decision: eval-init-ready-order](../decisions/2026-11-06-eval-init-ready-order.md)

## 何を検証したか

The gap recorded in
[2026-11-06-eval-package-unit](../decisions/2026-11-06-eval-package-unit.md)
§6: the evaluator's lazy forcing reproduced Go's dependency order
but not its ready-set pick — the earliest-declared var whose
dependencies are all initialized runs first — and `test` decls ran at
their source position instead of after package init.

The fix ports `lower.CheckInitCycles`' refset/vardep analysis onto
`check.Decl`s (`internal/eval/initorder.go`) and makes `Run`
three-phase: frontier forcing (earliest-ready `let` first), then
tests in declaration order, then `main`. lower was not touched; the
analysis is deliberately parallel because the node universes differ
(`check.Decl` vs `core.Decl` — see the decision's residual note on
the static-alias split).

## 検証手順と結果

### Unit: frontier picks (`internal/eval`, `TestInitReadyFrontier`)

- **interleaved ready vars** — `a` depends on `d`; `b` is ready and
  declared between them; `c` is ready last. Order `b,a` forcing `d`,
  then `c` — Go initializes `b` before `a`'s chain. ✔
- **deps through functions** — `x` reads a top-level `fn` whose body
  reads `w`; `y` reads a lambda-value thunk whose body reads `v`.
  vardep propagates through function bodies and closures; `w`, `v`
  init first. Also exercises `check.MemberExpr` and `Apply` callee
  references. ✔
- **wildcard + destructure lets** — `let _ = …` has no `letInit` yet
  still runs in the frontier (it lowers to a package var); a pattern
  destructure shares one node across its bound syms. ✔
- **tests after init** — a `test` decl sitting between two `let`s
  runs after both. ✔
- **cycle leftover** — a self-referential `x` (impossible from
  check, hand-built) stalls the frontier; leftover nodes force in
  declaration order so E9003 re-entrant read still reports. ✔

`go test ./internal/eval` — ok.

### End-to-end: eval vs compiled stdout (`internal/build`)

`evalDiff` runs the same sources through the real `eval` pipeline and
through build → `go run` / `go test`, comparing stdout byte-for-byte
with class `clean`.

- `TestDiffInitReadyOrder` — `a` calls `zed()` (a `fn` reading `b`)
  plus `b`; `c`,`d` ready between `a` and `b`. Both sides print
  `init:c, init:d, init:b, init:a, main`. Before the change eval
  forced `a` first — its own `init:a` side effect precedes the
  `zed()`/`b` reads inside its body — so old eval printed
  `init:a, init:b, init:c, init:d`; now identical to compiled. ✔
- `TestDiffInitReadyOrderFiles` — same shape split across `a.gom` /
  `b.gom` via `eval.NewFiles`; lexical-basename file order gives the
  same declaration order. ✔
- `TestDiffInitReadyOrderTestDecl` — `test "t"` declared between two
  `let`s prints after both initializers on both sides
  (`init:x, init:y, test:t, main`). ✔

`go test ./internal/build -run 'TestDiffInitReadyOrder'` — ok.
`go test ./...` — all 29 packages ok, no FAIL.

### Residual confirmed: `let`s lower emits as ValueAlias FuncDecls

The residual class is a `check.LetDecl` that lower turns into a
ValueAlias `FuncDecl` — a static function alias (decisions
[2026-11-03-static-function-alias-declarations](../decisions/2026-11-03-static-function-alias-declarations.md),
[2026-11-05-aggregate-function-bindings](../decisions/2026-11-05-aggregate-function-bindings.md)).
On `check.Decl`s the wrapper/temp split has not happened, so the
two node universes disagree — observable two ways.

**Effectful alias initializer** — the temp-var split:

```gom
let a = { ignore (fmt.Println ("init:a")); f () }
let f = { ignore (fmt.Println ("init:f")); count }
fn count () -> Int = w
let w = { ignore (fmt.Println ("init:w")); 1 }
```

`f`'s initializer has effects and peels to a bare local-function
reference, so lower emits it as a hidden package var *plus* a
function wrapper. The compiled order is `w, a, f-tmp` — `a` calls
the wrapper, the wrapper's *body* reads `count`/`w`, and the
effectful temp is a separate later-declared var. Eval sees one
source `let f` node, so `a`'s reference to `f` is a var dependency
and `f`'s effects run before `a`: `w, f, a`. Confirmed
empirically — generated Go inspected and executed
(`init:w, init:a, init:f, main` vs eval `init:w, init:f, init:a,
main`).

**Pure alias on a function-path reference loop** — found in fix
review; the earlier "pure aliases are observably identical" claim
was wrong in this corner:

```gom
let w = { ignore (fmt.Println ("w")); f () }
let f = g
fn g () -> (() -> Int) = { ignore (f); count }
fn count () -> Int = 1
let z = { ignore (fmt.Println ("z")); 0 }
```

In core, `let f = g` is a ValueAlias `FuncDecl` (a wrapper, no
package var): `w`'s call path `w → f → g → {f, count}` is functions
only, so `w` is ready and the compiled binary prints `w, z` —
verified by `gom build` + `go run`. In eval `f` is still a var
node: `g`'s value-position reference to `f` resolves into `varNode`,
giving `f` a self-dependency. `f` is never ready, `w` waits on it,
and the leftover sweep forces `w` only after every ready var —
eval prints `z, w`. Pinned as the known-residual regression test
`TestInitReadyFrontier/known_residual:_pure_alias_on_a_function-path_loop`.

A pure alias *without* the loop (`let f = count`, or `f`'s
function path not referencing `f` again) is still observably
identical: `f`'s frontier slot is invisible (its init is a pure
closure bind) and its transitive vardep set matches what Go's
function traversal yields, so dependents wait on the same vars.

Not fixable on `check.Decl`s alone: the split and its
classification (`bindElems`, transitive `static()`, thunk/arity
checks) live in lower — though `internal/lower/alias.go` already
operates entirely on `check.Decl`/`check.Expr` (arity and thunk
indexes aside, both recomputable from `check.FnDecl`), so the lift
is closer to a file move than a re-port. Flagged in the decision
and proposed for the gaps ledger rather than papered over by
duplicating lower logic in eval.

## 残っている論点

- Static-alias residual above: eval-vs-compiled init order can
  still differ for a `let` lower emits as a ValueAlias FuncDecl —
  an effectful alias initializer referenced during another var's
  init, or a pure alias sitting on a function-path reference loop
  back to itself.
- The frontier covers vars; `let` initializers that eval only reaches
  through `Expr`/`Apply` entry points keep lazy-forcing order by
  design (no package init happens without `Run`).
