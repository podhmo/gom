# SelfImportPath is the input's module-relative package path

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-self-import-path-module-derived-verification.md)）
- 日付: 2026-11-06

## Context

`build.Options.SelfImportPath` exists so codegen can emit
unqualified references into the package the output lands in — a
package never imports itself. `cmd/gom` fed it from
`gomstd.SelfPath`, which pattern-matched the *spelling* of the
input path for a `stdlib/<pkg>/` segment pair. Two failure modes
came out of that ([problem](../problems/2026-11-05-self-import-path-layout-convention.md)):

- **Missed match (the footgun).** `cd stdlib/list && gom build
  list.gom` — a basename input has no `stdlib/list` segments —
  left `SelfImportPath` empty. References into the toolchain's
  list package then emitted a qualified
  `import "github.com/podhmo/gom/stdlib/list"` inside `package
  list`'s own output: a self-import only `go build` would reject.
- **False positive.** A *user* module reusing the `stdlib/<pkg>/`
  layout (a vendoring convention, a mirror) also matched — the
  generated file emitted bare references to names it does not own
  and dropped the real toolchain import: a silent miscompile.

## Options

- **(i) Reject a generated self-import early** with a fresh E05xx
  diagnostic — catch the footgun at compile time instead of at
  `go build`.
- **(ii) Derive SelfImportPath from the module** — the input
  directory rendered module-relative via the nearest `go.mod`,
  which is exactly the package the output occupies.
- **(iii) Keep the lexical scan but gate it on the toolchain
  module** (`go.mod` module path `github.com/podhmo/gom`).

## Decision

Option (ii): `gomimport.Loader.EntryPath()` — the same
module-relative computation the implementation-import cycle check
seeds from — now answers `SelfImportPath`. The loader already
resolves the main module for `import` clauses, so the identity
cannot drift from the one `import` resolution uses.

Why (i) is not needed: a rejected self-import can only be produced
by a *missing* suppression, and with (ii) suppression is keyed on
the true output-package path — emitted imports come only from
references whose package path is not `selfPath`, so a self-import
is unrepresentable in generated code. An explicit `import
"<own path>"` in source is already E0606 (the implementation-scan
DFS sees the self-edge). A diagnostic would pin a state that
cannot occur.

Option (iii) fixes the false positive but keeps the wrong
abstraction: the toolchain path matched lexically is still a
guess about where output lands — it misses every package that is
not stdlib (a toolchain package built from a worktree or a
different module path), while (ii) is the definition of "the
package this file's output occupies" for *any* input.

## Consequences

- `gomstd.SelfPath` is deleted; `stdlib/<pkg>/` directory names
  stop being reserved in any sense — a user layout is just a
  directory name, resolved by its own `go.mod`.
- `cd stdlib/list && gom build list.gom` emits unqualified
  self-references again (no self-import) for any input spelling,
  because the directory — not the argv string — decides.
- Module-less inputs answer `""` exactly like before: their output
  has no importable identity, and a toolchain-path import they
  emit is a genuine import, not a self-reference.
- `cmd/gom`, `internal/stdlib`'s freshness test, and
  `build.Options.SelfImportPath`'s doc now point at
  `gomimport.EntryPath`; `TestEntryPath` and the new
  `TestBuildSelfImportPath` pin both module contexts.
