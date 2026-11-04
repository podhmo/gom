# Sketch: package initialization order — verification log

Verification for
[decision: package initialization order](../decisions/2026-11-05-package-initialization-order.md):
the residual-cycle check now builds its dependency graph over the
whole package unit, so a variable cycle routed through sibling
`.gom` files is reported as E0907 by `gom build` instead of being
left for `go build`.

## The reproduction

Two files of one package, each reading the other's variable:

```gom
-- a.gom                          -- b.gom
package main                      package main

let x = y + 1                     let y = x + 1

fn main () -> Unit = ()
```

Before the unit pass, each file's `checkInitCycles` saw only its
own `Decls`: `x`'s initializer referenced a `y` the graph did not
contain (and vice versa), so both files emitted `var x = y + 1` /
`var y = x + 1` and `go build` reported the cycle the compiler
missed — the gap recorded in
[2026-11-05-v6-residual-verification.md](2026-11-05-v6-residual-verification.md).

After: `gom build` on the pair reports two E0907 diagnostics — one
per variable on the cycle, at its own file's span, like Go's
"initialization cycle" report — and withholds every output:

```json
{"code":"E0907","message":"initialization cycle for `x`: `x` refers to `y` refers to `x`","span":{"file":".../bad/a.gom",...},"data":{"path":["x","y","x"],"var":"x"}}
{"code":"E0907","message":"initialization cycle for `y`: `y` refers to `x` refers to `y`","span":{"file":".../bad/b.gom",...},"data":{"path":["y","x","y"],"var":"y"}}
{"kind":"summary","ok":false,"errors":2,"warnings":0,"outputs":[]}
```

`internal/build/files_test.go` covers this as
`TestCompileFilesInitCycleAcrossSiblings` (per-file spans, no `tmp`
names, no outputs) and `TestCompileFilesInitCycleThroughSiblingFn`
— `fn f` in a.gom reads `y` while b.gom's `let y = f 0` calls it,
a cycle whose two ends sit in different files and which the old
per-file pass could not see at all (file a has no `var`s, file
b's `f` is not in its own `funcs` set).

## Dependency order, not file order

The companion check shows the analysis is a dependency order, not
a per-file gate:

```gom
-- a.gom                                    -- b.gom
let x = { ignore (fmt.Println "a-init");    let y = { ignore (fmt.Println "b-init");
          y + 1 }                                     40 }
fn main () -> Unit = { ignore (fmt.Println x); () }
```

`a.gom` sorts first by file name, but `x` depends on `y`, so the
generated package initializes `b.go`'s `var y` before `a.go`'s
`var x`. Compiled and run, the observable order is:

```
b-init
a-init
41
```

`TestCompileFilesSiblingInitOrderOK` covers the same shape at the
driver level — a cross-file forward reference compiles without
diagnostics.

## Mechanism

- `lower.CheckInitCycles(files []*core.File) []diag.Diagnostic`
  replaces the lowerer's per-file method. The algorithm is
  unchanged — refset over `Var`/`FuncRef`/`CallDirect` walked
  through nested bodies, vardep by reachability through function
  nodes, one E0907 per variable reaching itself — it just reads
  `Decls` from every member of the unit.
- `build.CompileFiles` runs it after all members lower, spreads
  the diagnostics to the result owning each span's file, and the
  existing `unitFailed` path withholds every output.
  `build.Compile` is the one-file degenerate case.
- Naming moved off the lowerer-local `tmpLets` map onto
  `core.VarDecl.DiagName` (`"let " + patText(d.Pat)`), set at all
  four hidden-temporary sites, so a `let (a, _)` on a cycle still
  prints the pattern — verified by the existing spec fixtures
  (`E0907_init_cycle_*`) and the no-`tmp` assertion in the new
  tests.

## Test results

- `go test ./internal/lower ./internal/build` — pass; the new
  driver tests pass and the same-file E0907 suite
  (`initcycle_test.go`, `testdata/spec/E0907_*`) is unchanged.
- `go test ./...` — run as part of the final gates for the commit.

## Related residual gap (recorded, not fixed)

`@tailrec` R4 has the same file-local limitation this change
removed from the init-cycle pass: its direct-reference graph sees
one file, so mutual recursion routed through a *sibling* function
or let escapes E0904. Left for the implementation-gaps ledger;
fixing it means building R4's graph over the unit's `Decls` the
same way `CheckInitCycles` now does.
