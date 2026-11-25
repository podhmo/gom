# `.gomi` interface files

Condensed from `docs/spec.md` chapter 7. The `.gomi` is Gom's
`.mli`: a package's public API, written separately from its `.gom`
implementation. Dependents compile against the `.gomi` alone.

**Status:** `.gomi` files parse, check, and are enforced: `gom
check` on a directory containing `x.gom` + `x.gomi` verifies
implementation↔interface agreement (E0601–E0603) and the interface's
listed names emit as exported Go identifiers (spec section 5). The
canonical example is the standard library itself —
`stdlib/{list,option,result}/*.gomi` is the public surface the
compiler resolves `List.map`/`Option.get`/`Result.map` from.
`import "mod/path"` (no `go` keyword) resolves a Gom package
through its `.gomi` — spelled as a full module path
(`import "example.com/m/shapes"`), then members spell
`shapes.Shape`, `shapes.Circle`, `shapes.area`. Resolution is
not limited to the main module: any package a module in the
build list provides works (`go list -m` resolution, vendored
builds honored). `import alias "mod/path"` renames the
file-local binding; without an alias the bound name is the
path's last segment.

The compiler's own stdlib also spells short: `import "list"`,
`import "option"`, `import "result"` unfold to
`github.com/podhmo/gom/stdlib/<name>` — handy in external
modules where the full path is noise.

## Syntax

A `.gomi` contains **only** `package`, `import` declarations, type
declarations, `fn`/`let` signatures, and `///` doc comments — no
expressions, no bodies (E0602). Imports in a `.gomi` use the same
forms as `.gom` (`import "path"`, `import go "path"`, aliased) and
scope the interface's own signatures only — a signature can name
`io.Reader` or another Gom package's `shapes.Shape`, and consumers
never see the interface's local import names.

```gomi
package shapes

/// A shape. Constructors are exposed, so users may match.
type Shape =
  | Circle (radius: Float)
  | Rect (w: Float) (h: Float)
  | Empty

/// Abstract type: name only, representation hidden.
type Canvas

fn area (s: Shape) -> Float
fn new_canvas (~width: Int) (~height: Int) -> Canvas
fn draw (c: Canvas) (s: Shape) -> Canvas
let unit_circle : Shape
```

Rules:

- One `.gomi` per package: `<pkgname>.gomi` — a file whose name
  doesn't match its package (or a second `.gomi`) is stray (E0604).
- `fn` signatures use the same parameter syntax as `.gom` —
  argument count, names, and labels are interface surface.
- A sum type with full constructors is **transparent** (callers may
  construct and `match`); a bare `type Canvas` is **abstract**
  (opaque to callers).
- Implementation types must agree with the interface — v1 allows
  only type-variable renaming (E0603). A `let` signature MAY
  quantify type variables: `let empty : List a` publishes the
  generalized value the impl's `let empty : List a = []`
  generalizes into. A non-function polymorphic member crosses
  the `.gomi` as a thunk — the member reads as a plain value
  (`m.empty : List a` at use sites) but emits `m.Empty[T]()`;
  a function-typed one (`let ident : (a) -> a`) stays a real
  `fn` member and is called normally (spec §7).
- `package main` needs no `.gomi` (binaries export nothing);
  every other package requires one (E0601). Sibling `.gom` files
  must all declare the same `package` name (E0605).
- Public names on the Go side are the capitalized form
  (`.gomi` `fn area` → Go `func Area`); unlisted definitions stay
  lowercase-private. Go keyword collisions mangle with a `_` suffix.
- Exported members whose emitted Go names collide — e.g.
  `type Foo` plus `let foo` (the value would dedup to `Foo1`,
  which importers cannot spell) — are an error on the declaring
  package (E0607); rename so the emitted names differ.
- A package that fails conformance (E0601/E0603) emits **no**
  output — partial `.go` files are withheld rather than left
  broken.
- Recompilation of dependents triggers only on interface changes
  (comments/formatting excluded) — implementation-only edits don't
  cascade. `gom build` keys its incremental cache on this interface
  hash plus a canonical token hash of each source (test decls
  excluded); `GOM_BUILD_NOCACHE=1` disables it and
  `GOM_BUILD_CACHE_DIR` relocates it.
- `//gom:` directive lines carry annotations across the package
  boundary. `//gom:noescape` / `//gom:result-notaint` are re-inferred
  from ordinary bodies on regeneration; for an extern `fn` or
  function-typed extern `let` there is no inspectable body, so the
  prior lines carry within the same package while the canonical
  signature is unchanged. A rewritten extern signature drops them rather
  than moving a positional guarantee onto a different parameter.
  `//gom:mutable` is an author-written claim and also carries within the
  same package. An `@method` line before an
  `fn` keeps the record→Go-interface method set.
- Drafting command: `gom info FILE|DIR...` (`moon info`-style)
  writes or updates the `.gomi` draft; `-o -` prints it. A stdlib
  short name (for example `gom info list`) prints the existing
  interface read-only when no same-named file/dir exists. `gom
  gomi` is a rename stub — it prints the pointer and exits 2.

Importers spell members `pkg.Name`; variant constructors spell
`pkg.Ctor` or the explicit `pkg.Type.Ctor`.

## Visibility

A `.gomi`-listed member exports as its capitalized Go name; an
unlisted top-level definition stays a lowercase-private Go
identifier. `import "mod/path"` resolves a Gom package through its
`.gomi` — including packages from other modules in the build
list and the stdlib short names (`import "list"` …).
`.gom`-side import cycles are detected as E0606 (only the actual
cycle members are blamed); a cycle that exists only between
`.gomi` files surfaces as an ordinary E0203 import-load failure
instead.
