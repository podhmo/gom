# E0302 granularity: arm-level vs per-alternative or-patterns

`internal/pattern` reports E0302 only when *every* alternative of an
arm is already covered. A partially covered or-pattern passes
silently:

```gom
match o {
  None -> 0
  Some _ | None -> 1   // the `None` alternative can never match
}
```

Maranget 2007 §6 ("useless or-pattern alternatives") defines the finer
check — each alternative of a disjunction gets its own usefulness test
against preceding rows and the arm's earlier alternatives — and both
OCaml and rustc report the dead alternative ("this pattern is
unreachable on the left-hand side"). Gom's spec section 4 and
error-codes.md describe E0302 only at arm granularity, so the current
implementation is conformant; the per-alternative diagnostic would be
an addition, not a fix.

Worth deciding:

- Whether a per-alternative report uses E0302 with `alternative`
  data (like E0304's) or a new code.
- Whether covered *first* alternatives make a later whole-covered arm
  redundant under a different message.
- Same machinery, different budget: the alternative-level check is
  another `useful` call per alternative, negligible in practice.

Deferred until the E0303/E0304 wiring (P1-11) settles which data
fields pattern diagnostics carry in practice.
