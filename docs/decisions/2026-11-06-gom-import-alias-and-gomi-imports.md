# Define `import alias "path"` and `.gomi` imports in the spec

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-import-alias-and-gomi-imports-verification.md)）
- 日付: 2026-11-06

## Context

The grammar has always accepted an optional identifier between the
`import` keyword (and optional `go`) and the path string, and the
parser/resolver implement it: `import l "example.com/t/lib"` binds
`l` as the local module name and the generated Go file carries the
alias verbatim (`import l "example.com/t/lib"`). The spec only ever
documents `import "path"` and `import go "path"`, so the alias form
was provisional ([problem](../problems/2026-11-01-gom-import-syntax.md)).

Separately, `.gomi` interface files accept `import` declarations
and the loader resolves them — a signature can name
`lib.Color` or `io.Reader` in an interface. Nested `.gomi` imports
resolve transitively and cycles are rejected with E0606, the same
diagnostic `.gom` import cycles use. This, too, was never specified
([problem](../problems/2026-11-05-gom-package-imports.md)).

## Options

- **(i) Specify the implemented behaviour** — alias renames the
  locally bound module name (for both Gom and Go imports), and a
  `.gomi` may carry imports whose names scope that interface's
  signatures only.
- **(ii) Restrict** — reject the alias token and `.gomi` imports at
  parse/resolve time, shrinking the grammar to what the spec showed.

## Decision

Option (i): specify. The machinery is complete, end-to-end correct
(alias survives into generated imports, `.gomi` signature types
resolve through imported member tables, cycles produce E0606), and
already relied on by `internal/gomimport` tests. Restricting would
remove working surface — `.gomi` imports in particular are how an
interface names types from other packages, which is unavoidable
once signatures mention foreign types — for no correctness gain.

Semantics pinned by the spec text:

- Bound name: the alias when written, otherwise the last path
  segment (so `import "example.com/t/lib"` binds `lib` even when the
  package clause disagrees). The emitted Go import carries the alias
  only when the bound name differs from the path's base — so a
  directory whose last segment does not match its `package` clause
  still breaks generated code unless the writer supplies an
  explicit alias (`import other "example.com/t/lib"`). Emitting the
  bound-name alias unconditionally (or piping the real package name
  through to codegen) is follow-up work, not part of this wording
  decision.
- `.gomi` imports are file-local like `.gom` imports. Alias names in
  a `.gomi` never appear in the public surface: member types are
  recorded under their absolute import path, so a consumer never
  needs the interface's local names.
- Cycles: import cycles that run through `.gom` (implementation)
  edges report E0606 with the cycle path — the impl scan walks only
  `*.gom` imports. A cycle that exists purely between `.gomi` files
  is caught instead by the loader's in-flight guard and surfaces as
  an ordinary E0203 load failure, without the cycle path.

## Consequences

- `docs/spec.md` §5 documents the alias form and the bound-name
  rule; §7 documents `.gomi` imports (forms, signature-only scope,
  cycle rule).
- `docs/grammar.ebnf` drops the `[PROVISIONAL gom-import-syntax]`
  tags on `import-decl` and `iface-decl`.
- The spec suite cannot pin multi-package imports (no module
  context), so the alias is pinned by an `ok_` fixture using
  `import go alias "path"`; `.gomi` imports stay covered by
  `internal/gomimport` tests and a spec-suite dir case.
- `docs/problems/2026-11-01-gom-import-syntax.md` and
  `docs/problems/2026-11-05-gom-package-imports.md` item 3 are
  resolved in the part covering alias + `.gomi` import surface.
