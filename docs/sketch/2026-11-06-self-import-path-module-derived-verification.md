# Verification: module-derived SelfImportPath

- Date: 2026-11-06
- Decision: [self-import-path-module-derived](../decisions/2026-11-06-self-import-path-module-derived.md)

## The reported footgun, reproduced then fixed

Before the change, `cd stdlib/list && gom build -o out.go list.gom`
(basename input — no `stdlib/list` segments to lex) emitted:

```go
package list

import (
	"github.com/podhmo/gom/rt"
	"github.com/podhmo/gom/stdlib/list"   // ← self-import
	"github.com/podhmo/gom/stdlib/option"
)
func Of[A any](x A) list.List[A] { ... }
```

`go build` would reject this with `package
github.com/podhmo/gom/stdlib/list imports github.com/podhmo/gom/
stdlib/list: import cycle not allowed`.

After the change the same command emits:

```go
import (
	"github.com/podhmo/gom/rt"
	"github.com/podhmo/gom/stdlib/option"
)
func Of[A any](x A) List[A] { ... }
```

— bare self-references, no self-import, matching the committed
`list.go`'s convention.

## Both module contexts pinned by tests

`cmd/gom`'s TestBuildSelfImportPath:

- toolchain side: builds the real `stdlib/list/list.gom` by
  absolute path with `-o` into a temp dir; asserts no
  `"github.com/podhmo/gom/stdlib/list"` import, bare `List[A]`
  self-references, and the sibling `stdlib/option` import intact.
- user-layout side: a `module example.com/m` fixture with
  `stdlib/list/main.gom` using ambient `List` members; asserts the
  generated file *keeps* `import
  "github.com/podhmo/gom/stdlib/list"` and qualified
  `list.Of[int64](`/`list.Length[int64](` references — the old
  lexical rule suppressed that import and emitted bare `Of`/`Length`
  (undefined in the user's package).

`internal/stdlib`'s TestEntryPath pins `gomimport.EntryPath` itself:
in-module dirs (including the toolchain `stdlib/<pkg>` and an
unrelated `testdata/spec`), a foreign module reusing the
`stdlib/list` layout (`example.com/m/stdlib/list`), and a
module-less dir (`""`).

`TestStdlibGeneratedFilesAreFresh` — which regenerates every stdlib
package through `ml.EntryPath()` — still matches the committed
`.go` files, so the new source of `SelfImportPath` reproduces the
old convention where it was correct.

## Source-level self-import remains rejected

An explicit `import "example.com/t/self"` inside that package's own
`.gom` still reports E0606
(`import cycle: example.com/t/self -> example.com/t/self`) at the
import position — checked against a scratch module. Suppression
never sees it; the cycle check fires first.
