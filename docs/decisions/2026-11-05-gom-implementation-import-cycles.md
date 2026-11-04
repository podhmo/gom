# `.gom` implementation imports must be acyclic (E0606)

- 状態: draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)）
- 日付: 2026-11-05

## Context

`import "path"` between Gom packages (decision
[package-compilation-unit](2026-11-05-package-compilation-unit.md))
compiles the imported package's `.gomi` and hands out members whose
generated references are ordinary `pkg.Name` Go selectors. The loader
already rejected `.gomi`-level cycles (two interface files importing
each other), but a cycle through *implementation* imports was left
unchecked — the deferred boundary listed at the end of that
decision. `gom check`/`gom build` accepted `a.gom ↔ b.gom` mutual
imports and emitted Go that `go build` then rejected with "import
cycle not allowed" — the exact boundary failure the V-6 residual
round exists to close.

## Decision

### 1. The loader owns the cycle check over `.gom` edges

`internal/gomimport.Loader` scans the entry package's `.gom` imports
once and runs a deterministic depth-first walk over the `.gom`
import graph it can see on disk (each package directory's sibling
`.gom` files, sorted). Reaching a package already on the walk's
stack reports a typed `resolve.ImportCycleError` carrying the cycle
`Chain` — `a -> b -> a` — in source order of first visitation.

The check distinguishes *implementation* edges from interface edges:
only `.gom` imports count. A `.gomi` that names the importing
package back is still legal — an interface may reference the
implementing package's types without creating a code dependency.

`loadPackage` maps the typed error to **E0606** at the import path
span, with `data.cycle` holding the chain. The generated code is
never produced for a cyclic package graph, so `go build` never sees
the loop.

### 2. Rule for spec §7 (proposed wording)

> A Gom package may not import itself — directly, or transitively
> through the `.gom` files of packages it reaches. The import path
> of the offending edge reports E0606 listing the cycle.
> `.gomi`-to-`.gomi` references do not create implementation
> dependencies: an interface may name the importing package back.

`docs/spec.md` is *not* edited here — the rule is recorded in this
decision and lands in spec §7 through the normal documentation
pass.

## Consequences

- Self-imports (`p` importing `p`), mutual cycles, and longer
  transitive cycles are all E0606 at the offending import edge —
  the diagnostic carries the whole chain so a user sees where to
  break the loop.
- Detection is deterministic: edge sets are sorted, the DFS stack
  preserves first-visit order, and completed packages are memoized,
  so a diamond reuses the shared subgraph instead of re-walking it.
- Cycles that only close back through the entry package (the
  "import cycle back to entry" shape) are detected even though the
  entry is never `Load`-ed again — the DFS treats it as a graph
  node like any other.
- Detection happens at the *import* boundary: a package with an
  internal cycle that nobody imports is never scanned. That is the
  same reachability bound `.gomi` conformance has always had.
- The verdict is scoped to the loop's members: the loader records
  the distinct chain nodes (`implCycleSet`) and `Load` returns the
  cycle error only for those paths. An import of a package that is
  not on the found cycle loads normally — one cyclic subgraph does
  not poison unrelated imports with a false E0606.

## Alternatives

- *Detect cycles in `resolve`/`build` over a package graph.*
  Rejected: only the loader knows which import paths are Gom
  packages vs Go packages and where their directories live — the
  graph the compiler can see is exactly the graph `gomimport`
  walks.
- *Defer to `go build`'s "import cycle not allowed".* Rejected: the
  residual item exists precisely because that message names
  generated `.go` files and paths the user never wrote.
