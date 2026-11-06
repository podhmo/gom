# Verification: .gomi-only import cycles report E0606 with the path

- Date: 2026-11-06
- Decision: [iface-import-cycle-e0606](../decisions/2026-11-06-iface-import-cycle-e0606.md)

Prototype: `internal/gomimport`'s in-flight guard carries the load
stack, reconstructs the chain at re-entry, and `load` propagates a
deeper compile's E0606 as the typed `resolve.ImportCycleError`.
Verified by `internal/gomimport/cycle_test.go` on this branch.

## Mutual .gomi cycle — E0203 becomes path-bearing E0606

Fixture: `a/a.gomi` imports `example.com/m/b`, `b/b.gomi` imports
`example.com/m/a`, neither `.gom` imports the other, and the entry
`main/main.gom` imports `example.com/m/a`
(`TestIfaceImportCycleMutual`).

Before: the re-entry returned `cyclic Gom import of "…/a"`, wrapped
into `b.gomi does not check: 1 diagnostic(s)` at `a`'s load and
surfaced at the entry as E0203 `cannot resolve import
"example.com/m/a"` — no chain, reason string nested per hop.

After: exactly one diagnostic at the entry's `import` clause:

```
E0606 import cycle: example.com/m/a -> example.com/m/b -> example.com/m/a
```

with `data.cycle` carrying the same string.

## Chain is the loop, not the entry path

`entry -> a -> c -> d -> c`: `a`'s `.gomi` only reaches the `c <-> d`
loop. `TestIfaceImportCycleInsideGraph` asserts the reported cycle
is `example.com/m/c -> example.com/m/d -> example.com/m/c` — the
in-flight stack slice, not the full reach path.

## Boundary preserved: .gomi back-edges are not cycles

`TestImplImportNoCycleWhenOnlyIfaceDepends` (entry `a` imports `b`
in `.gom`; `b.gomi` imports `a`) still produces zero diagnostics:
`a` is not mid-load when `b.gomi` resolves its import, so no
re-entry fires. The `.gom`-edge impl scan is untouched — `.gomi`
edges never become generated Go imports, so folding them into the
static scan would over-report. The two detectors remain mirrors of
each other: static `.gom` scan for generated-package cycles,
dynamic in-flight chain for interface-only cycles, both reporting
E0606.

## Determinism

`loadPackage` resolves imports in declaration order and the chain is
rebuilt from the load stack at the re-entry point, so a given module
always reports the same loop (member order inside a cycle differs
only by which package the entry reaches first — the loop itself is
what `data.cycle` records, as with `.gom` cycles).
