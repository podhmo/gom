# Sketch: multi-file package unit — verification log

Verification report for
[decisions/2026-11-05-package-compilation-unit.md](../decisions/2026-11-05-package-compilation-unit.md):
the package-level compilation unit (shared top-level scope across
the `.gom` files of one directory) plus `.gomi`-backed Gom package
imports. Covers the V-6 vertical slice: *a small library of two
`.gom` files plus one `.gomi`, called from another Gom package and
from Go — one public function, one ADT, one function value.*

## Fixture (testdata/multifile/)

A nested Go module (`module example.com/multifile`, `replace
github.com/podhmo/gom => ../..`) with three packages:

| Package | Contents | Role |
| --- | --- | --- |
| `shapes/` | `shapes.gom` (`type Shape` ADT + `fn area` reading the sibling's `let pi`), `ops.gom` (`let pi`, exported `let tau`, `fn scale` matching on the sibling file's `Shape`/`Circle`/`Rect`, `let twice` — a `let`-declared function value), `shapes.gomi` | the library: two impl files, one interface, one ADT, public fns, one function value, one exported `let` |
| `gomapp/` | `main.gom` — `package main`, `import "example.com/multifile/shapes"`; `describe` matches the foreign ADT via `shapes.Circle`/`shapes.Rect` patterns, `main` calls `shapes.Area`/`shapes.Twice` and constructs `shapes.Circle 2.0` | Gom consumer |
| `app/` | `app_test.go` — calls `shapes.Area`, builds `Shape_Circle{Radius:}`/`Shape_Rect{W:,H:}` literals, calls `shapes.Scale`/`shapes.Twice[int64]` | Go consumer |

Generated `*.go` is gitignored under `shapes/` and `gomapp/`; the
e2e test copies the fixture into `t.TempDir`, repoints the
`replace` at the worktree, and regenerates.

## Driver: `cmd/gom.TestEndToEnd_MultifilePackage`

`invoke()` runs the real CLI in-process:

1. `gom check shapes gomapp` — both directories compile as two
   units in one invocation; `gomapp` resolves `import "…/shapes"`
   through `shapes.gomi` (the generated `shapes/*.go` does not
   exist yet — the interface is the whole contract).
2. `gom build shapes gomapp` — emits `shapes/shapes.go`,
   `shapes/ops.go` (both `package shapes`), `gomapp/main.go`.
3. `go build ./...`, `go test ./...`, `go run ./gomapp` inside the
   temp module (`GOWORK=off GOFLAGS=-mod=mod`).

## Verified facts

- **Shared scope, both directions**: `ops.gom` uses `Shape`,
  `Circle`, `Rect` from `shapes.gom` unqualified; no import or
  ordering ceremony. `go run ./gomapp` prints
  `12.56636 / circle 4 / 2` — `Scale`'s cross-file match and the
  `Circle` literal both executed.
- **Cross-package constructor fields**: `shapes.Circle 2.0` in
  `gomapp` emits `shapes.Shape_Circle{Radius: 2.0}` — the
  `.gomi`-carried field name, not the `V0` guess
  (problems/2026-11-02-foreign-adt-ctor-fields, now 解消).
- **Foreign ADT patterns**: `describe` type-switches on
  `shapes.Shape_Circle`/`shapes.Shape_Rect`; the exhaustiveness
  oracle enumerates the imported variants (no default arm in
  source, none needed).
- **Function value**: `let twice = fn f x -> f (f x)` exports as
  `func Twice[A any](f func(A) A, x A) A`; `shapes.Twice[int64](…)`,
  and the Go test calls it directly.
- **Package-scope `let`, both directions**: `ops.gom`'s private
  `let pi` lowers to `var pi` that `shapes.gom`'s `area` reads
  (`pi * tmp2` in `shapes.go`); exported `let tau` ships as
  `var Tau` — `shapes.tau` in `gomapp` emits `shapes.Tau`, the Go
  test reads `shapes.Tau`. Sibling `let`s reach Core IR via
  `core.File.Unit` (`UnitScope.Vars/Types/TParams`) so Validate's
  scope check and builtin-shadowing checks see the package view.
