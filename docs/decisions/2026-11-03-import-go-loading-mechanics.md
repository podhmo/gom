# How `import go` loads Go packages: go/types + go/importer over `go list -export`

- 状態: 採用（P1-9 の `internal/goimport` が実装・検証した — E2E で `strconv`/`bytes`/`os`/`fmt` の呼び出しが生成コードの build・run まで通る）
- 日付: 2026-11-03

P1-9 needs a concrete loading mechanism behind the `resolve.Loader`
seam ([import-go-loader-seam](2026-11-02-import-go-loader-seam.md)).
P0-5 assumed `golang.org/x/tools/go/packages`; the repository has no
x/tools dependency yet, so the choice is revisited here.

## Decision

`internal/goimport.Loader` loads a Go package with the standard
library only:

- `go/importer.ForCompiler(fset, "gc", lookup)` as the
  `go/types.Importer`. One importer (and one `token.FileSet`) is
  shared per Loader so package-level `types.Identical` /
  named-type identity holds — P0-5 showed identity is lost across
  importer sessions.
- `lookup(path)` resolves a package to its export data by running
  `go list -export -f '{{.Export}}' <path>` in the configured module
  directory and opening the returned file. The same call works for
  standard-library packages (export data lands in the build cache)
  and for module dependencies and the module's own packages
  (`github.com/podhmo/gom/rt` was verified to load this way).
- Results are cached per import path; `Load(path, goPkg=false)`
  reports "not loadable yet" (the .gomi reader is P4-1) so a plain
  `import` produces E0203 rather than falling back to Go loading.

## Why not go/packages

- No new module dependency is required for the packages P1-9 must
  cover. Export data is complete for imported *signatures* —
  everything the checker needs is in `pkg.Scope()` and method sets.
- What is lost: source-level positions for member definitions
  (irrelevant — imported symbols have no Gom Def anyway) and
  syntax-level information `go/packages` could offer for gopls-style
  tooling (not needed by the pipeline).
- The per-import `go list` subprocess is a startup cost (~50–100 ms
  per package). A later optimization can batch `go list -export -deps`
  once per build; the seam does not change.

## Constraints this preserves

- **One session per compile**: the Loader holds the importer for its
  lifetime; NamedRefs minted by it are comparable within one compile.
- **Only `import go` reaches it**: `Load` receives `goPkg` and refuses
  Gom paths.
- **Failures are E0203**: any importer error (missing package, no
  export data, missing `go` binary) propagates to the resolver's
  existing load-failure path.
