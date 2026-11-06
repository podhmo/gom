# Always emit the bound import name as a generated alias

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-06-generated-import-bound-name-alias-verification.md)。spec.md §5 へ反映済み）
- 日付: 2026-11-06
- Supersedes: [gom-import-alias-and-gomi-imports](2026-11-06-gom-import-alias-and-gomi-imports.md) のうち「別名付き import の生成規則」（bound name == path base のとき無修飾 import を出す規則を、常に束縛名を別名として出す規則に置き換える）

## Context

`import "example.com/m/lib"` binds `lib`, and every generated
reference spells `lib.X`. Codegen emitted an unqualified
`import "example.com/m/lib"` whenever the bound name equalled the
path's last element — but Go binds an unqualified import under the
imported package's own `package` clause, not the path base. A `lib/`
directory declaring `package other` therefore generated
`import "example.com/m/lib"` (bound as `other`) next to `lib.X`
references — `undefined: lib.X` at `go build`
([gaps](../implementation-gaps.md)「生成 import の別名は束縛名が
パス末尾と違うときだけ」, wave-11 wording-hygiene F-1).

The same hazard already existed for `import go`: `math/rand/v2`'s
package clause is `rand`, so `import go "math/rand/v2"` binds `v2`
but an unqualified import binds `rand` — `v2.IntN` never compiled
either.

## Options

- **(i) Always emit `name "path"`** — the bound name is asserted
  explicitly at every generated import clause. Go then binds the
  name the source-level references spell, whatever clause the
  imported package declares.
- **(ii) Pipe the real package-clause name through to codegen** —
  keep the conditional emit, comparing the bound name against the
  imported package's true clause instead of the path base.

## Decision

Option (i): always emit the bound name.

Correctness by construction: `g.imports` already maps each path to
exactly the identifier the generated references use, so emitting it
unconditionally needs no new information channel. Option (ii) would
need a "true package name" oracle piped resolve → check → core →
codegen, and for Gom packages the only witness the loader reads is
the `.gomi` clause — which can itself disagree with the `.gom`
clause that actually names the generated package (a conformance
error, but the importer must not rely on it). Option (ii) therefore
cannot be sound without additionally parsing the imported package's
`.gom` files; option (i) needs none of that and also closes the
`import go "math/rand/v2"` hole.

The cost is cosmetic: aliases that repeat the path base
(`fmt "fmt"`, `rt "github.com/podhmo/gom/rt"`) and a one-time golden
regeneration. Generated files are `DO NOT EDIT` artifacts; the
explicit alias is unambiguous for readers and debuggers alike.

## Consequences

- `internal/codegen/codegen.go` `render` emits `name "path"` for
  every collected import (main and `_test.go` halves alike).
- `testdata/golden/` and `internal/codegen/testdata/` regenerated —
  every generated import is now aliased.
- spec §5 no longer needs the "write an explicit alias to rescue a
  clause mismatch" escape: `lib "example.com/m/lib"` binds `lib`
  whatever the clause says. An explicit `import alias "path"` is
  still the way to choose a different local name.
- Self-import stays impossible: `SelfImportPath`/`EntryPath`
  references emit unqualified before the import table is consulted,
  so they never reach the import clause.
- A `.gom` package whose clause disagrees with its `.gomi` remains
  a conformance error on that package's own compile (E0603) — the
  alias emit does not legitimise it.
