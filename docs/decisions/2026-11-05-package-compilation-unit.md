# The package compilation unit: shared top-level scope and .gomi imports

- 状態: 採用（[sketch: multifile package verification](../sketch/2026-11-05-multifile-package-verification.md) — V-6 vertical slice verified end to end）
- 日付: 2026-11-05

## Context

V-6 asks for a vertical slice: a small library of two `.gom` files plus
one `.gomi`, called from another Gom package and from Go. Two
open-questions entries block it:

- *Do the `.gom` files of one package share a top-level scope?*
  (open-questions) — the P1-11 driver compiles each file independently
  (decisions/2026-11-02-build-driver.md), so `a.gom` cannot see
  `b.gom`'s definitions today (E0201).
- Ordinary `import "path"` for Gom packages resolves to nothing — only
  `import go` and builtin-scope modules are implemented.

Sharing affects name resolution, type identity, initialization order,
and generated-name collisions, so the question is answered by a
package-granular front end, not by reading sibling `.gomi` files
(which would hide the package's private names and re-derive identity
from names instead of symbols).

## Decision

### 1. The package is the resolve/check unit; the file stays the emission unit

`resolve.Files` resolves all `.gom` files of one package against a
single *unit* scope: every file's type names, constructors, functions
and `let` binders land in the same scope, so a top-level name declared
in one file is visible in the others, and a duplicate across files is
one E0202 at the second definition (the data field
`first_defined_at.file` points across files). Each file still gets its
own child scope for its `import` declarations — imports stay
file-local, matching Go.

`check.Files` type-checks the unit as one environment — sibling
declarations share `vals`/`tyCons`, so type identity and
constructor/payload metadata need no oracle — and partitions the typed
declarations back into one `check.File` per input. Pattern checking,
lowering and codegen remain per-file.

Package clauses must agree: files of one unit declaring different
`package` names report E0605 at the later clause.

### 2. `import "path"` loads a Gom package through its .gomi

`internal/gomimport` implements `resolve.Loader` for `import "path"`:
it locates the package directory, compiles the package's `.gomi`, and
hands out member symbols with `Package = path` whose signatures are
requalified — the `.gomi`'s own type names become `Con{Qual: path}`,
builtin and other-package names stay untouched. The import path is the
Go import path of the *generated* package: generated references spell
`pkg.Name`/`pkg.Type`/`pkg.Type_Variant` exactly like `import go`
references.

Directory lookup reads the `go.mod` nearest the importing file
directly: the main module's root plus the module-relative path.
Gom packages outside the main module — workspace siblings and
dependency modules included — are not importable in v1: an
ordinary E0203 with the reason.

The loaded surface also feeds the later phases through the existing
seams: member signatures through `check.SignatureSource`, declared
arities through `lower.Env.FuncArity`, sum/record shapes through
`lower.Env.Types` and `pattern.Options.Decls`, and payload field names
for labeled constructor patterns through `resolve.Config.CtorFields`.
`check` learns the tables from the source itself: a
`SignatureSource` may implement `TypeDeclSource` so `.field`, record
literals, and named payload patterns type against the imported
declaration rather than failing through `builtinVariant`'s detached
entry.

### 3. Sibling facts reach later phases as data, not as re-resolution

- `lower.Env.Siblings []check.Decl` indexes the package's other files'
  declarations into the same arity/thunk/type tables local decls use.
- `core.File.Unit *UnitScope` carries the sibling package scope into
  Core IR validation: sibling `let` binders (package vars), sibling
  type names (builtin `Unit`/`Result` shadowing is package-wide), and
  fn/variant type-parameter arities. `Var` references to a `let` whose
  symbol carries a package — an imported `.gomi` `let` member —
  lower to qualified `pkg.Name` like Go member vars.
- `UnitScope.Decls []UnitDecl` carries each sibling value-declaration
  symbol *with the export status the unit's shared .gomi oracle gave
  it* (`lower.Env.Exported` is dir+package keyed, so every member
  agrees). Codegen claims package-scope names for the whole unit —
  own decls plus `Unit.Decls` — in one canonical order sorted by
  (emitted base, Gom name): a sibling reference and the declaring
  file therefore emit the same identifier, and a member's binder or
  temp cannot steal a package name a sibling declared. The export
  *decision* arrives on the node; codegen only computes spellings,
  which is its own job (design.md section 2.1).
- `pattern.Options.Decls []check.Decl` extends the exhaustiveness
  environment — sibling and imported type declarations alike.
- `build.Options.TypeDecls` supplies the imported type decls lazily
  (they are discovered while imports resolve).
- `core.CtorApp`/`core.CtorRef` carry `Fields` — the variant payload
  names — so codegen emits `pkg.Shape_Circle{Radius: v}` without
  consulting a type oracle (design.md 2.1: semantic facts settle in
  Core IR).

### Consequences

- Name resolution: mutual visibility is package-wide and
  order-independent within the unit; resolution order across files is
  the sorted input order, which only affects which side of an E0202
  reports `first_defined_at`.
- Type identity: shared by construction — one checker environment,
  pointer-identical symbols and type objects.
- Initialization order: Go's own package-level dependency order —
  a cross-file `let` reference initializes like a same-file one.
- Generated names: package-scope declarations cannot collide (E0202
  covers them); per-file generated names carry a deterministic file
  tag — `gomEq*`/`gomShow*` and the package-scope `tmp*` scrutinee
  and tuple temporaries of non-trivial `let` patterns (`pkgTmp`,
  `tmp<N>_<fnv32a>`; function-body `tmpN`s are block scope and stay
  untagged). Two same-named `test` blocks across
  files emit duplicate `TestX` functions — the same failure Go gives
  two same-named test functions, surfaced as E0001.
- The `.gomi` remains the cross-package contract: it is the single
  source read for imports, for conformance, and for `gom info`.

### Deferred boundaries (recorded in [problems/2026-11-05-gom-package-imports](../problems/2026-11-05-gom-package-imports.md))

- `pkg.Type.Ctor` qualification for imported types — `pkg.Ctor`
  works; the type-qualified spelling needs a resolve-side member table
  per type.
- Record-literal field *punning* on imported records and `pkg.Type`
  used where a local type's constructors imply field names — covered
  where `CtorFields`/`TypeDeclSource` supply the tables; anything
  beyond is future work.
- `.gomi`-side `import "path"` loads mechanically through the same
  loader but is not specified by spec §7 — and cyclic *interfaces*
  are detected while cyclic *implementations* are not (see below).
- Module-local lookup only: the `go.mod` nearest the importing file
  bounds resolvable packages — workspace siblings and dependency
  modules do not resolve.
- Same-named `test` blocks in sibling files emit duplicate Go
  `TestX` symbols — a Go-level error, not a Gom diagnostic.
- `.gom`-level cyclic imports are unchecked at the Gom level (the
  loader detects `.gomi`-level cycles) — generated Go reports the
  import cycle instead.
