# Verification: aggregate-bound function values as named wrappers

- Date: 2026-11-05
- Decision: [Aggregate-bound function values become named wrappers; residual initialization cycles are rejected](../decisions/2026-11-05-aggregate-function-bindings.md)
- Scope: P2-2c — initialization cycles through aggregate values

## Method

The prototype is the implementation itself: `indexStaticAliases`
learned element tracing through pattern shapes (`bindElems`), the
three `letDecl` emission paths emit the same `ValueAlias` FuncDecl a
`let g = f` produces, and a residual-dependency pass
(`checkInitCycles`) reports remaining variable cycles as E0907.
Each boundary case was compiled and, where legal, executed.

## Before

`let (again, _) = (count, 0)` compiled to

```go
var tmp4 rt.Tuple2[func(int64) int64, int64] = rt.Tuple2[...]{V0: count, V1: 0}
var again func(int64) int64 = tmp4.V0
```

and `go build` rejected it:

```
initialization cycle for tmp4
tmp4 refers to count
count refers to again
again refers to tmp4
```

surfacing in `gom build` only as a generic E0001 toolchain error
pointing at a `tmp4` the user never wrote.

## After — allowed shapes

The same program now emits the aggregate unchanged plus a named
wrapper (see `testdata/golden/aggregate_function_alias.golden.go`):

```go
var tmp4 rt.Tuple2[func(int64) int64, int64] = rt.Tuple2[...]{V0: count, V1: 0}
func again(tmp5 int64) int64 { return count(tmp5) }
```

Verified end-to-end (`go run` output in parentheses):

| Shape | Result |
| --- | --- |
| `let (again, _) = (count, 0)` | `func again` calls `count` (3) |
| reverse source order (`let` before `fn count`) | wrapper (3) |
| `let (a, b2) = (count, count)` — two wrappers | (3) |
| `let b = count; let (a, _) = (b, 0)` — alias through a tuple | `func a` calls `b` (3) |
| `let {f = ragain, x = rx} = {f = count, x = 7}` | `func ragain`; `var rx = tmp.X` (7) |
| `let Box g = Box count` (single-variant ctor) | `var tmpN = <match IIFE>` keeps the init-time variant check; `func g` (3) |
| `let ((a, x) as t) = (count, 0)` | `func a`; `var t`/`var x` projections (3) |
| `let (plus, px) = (add, 9)` — multi-arity element | wrapper closes over the `add` adapter, `plus px 9` (18) |
| `let (f, x) = (mk (), 41)` — untraceable, acyclic | `var f = tmp.V0` stays eager, compiles |

Eager initialization is unchanged: `let (again, x) = (count, mark 0)`
prints `init` during package initialization before `main` runs —
`TestAggregateAliasEagerInit` pins the ordering `init\nmain\n3\n`,
and the ctor shape's discard `VarDecl` keeps the invalid-variant
panic at init rather than at first call.

## After — rejected shapes (E0907)

Each diagnostic names the `.gom` binding or the destructuring `let`
by its pattern (`let (f, x)`), never `tmp4`:

| Shape | Diagnostics |
| --- | --- |
| `fn g = x + n; let (f, x) = (g, 41)` | cycle for `let (f, x)` and for `x` |
| `let (f, _) = (mk (), 0)`, `mk` returns `f` | cycle for `let (f, _)` and for `f` |
| `let pair = (count, 0); let (a, b) = pair`, `count` calls `a` | cycle for `pair`, `let (a, b)`, `a` |
| `let r = f 0`, `f`'s else reads `r` | `r` refers to `f` refers to `r` |
| `let (g, _) = (fn n -> f (n-1), 0)` (@tailrec fixture) | `let (g, _)` and `g`, plus the existing E0904/E0905 |

