# Verification: always emit the bound import name as a generated alias

- Date: 2026-11-06
- Decision: [generated-import-bound-name-alias](../decisions/2026-11-06-generated-import-bound-name-alias.md)

Prototype: `internal/codegen`'s `render` emits `name "path"` for every
collected import. Verified under a scratch module `example.com/t`
(outside the repo) with a `gom` binary built at this branch.

## Package-clause mismatch — the reported bug

`mismatch/mismatch.gom` declares `package other` with a conforming
`other.gomi`; `app2/main.gom` does `import "example.com/t/mismatch"`
and calls `mismatch.double 3`.

Before: the generated `app2/main.go` carried an unqualified
`import "example.com/t/mismatch"` — Go binds it as `other`, so
`mismatch.Double(3)` was `undefined` at `go build`.

After:

```go
import (
	mismatch "example.com/t/mismatch"
)

mismatch.Double(3)
```

`go build ./...` and `go vet ./...` pass over the whole generated
module (`mismatch.go` declares `package other`; `main.go` references
it through the explicit alias). `lib.X` is honest regardless of the
clause the importer never sees.

## `import go` clause mismatch — same hole, closed

`import go "math/rand/v2"` binds `v2`; the package clause is `rand`.
The unqualified import previously emitted bound `rand`, so
`v2.IntN`-style references could not compile. The bound name is now
emitted explicitly (`v2 "math/rand/v2"`) — same fix, no special case.

## Alias and collision forms unchanged

- `import go f "fmt"` → `f "fmt"` (explicit alias flows through as
  before).
- Collision-suffixed bindings emit their suffixed name verbatim
  (`fmt_ "fmt"` keeps `fmt_.X` references valid).
- Self-imports stay unreachable: `SelfImportPath` references emit
  unqualified before the import table is consulted, so no module's
  own path enters the clause.

## Golden regeneration

`go test ./internal/codegen -update` +
`go test ./internal/build -run TestGolden -update` rewrote 41
`testdata/golden/` outputs plus the codegen fixtures — every
generated import is now aliased (`fmt "fmt"`,
`result "github.com/podhmo/gom/stdlib/result"`, …). No behavioural
diff: the bound identifier each line asserts is exactly the one
references already spelled.
