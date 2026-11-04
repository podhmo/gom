# Same-named `test` blocks in sibling files take a file tag

- 状態: draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)）
- 日付: 2026-11-05

## Context

A `test "name"` block lowers to a package-scope `func TestX` in the
generated `_test.go`. Under the package compilation unit the per-file
generated names (`gomEq*`, `pkgTmp`) were already collision-proof —
each carries an fnv32a tag of the file path — but `TestX` was emitted
bare, so two sibling files writing `test "x"` produced two identical
`TestX` functions and the package failed at `go build`, not at a Gom
diagnostic (listed as a deferred boundary of
[package-compilation-unit](2026-11-05-package-compilation-unit.md)).

## Decision

### 1. Collision detection is data, not re-derivation

`core.UnitScope.Tests` records the emitted base name of every sibling
file's `test` block — computed by `core.TestFuncBase`, the single
contract both files share (`test "rect area"` → `TestRectArea`).
Lowering a file populates the map from `lower.Env.Siblings` the same
way it shares sibling binders, so the collision fact arrives on the
Core IR node; codegen never re-scans declarations.

### 2. Colliding names take the deterministic file tag

When a file's emitted test base appears in `UnitScope.Tests`, codegen
suffixes the file's own fnv32a path tag: `TestX` → `TestX_a1b2c3d4`
(eight lowercase hex digits — the same convention `pkgTmp` and
`gomEq*` use). A base no sibling repeats stays bare, so the common
one-test-per-file output is unchanged.

## Consequences

- `a.gom` and `b.gom` both writing `test "x"` emit `TestX_<tag(a)>`
  and `TestX_<tag(b)>` — distinct, deterministic, stable across runs
  and file ordering.
- A test name unique in the package emits the un-tagged `TestX` —
  generated output for single-file packages is bit-identical to
  before.
- The tag identifies *which file* failed when `go test` reports —
  grep-able back to the source.

## Alternatives

- *Reject same-named tests across files with a diagnostic.* Rejected:
  Gom `test` names are prose, not identifiers — requiring package-wide
  prose uniqueness is a spec change no user benefit justifies; the
  collision is an emission concern and it is solved at emission.
- *Numeric suffixes by file order.* Rejected: input order is not a
  stable naming contract (a third file added later renumbers everyone);
  the path hash is order-independent, matching the established
  `gomEq*`/`pkgTmp` convention.