A self-referencing lambda cannot be written — `let (f, _) =
(fn x -> f x, 0)` fails earlier at E0407 because the lambda's type
cannot be inferred — but the cycle checker covers the shape
regardless (a self-loop through a function literal is still a cycle
in Go's model).

## Notes on the boundary

- The wrapper must call the element's **root function**, not read
  the aggregate: `func again(p) { return tmp4.V0(p) }` keeps `tmp4`
  on the path (`tmp4 → count → again → tmp4`) and still fails — the
  decision's key measurement.
- `let pair = (count, 0); let (a, b) = pair` stays rejected: a
  projection wrapper `func a(p) { return pair.V0(p) }` would make
  `pair`'s own initializer depend on `a`'s dependency set —
  including `pair` — a self-loop. Second-order tracing through
  variable right-hand sides is possible in principle and is left
  out of scope.
- `gom check` does not run lowering, so E0907 (like the other E09xx
  lowering diagnostics) appears on `gom build`/`run`/`test`, not on
  `gom check`.
- `TestTailRecPkgVarSeed` was reshaped to seed the cycle through a
  second function: its original `let r = f 0` with `f` reading `r`
  is a genuine initialization cycle (previously producing
  uncompilable Go) and now correctly reports E0907.
- `core.Validate`'s `ValueAlias` rules are unchanged — pattern-bound
  symbols are `KindVar` with monomorphic arity-one function types by
  construction (destructured lets are never generalized).

## Test evidence

- `internal/lower/initcycle_test.go`: tuple/record-adjacent emission
  shape, ctor match-discard preservation, alias chains, untraceable
  elements, and each rejected shape's E0907 codes.
- `internal/build/initcycle_test.go`: compiled-binary execution of
  the repro (prints 17), eager-init ordering, and per-shape E0907
  rejection.
- `internal/build/compare_test.go`: `aggregate_function_alias`
  evaluator-vs-compiled parity case (`3:7:2:5`).
- `testdata/spec/E0907_init_cycle_*.gom` ×5 pin the diagnostics;
  `testdata/golden/aggregate_function_alias.golden.go` pins the
  generated shape.
- `go test ./...`, `go vet ./...`, `gofmt -l .` all clean.

## Review follow-up — VarPat block initializers

Review of `b6a3fd4` found a real evaluation-order regression: a
plain `let g = { …; f }` emitted only the `FuncDecl` wrapper and
`d.Value` was never lowered, so the block's statements silently
never ran. The fix keeps the initializer as a hidden eager
`VarDecl` — the role a destructure's shared temporary already plays
— whenever the `VarPat` initializer is not itself the pure
reference (`initNeedsEval` on `alias.go`; emission in `letDecl` at
`lower.go`). Declining the alias instead was rejected: even
`let g = { count }` would then re-diagnose E0907 when `count`
refers back, splitting the VarPat path from the destructure one
for no semantic gain.

```go
// let g = { mark (); count }
var tmp21 func(int64) int64 = func() func(int64) int64 {
	mark(struct{}{})
	return count
}()
func g2(tmp22 int64) int64 { return count(tmp22) }
```

`TestAggregateAliasVarBlockRuns` executes the cyclic
`let g = { ignore (mark 0); count }` and pins `init\n3\n` — the
effect runs during package initialization and the cycle stays cut.
The review's unpinned shapes are now covered as well:

| Shape | Pinned by |
| --- | --- |
| `let (f as g, _) = (count, 0)` | `TestAggregateAliasAsPat`; `aggregate_alias_shapes` compare case |
| `let ((a, _), (_, m)) = ((count, 0), (0, 41))` | `TestAggregateAliasNested`; compare (`3:3:3:3:41`) |
| `let (h, _) = ({ (); count }, 0)` | `TestAggregateAliasBlockElem`; compare |
| `let g = { …; f }` eager + cyclic | `TestAggregateAliasVarBlock`/`VarPlain`/`VarBlockRuns` |
| `let g = { h }`, `h` bound to a call — rejected | `TestInitCycleVarBlock`; `var_block_untraceable`; `testdata/spec/E0907_init_cycle_var_block.gom` |
