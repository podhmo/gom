# Verification report: `@tailrec` requirement attribute

- 日付: 2026-11-02
- 対象: [decision: tailrec-requirement](../decisions/2026-11-02-tailrec-requirement.md)

`internal/lower/tailrec.go` and the `TailRec` plumbing through
`syntax` → `check` → `lower` are the implementation of the draft
decision. This report records what was verified before adoption.

## Verified behaviors (probe programs through `gom build`)

| Probe | Result |
| --- | --- |
| `@tailrec fn loop` with a self-call at an `if` branch tail | accepted; emitted Go is the `for` loop (`ok_tailrec`) |
| `f (n - 1) + 1` — operand position | E0902 at the call (`f (n - 1)` span) |
| `Ok (n - 1) \|?> f` — post-processing pipe | E0902 at the inner call |
| `let g = f; g (n - 1)` — alias escape | E0903 at the `f` reference, plus E0905 (the `CallFunc` is invisible to R1) |
| `even → odd → even` (only `even` marked) | E0904 `through odd` with `path: [even odd even]`, plus E0905 |
| both `even` and `odd` marked | each reports its own E0904 + E0905 |
| `@tailrec fn f () -> Int = 42` | E0905 at the function name |
| `@tailrec let x = 1` | E0101 at parse: "applies only to a \"fn\" declaration" |
| self-call only inside `go { ... }` | E0905 — superseded row: exempt from R2 but not convertible, so R1 fails after the post-review tightening below |
| self-call inside a nested `let g = fn k -> f k` | E0902 (function boundary) |
| self-call inside a `for` body | E0902 |
| `x \|> f` at tail, `match` arm results, arity-split `cur (n-1)` | accepted |
| `let h = g` in `f` where `g` calls `f` — cycle through a `FuncRef` edge | E0904 (the FuncRef edge, see deviations) |
| `try (f (n - 1))` bound to a `let` | E0902 (`try` as operand) |
| polymorphic `@tailrec fn rep (n) (xs: List a)` | accepted; generic function still emitted |
| same non-tail call **without** the marker | accepted, no diagnostics — unmarked code unchanged |

## `gom check` coverage

Confirmed the open question's trade-off: `gom check` stops before
lowering (`build.Options.CheckOnly`), so E0902–E0905 are `gom build`
diagnostics. Spec §1.1 item 8 now states this; the alternative
(typed-AST check for `gom check` coverage) stays documented in the
decision.

## Deviations from the draft

1. **R4's graph counts `FuncRef` edges**, not only `CallDirect`. A
   file-local `FuncRef` can still be applied through a variable or
   combinator — `let h = g` in `f` where `g` calls `f` is the same
   stack-growing cycle. This is strictly stronger than the drafted
   "direct-call graph".
2. **R1 counts every self `CallDirect`, including nested-closure and
   `Spawn` positions**, not only converted `TailCall`s — `f` is
   recursive wherever the call sits; R2 separately decides whether
   its position is legal.
3. **Representation stays the `TailRec bool` MVP** next to
   `TestOnly`; the `Attrs []Attr` generalization (and its two problem
   files) is deferred — see the decision's *Surface syntax*
   alternatives.
4. The emitted messages carry the backquoted name `` `@tailrec`
   function `f` ... `` rather than the table's sketch wording, and
   E0905/E0904 report at the **function name** span.

Post-review amendments (VarDecl graph vertices, the R1 TailCall
requirement, the R3 `go` rationale, deterministic paths) are
recorded in the decision's *Resolution* section — same list, later
entries.

## Test coverage

- `internal/lower/tailrec_test.go`: per-rule unit cases —
  accepted / E0902 / E0903+E0905 / E0904 (direct, FuncRef edge,
  package-var alias, destructured var, alias chain) / E0905 /
  `Spawn`-only → E0905 / `Spawn` + tail accepted / nested closure /
  unmarked baseline / `FuncRef` inside `go` still E0903 /
  match-arm tail accepted / package-var seed initializer is not a
  cycle.
- `testdata/spec/`: `ok_tailrec`, `E0101_tailrec_on_let`,
  `E0902_tailrec_operand`, `E0902_tailrec_then_pipe`,
  `E0903_tailrec_let_alias`, `E0904_tailrec_mutual`,
  `E0904_tailrec_var_alias`, `E0904_tailrec_var_destructure`,
  `E0905_tailrec_no_recursion`, `E0905_tailrec_go_only` —
  end-to-end through the real driver.
- `internal/syntax` tests cover the attribute: accepted on `fn`,
  E0101 on `let` and on unknown names, `@test` unchanged.
- Existing tail-call / capture-snapshot tests unchanged and passing;
  no `testdata/golden/` changes (codegen untouched).

## Post-review probes (R4 soundness hole, same day)

Review found R4's graph saw only `CallDirect`/`FuncRef` edges
between `FuncDecl`s, so cycles routed through package-level `let`
values compiled to `return g(...)` inside the emitted `for` — a
real call per iteration, accepted silently. The graph now includes
`VarDecl` vertices; probes through `gom build`:

| Probe | Result |
| --- | --- |
| `let g = f` + `g (n - 1)` in `f` | E0904 `through g`, `path: [f g f]`, plus E0905 |
| `let (g, _) = (fn n -> f (n - 1), 0)` + `g (n - 1)` | E0904 `through g`, `path: [f g tmp1 f]` — g → tuple tmp via the `FieldGet`'s `Var`, tmp → f via the stored closure's `CallDirect` |
| `let h = g; let g = f` + `h (n - 1)` | E0904 — alias chains close through `Var`→`VarDecl` edges |
| `let g = f` + `apply2 g (n - 1)` (var handed to a combinator) | E0904 — any `Var` use is an edge, not only `CallFunc` callees |
| `let h = g` local rebind inside `f` + `h (n - 1)` | E0904 through the package `var g` |
| `let r = f 0` + `if n <= 0 { r } else { f (n - 1) }` | **accepted** — the initializer's top-level `CallDirect` runs once at package init; it is not a call the stored value performs |
| `let g = fn n -> f (n - 1)` (VarPat → `FnDecl` flip) | E0904, unchanged |
| only `go { f (n - 1) }` | E0905 — spawned calls are R2-exempt but not convertible, so no loop exists to certify |
| tail call + spawned self-call | accepted |
| `g n h = f n h` where `f` calls `h` (parameter fed back) | outside the static guarantee — no resolvable reference edge (only E0905, since `f` has no direct self-call) |

The `CallFunc` callee is a `*core.Var` atom naming the package
`VarDecl`'s symbol — counting `Var` atoms to file-local VarDecls as
edges covers it (and `FieldGet` receivers, combinator arguments,
`let` rebinds) without a special case. What is not covered
statically: recursion through values of function type produced by
calls (a returned function) or fed in as parameters — documented in
spec §1.1 item 8 as outside the guarantee.

## Not covered (recorded as future work)

- `@test @tailrec fn` — grammar admits one attribute per decl;
  needs the `Attrs` list generalization.
- Multi-file packages — the SCC graph is file-local, which is
  complete today (one `.gom` per compilation unit).
- `let`-bound recursion conversion — `@tailrec let` stays a parse
  error until `TailCall` can reach through package `var`s.
