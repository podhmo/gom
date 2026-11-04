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
through its `.gomi` inside the same Go module — spelled as a full
module path (`import "example.com/m/shapes"`), then members spell
`shapes.Shape`, `shapes.Circle`, `shapes.area`.

## Syntax

A `.gomi` contains **only** `package`, type declarations, `fn`/`let`
signatures, and `///` doc comments — no expressions, no bodies
(E0602).

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
  only type-variable renaming (E0603). A polymorphic `let`
  signature is rejected for now (E0602 — thunk representation is
  undecided).
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
  cascade. This is the build-cache key (the interface hash exists;
  the incremental cache consuming it is still unimplemented).
- Drafting command: `gom info FILE|DIR...` (`moon info`-style)
  writes or updates the `.gomi` draft; `-o -` prints it. `gom
  gomi` is a rename stub — it prints the pointer and exits 2.

Importers spell members `pkg.Name`; variant constructors spell
`pkg.Ctor` or the explicit `pkg.Type.Ctor`.

## Visibility

A `.gomi`-listed member exports as its capitalized Go name; an
unlisted top-level definition stays a lowercase-private Go
identifier. `import "mod/path"` resolves a Gom package through its
`.gomi` — the importing file must sit in the same Go module.
`.gom`-side import cycles are detected as E0606 (only the actual
cycle members are blamed); `.gomi`-side import rules are not yet
specified (see problems/2026-11-05-gom-package-imports).
