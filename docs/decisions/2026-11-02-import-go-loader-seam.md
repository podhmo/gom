# The package-loader seam for `import` and `import go`

- 状態: draft
- 日付: 2026-11-02

Name resolution needs the member table of every imported package —
which is exactly the dependency information P1-4 must not load
itself (`import go` loading is P1-9, `.gomi` reading is P4-1). This
decision fixes the seam that keeps the resolver independent of how
packages are found.

## Contract

`internal/resolve` exposes three small types:

- **`Loader`** — `Load(path string, goPkg bool) (*Package, error)`.
  The single extension point: `internal/goimport` implements it over
  `go/packages` (P1-9) and the `.gomi` reader implements it for Gom
  packages (P4-1). `goPkg` distinguishes `import go "fmt"` from
  `import "foo"` — the two may load through different mechanisms.
- **`Package`** — `{ Path string, Members map[string]*Symbol }`: the
  exported surface as a flat name→symbol table. Member symbols are
  created by whoever loads the package and shared by every importing
  file, so symbol identity stays package-granular. Members of a Go
  package carry `IsGo`; their `Package` field holds the import path.
- **`Config{Loader, Modules}`** — per-file configuration. A nil
  Loader means "no packages exist": every `import` reports E0203.
  `Modules` injects member tables for the builtin stdlib modules
  (`List.map`, `Chan.send`, ...) until the real stdlib exists
  (P4-8); `Go` is hardcoded and not looked up there.
- **`MapLoader`** — a `map[string]*Package` Loader for tests and for
  phases that stub their dependencies; Go packages are keyed
  `"go:" + path`.

## Failure and recovery

`bindImport` binds the module alias (`KindModule`, `Package` =
declared path, `IsGo` for `import go`) *before* loading, so a failed
import still leaves a consistent binding — `fmt.Println` on a failed
`import go "fmt"` marks the module's table `nil` and suppresses
member diagnostics rather than cascading; the only diagnostic is the
E0203 at the import's path literal. Loader calls are cached per
(path, goPkg) so duplicate imports of one path share a Package.

## `extern go` overlay

An `extern go "io"` block contributes symbols per path into a table
merged over the loaded members when the matching `import go "io"`
runs — spec 8.4's "extern overrides the derived bindings". Unqualified
type names inside an extern signature resolve against the extern'd
package's members first (`fn Copy (~dst: Writer)` means `io.Writer`),
then the ordinary scope. The extern's own entries are `KindFunc`,
`IsGo`, `Package` = the extern path.

## What P1-9 must provide

- A `Loader` over `go/packages` producing `Package` values whose
  member symbols have `IsGo`/`Package` set — including `KindMethod`
  members where applicable, and the `KindType` members `q.T` type
  names resolve to.
- The `.gomi` reader eventually does the same for Gom packages so
  cross-file Gom references resolve through this seam instead of
  bespoke machinery.
- Neither needs resolver changes: `members` tables and `Members`
  lookups already consume arbitrary symbol sets.