- **Atomicity**: `TestBuildDoesNotWritePartialPackage` (CLI,
  resolve/check failure) and `TestCompileFilesNoPartialOutputs`
  (driver, pattern failure) — one bad member means *no* `.go` for
  the package at either stage.
- **Unit-wide name claiming**: `core.UnitScope.Decls` carries each
  sibling value-decl sym plus its export flag (the shared .gomi
  oracle); codegen's `indexDecls` claims every unit decl — own and
  sibling — in one canonical order sorted by (emitted base, Gom
  name). A sibling ref spells exactly what the declaring file emits
  (`shapes.go` reads `Pi`, `ops.go` declares `var Pi` — the dirty
  `.gomi` exporting `let pi` is the e2e repro for what used to emit
  `undefined: pi`), a binder shadowing a package name mangles
  instead (`TestCompileFilesSiblingNameBlockedByBinder` →
  `answer1`), and a sibling `type Option`/`Result`/`Unit` joins
  `localT` package-wide (`TestCompileFilesSiblingTypeShadowsBuiltin`
  — the builtin map is no longer fooled by a sibling decl).
  Package-scope `tmp*` scrutinee/tuple temporaries of non-trivial
  `let` patterns carry the same `fnv32a(path)` file tag as `gomEq*`
  (`pkgTmp` — `TestCompileFilesTmpVarsDontCollide` builds both
  members' outputs in one scratch module).
- **Unit diagnostics**: `resolve.Files` tests pin E0202
  cross-file duplicates, E0605 mismatched package clauses (later
  clause wins the span), sibling-ctor resolution to the same
  `*Symbol`, and file-local imports (`fmt` in `a.gom` does not
  resolve in `b.gom`). `check.Files` tests pin sibling calls,
  sibling ADT typing, error spans landing on the *calling* file.
  The single-E0202 report for a same-file `let` dup is pinned by
  `testdata/spec/E0202_duplicate_let.gom`.
- **`gom test`/`gom fix` use the unit too**: `compileInputs` gains
  an options hook; `TestTestSiblingPackage` runs `gom test` on a
  two-file package whose test block references sibling decls
  (`assert_eq (bump base) 41` → pass, `b_test.go` emitted).
- **`gomimport` unit tests** (`loader_test.go`): module lookup
  (nearest `go.mod`, outside-module error, same-prefix non-match),
  the one-`.gomi` rule, member extraction (fn arity, `let` var,
  packaged type/ctor syms, payload fields, `TypeDecls`/`Variants`
  oracles), stable symbol identity and cached failures, the
  in-flight cycle guard, and `.gomi`-side nested imports.

## Where the package rules are pinned

The gomtest spec harness (`internal/gomtest`) compiles *one* file
per fixture, so package-level rules can't be `testdata/spec` cases:
they live in `internal/resolve/files_test.go` (scope/diagnostics),
`internal/check/files_test.go` (typing/partition),
`internal/build/files_test.go` (driver atomicity),
`internal/gomimport/loader_test.go` (import loading), and this
e2e — extending the harness to directory fixtures is future work
if spec-level cases become valuable.

## Unit seams exercised

`resolve.Files` → `check.Files` (merged env, per-file partition)
→ per-file `pattern` (sibling+imported `Decls`) → per-file `lower`
(`Env.Siblings`) → per-file `codegen` (`CtorApp.Fields`).
`internal/gomimport` serves `resolve.Loader`,
`check.SignatureSource`+`TypeDeclSource`, `resolve.CtorFieldSource`,
and lowering arity/type tables, chained with `goimport` so
`import go "fmt"` and `import "example.com/…"` coexist in one file.

## Residual boundaries (see problems/2026-11-05-gom-package-imports.md)

- `pkg.Type.Ctor` type-qualified spelling for imported constructors.
- Cyclic `.gom` package imports are unchecked at the Gom level —
  the generated Go reports the import cycle (`.gomi`-level cycles
  are detected by the loader).
- Module-local lookup: only packages inside the main module (the
  `go.mod` nearest the importing file) are importable — workspace
  siblings and dependency modules do not resolve.
- Same-named `test` blocks across sibling files emit duplicate Go
  `TestX` funcs — same failure Go gives same-named test functions.
- `gom dump` is a debug tool and still compiles single files — a
  multi-file package's members hit E0201 there.
