# Gom package imports: deferred boundaries of the first slice

- 状態: 解消（2026-11-23 wave36 棚卸し — 4 の module scope も対応済み: `gomimport` の lookup は importing file の module context で `go list` を走らせ、dependency・replace target・workspace sibling の `.gomi` を解決する。全 6 項目が閉じた）
- 発見: 2026-11-05, V-6 vertical slice ([decision](../decisions/2026-11-05-package-compilation-unit.md), [verification](../sketch/2026-11-05-multifile-package-verification.md))

## Situation

`import "path"` loads a Gom package through its `.gomi`
(`internal/gomimport`), and the `.gom` files of one directory
compile as one package unit. The first slice deliberately leaves
these boundaries:

1. ~~**Type-qualified constructor spelling**~~ **解決**:
   `resolve.CtorSource` / `Config.Ctors` gives every type symbol a
   per-type constructor table; the loader publishes `CtorsOf` from
   the `.gomi`'s requalified variant symbols. `pkg.Type.Ctor`
   resolves in expression and pattern position — see
   [decision](../decisions/2026-11-05-pkg-type-ctor-spelling.md).
2. ~~**`.gom`-level cyclic imports are unchecked**~~ **解決**:
   the loader DFS-scans `.gom` import edges from the entry package;
   a cycle surfaces as `resolve.ImportCycleError` → E0606 at the
   import path span with the chain in `data.cycle`. See
   [decision](../decisions/2026-11-05-gom-implementation-import-cycles.md).
3. ~~**`.gomi`-side `import "path"` is mechanical, not specified**~~
   **解決** (2026-11-06): spec §7 defines `.gomi` imports — same
   forms as `.gom` (Gom / `import go` / alias), scoping the
   interface's own signatures only; member types are recorded
   under their absolute import path. `.gom`-edge cycles keep
   E0606; interface-only cycles surface as E0203 load failures.
   See [decision](../decisions/2026-11-06-gom-import-alias-and-gomi-imports.md),
   [verification](../sketch/2026-11-06-import-alias-and-gomi-imports-verification.md).
4. ~~**Module scope**~~ **解決**（2026-11-23 確認）: 現在の
   `gomimport` lookup は importing file の module context で
   `go list` を使い、main module 外の package — dependency
   module、replace target、go.work workspace sibling — の
   `.gomi` も解決する（internal/gomimport/loader.go の
   `extDirs`/`extMods`）。該当しないものだけが E0203 になる。
   残件なし。
5. ~~**Generated-name collisions across siblings**~~ **解決**:
   `core.UnitScope.Tests` records every sibling's emitted test
   base; a colliding name takes the file's fnv32a tag
   (`TestX_<tag>`), a unique name stays bare. See
   [decision](../decisions/2026-11-05-test-name-file-tag.md).
6. ~~**`gom dump` is single-file**~~ **解決**: `runDump` compiles
   the whole directory's `.gom` files through `build.CompileFiles`
   and dumps only the requested file's tree — sibling names
   resolve, diagnostics stay on stderr.

## Why it is a problem

1–3 are expressiveness/correctness gaps a real library hits as soon
as it composes ADTs across packages — 2 and 3 in particular let
invalid or unspecified programs pass `gom check`/`gom build` and
fail only inside the Go toolchain. 4 constrains which checkouts
can depend on which. 5 and 6 produce raw Go or misleading Gom
errors with no source mapping. (1, 2, 3, 5, 6 は解消 — 残るは
4 の module scope。)

## Related

- `internal/gomimport/loader.go`, `internal/resolve/resolve.go`
  (member tables), `internal/check/files.go`, `cmd/gom/cli.go`
  (`runDump`)

## Possible responses

- Add a per-type member table to the resolver for imported types
  (like `CtorFieldSource` but keyed `Type → Ctor`), unlocking
  `pkg.Type.Ctor` — also fixes record-literal punning gaps.
- Detect `.gom`-level cycles by tracking the package-import graph
  the loader already sees (a compile of `a` that transitively
  imports `a`'s own path); needs a per-module session shared
  between the importer and the imported package's own compile.
- Specify `.gomi` imports in spec §7 (allowed forms, cycle rules)
  and pin them with tests.
- Extend lookup to a `go.work`-aware or `go list -deps`-style
  scan; at minimum the main-module constraint stands documented.
- Detect duplicate `test` block names during unit checking and
  report them E0202-style (they are, semantically, package-scope
  decls).
- Route `gom dump` through `compileInputs` when a multi-file
  package is dumped.
