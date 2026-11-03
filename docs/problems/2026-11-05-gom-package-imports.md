# Gom package imports: deferred boundaries of the first slice

- 状態: 未解決
- 発見: 2026-11-05, V-6 vertical slice ([decision](../decisions/2026-11-05-package-compilation-unit.md), [verification](../sketch/2026-11-05-multifile-package-verification.md))

## Situation

`import "path"` loads a Gom package through its `.gomi`
(`internal/gomimport`), and the `.gom` files of one directory
compile as one package unit. The first slice deliberately leaves
these boundaries:

1. **Type-qualified constructor spelling**: `pkg.Ctor` works;
   `pkg.Type.Ctor` (the spelling a user reaches for when a package
   exports several ADTs) is not — resolve has no per-type member
   table for imported types.
2. **`.gom`-level cyclic imports are unchecked**: `.gomi`-level
   cycles are detected — the loader marks in-flight paths
   (`loading`) and reports "cyclic Gom import". But package `a`'s
   `.gom` files importing package `b` while `b`'s `.gom` files
   import `a` compiles cleanly (each side only reads the other's
   `.gomi`) and fails at `go build` with a Go import cycle — no
   Gom diagnostic.
3. **`.gomi`-side `import "path"` is mechanical, not specified**:
   an `import` inside a `.gomi` resolves through the same loader
   (nested interface loads work), but spec §7 does not define
   `.gomi` imports and no test pins the behavior — types a
   signature mentions from another Gom package ride on this.
4. **Module scope**: lookup reads the `go.mod` nearest the
   importing file — only packages inside the main module resolve;
   workspace siblings and dependency modules get a plain E0203.
5. **Generated-name collisions across siblings**: generated names
   carry per-file tags — `gomEq*`/`gomShow*` and the package-scope
   `tmp*` temporaries of non-trivial `let` patterns — and package
   decl names are E0202-checked, but two same-named `test` blocks
   in sibling files emit two
   `func TestX` — a duplicate-symbol failure from the Go compiler,
   not a Gom diagnostic.
6. **`gom dump` is single-file**: the debug command compiles one
   file at a time, so dumping a member of a multi-file package
   reports misleading E0201s for sibling names.

## Why it is a problem

1–3 are expressiveness/correctness gaps a real library hits as soon
as it composes ADTs across packages — 2 and 3 in particular let
invalid or unspecified programs pass `gom check`/`gom build` and
fail only inside the Go toolchain. 4 constrains which checkouts
can depend on which. 5 and 6 produce raw Go or misleading Gom
errors with no source mapping.

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
