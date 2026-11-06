# Report .gomi-only import cycles as E0606 with the cycle path

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-iface-import-cycle-e0606-verification.md)。spec.md §7 へ反映済み）
- 日付: 2026-11-06
- Supersedes: [gom-import-alias-and-gomi-imports](2026-11-06-gom-import-alias-and-gomi-imports.md) のうち「`.gomi` 同士だけの循環は in-flight guard が普通の E0203 読み込み失敗として報告する」という扱い（検出位置は同じ guard のまま、経路付きの E0606 に揃える）

## Context

The loader keeps two independent cycle detectors:

- A static `.gom`-edge scan (`implEdges` + DFS) seeds at the entry
  package and reports implementation import cycles as E0606 with
  the chain — those cycles would otherwise surface only as
  `go build`'s "import cycle not allowed" after codegen.
- An in-flight guard on `Load` catches a `.gomi` import that
  re-enters a package mid-load — a cycle the static scan cannot
  see, because a loaded package's `.gom` files are never compiled
  through `Load` and only `.gomi` edges recurse. Before this change
  the guard returned a plain `cyclic Gom import of %q` error, which
  resolution wrapped into E0203 `cannot resolve import` — the
  generic load failure, with no cycle path and a reason string
  nested one `does not check` layer per `.gomi` hop
  ([gaps](../implementation-gaps.md) `.gomi` 同士の cycle 検出).

The user-facing difference matters: E0606 names the loop
(`a -> b -> a` in `data.cycle`); E0203 names only the import the
entry file happened to reach first, and buries the loop inside an
unstructured reason.

## Options

- **(i) Report .gomi-only cycles as E0606** — carry the in-flight
  load stack, reconstruct the chain at re-entry, propagate it up
  through `load` as the same typed `resolve.ImportCycleError`, and
  let resolution emit E0606 at the import site.
- **(ii) Keep E0203, word it honestly** — document that .gomi-only
  cycles are plain load failures and leave the chain implicit.

## Decision

Option (i): E0606 for .gomi-only cycles, detected where they
already were — the in-flight guard, now carrying the path.

The detection boundary stays exactly as it was; only the reported
shape changes:

- `Loader.loading` records each in-flight path's position in a
  `stack`; re-entry slices `stack[i:] + [path]` into the chain —
  the true loop, not the path the entry took to reach it.
- A `.gomi` compile that reports E0606 (the cycle it ran into
  deeper in the graph) propagates the typed error back through
  `load` instead of the opaque `does not check` wrap, so every
  importer up to the entry file reports the same cycle.
- The `.gom`-edge scan is unchanged and still walks `*.gom` only:
  `.gomi` imports never materialise as generated Go imports, so
  seeding them into the impl scan would over-report. Conversely a
  `.gomi` edge that merely points *back* at a package whose load is
  not in progress (entry `a` imports `b`; `b.gomi` imports `a`) is
  not a cycle — `TestImplImportNoCycleWhenOnlyIfaceDepends` keeps
  its semantics.

E0606's justification broadens correspondingly: "the generated Go
packages would not build" covers `.gom` edges; `.gomi`-only cycles
fail earlier, "the interfaces can never be ordered for loading."
Same error code — it is the same language-level error — so no new
code is introduced (error codes are not reused across meanings, and
this does not introduce a new meaning).

## Consequences

- `internal/gomimport/loader.go`: `loading` becomes path → stack
  index plus `stack []string`; re-entry returns
  `*resolve.ImportCycleError`; `load` re-emits a deeper compile's
  E0606 as the typed error (`data.cycle` round-trips the chain).
- `internal/gomimport/cycle_test.go`:
  `TestIfaceImportCycleMutual` and `TestIfaceImportCycleInsideGraph`
  pin the new behaviour — exactly one E0606 at the entry import
  clause with the true chain — and the existing
  `TestImplImportNoCycleWhenOnlyIfaceDepends` pins the boundary.
- `docs/spec.md` §7.1: .gomi-only cycles now read "経路付きの
  E0606"; the one-directional `.gomi` back-edge stays documented as
  not a cycle.
- `docs/error-codes.md` E0606 covers both edge kinds.
- Deterministic reporting: the chain starts where the load order
  first re-enters, so the same module always reports the same
  loop.
