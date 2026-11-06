# LSP package compilation unit: overlay gather, per-member diagnostics

- 状態: 採用
- 日付: 2026-11-06
- 検証: [2026-11-06-lsp-package-unit-verification](../sketch/2026-11-06-lsp-package-unit-verification.md)
- Supersedes の一部: [lsp-v1-scope](2026-11-06-lsp-v1-scope.md) の
  "single-open-file compile unit" 制限（同 §Deferred の multi-file
  package units 項目）を本決定で解消する。v1 の他の範囲（同期方式、
  機能 slice）は変わらない。

## Context

The v1 server compiles each open document alone, so a `.gom` file
referencing names declared in same-directory siblings reports E0201
that `gom check` of the directory would not emit (recorded in
implementation-gaps.md and the v1 decision). The build driver's
package unit already exists (`build.CompileFiles`, per
[package-compilation-unit](2026-11-05-package-compilation-unit.md));
the server lacked the gather and the per-file diagnostic routing.

## Decision

### 1. Compile unit = the package, gathered as an overlay

`internal/lsp`'s `Compiler` seam is package-shaped:
`func(inputs []build.Input) ([]*build.Result, error)`, one input per
`.gom` member of the document's directory. The gather is the standard
LSP overlay:

- The unit's members are the directory's `*.gom` files plus open
  `.gom` documents of the same directory (new unsaved files the glob
  cannot see).
- An open document's buffer shadows the disk file of the same path —
  unsaved edits compile.
- Unopened members read from disk at compile time.
- Members sort by path before compiling (resolve's sorted-input-order
  convention).
- A document whose path is not an *absolute* `.gom` file path — a
  `.gomi` interface (its own scope), an untitled buffer, a relative
  URI — forms a singleton unit; a one-member unit is the single-file
  pipeline through `CompileFiles`'s own degenerate branch. The
  `filepath.IsAbs` half is load-bearing: `uriPath` passes non-file
  URIs through verbatim, and an opaque `untitled:scratch.gom` still
  ends in `.gom` while `filepath.Dir` of it is `"."` — without the
  guard the gather would glob the server's own working directory
  into the unit.
- A sibling that vanishes or is unreadable between glob and read
  drops out of the unit (degrades toward single-file behavior).

`didOpen`, `didChange` and `didClose` of any member recompile the
whole unit. `didClose` also matters because closing lifts the
overlay: the disk version replaces the buffer, which can flip other
members' diagnostics, so the package recompiles once after the
document drops.

### 2. Diagnostics publish per member file

`build.CompileFiles` already attributes unit diagnostics to the
result whose path the span names (`spread`); spanless ones land on
the first member's result. Publishing then routes per file:

- Open documents publish under their own URI and version.
- Unopened members publish under a `file://` URI synthesized from
  their path — a broken unopened file cannot fail the package
  silently (eglot files these in `flymake-list-only-diagnostics`;
  VS Code shows them in Problems).
- The server only speaks when there is something to say: a member
  that never had a pushed set and has no diagnostics gets no
  notification; a member whose set flips to empty gets an empty
  publish; a member that leaves the package scope (deleted file, or
  an open doc that closes without a disk file behind it) gets its
  last set retracted with an empty publish; a member whose pushed
  URI respells (synthesized ↔ client-supplied) has the stale slot
  cleared.
- Per-member conversion keeps the single-file rule — a span naming
  a different file drops from this member's set (it publishes under
  that file's own URI).
- The closed document's own URI gets the spec-shaped empty publish
  at didClose; when the file stays a member (still on disk), the
  package republish refills it — diagnostics belong to the file,
  not to the tab.
- An internal compile failure (panic) surfaces as an "internal
  error" diagnostic on open members only — it is a session-level
  fault, not package truth; unopened members publish empty.

### 3. Fresh loader options per recompile

`cmd/gom` wires `build.CompileFiles` with the same `compileOptions`
`gom check` uses, rebuilt per compile — no loader session cache:
each `import go` re-runs `go list -export` behind the importer and
imported `.gomi` re-read, so a freshly edited sibling or regenerated
interface always loads as it stands on disk. Measured cost
(verification sketch): ~48–50 ms per recompile for a two-member
package with one `import go` (the `go list` subprocess dominates),
~0.1 ms with no imports — acceptable for v1. Session-reuse caching
is deferred: it would drop the cost to the no-imports scale but
trades staleness against regenerated `.gomi` and changed Go deps —
a real follow-up, not a correction.

## Consequences

- The recorded E0201-on-siblings limitation is gone; package-scope
  semantics (cross-file E0202, E0605, shared top-level scope) apply
  to open packages exactly as `gom check` reports them.
- Unit-level failure semantics are `CompileFiles`'s: a sibling parse
  error aborts the unit's later phases, so a healthy member's own
  check-phase diagnostics are withheld until the sibling parses —
  its own parse diagnostics still publish. A resolve-phase error
  similarly withholds every member's check-phase diagnostics (the
  earlier set retracts).
- Hover works on cross-file uses: the use site lives in the
  requesting document's own typed file.
- Deferred, unchanged: incremental sync, `$/cancelRequest`,
  didSave/workspace watching (an unopened member's disk change is
  only noticed on the next member-triggered recompile — the
  retraction test models this), go-tool diagnostics, completion /
  definition / references, and the loader-session cache.
