# `.gomi` interface files

Condensed from `docs/spec.md` chapter 7. The `.gomi` is Gom's
`.mli`: a package's public API, written separately from its `.gom`
implementation. Dependents compile against the `.gomi` alone.

**Status:** `.gomi` files parse and check today (`gom check
x.gomi`); enforcement of "library package must have exactly one" and
implementation↔interface agreement is task P4-1 (the checking codes
E0601–E0603 are reserved). A Gom-side `import "pkg"` (non-`go`) is
still E0203 — only `import go "pkg"` resolves today.

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

- One `.gomi` per package: `<pkgname>.gomi`.
- `fn` signatures use the same parameter syntax as `.gom` —
  argument count, names, and labels are interface surface.
- A sum type with full constructors is **transparent** (callers may
  construct and `match`); a bare `type Canvas` is **abstract**
  (opaque to callers).
- Implementation types must agree with the interface — v1 allows
  only type-variable renaming (E0603).
- `package main` needs no `.gomi` (binaries export nothing);
  every other package requires one (E0601).
- Public names on the Go side are the capitalized form
  (`.gomi` `fn area` → Go `func Area`); unlisted definitions stay
  lowercase-private. Go keyword collisions mangle with a `_` suffix.
- Recompilation of dependents triggers only on interface changes
  (comments/formatting excluded) — implementation-only edits don't
  cascade. This is the build-cache key.
- Drafting command: planned as `gom info` (`moon info`-style; the
  CLI usage text still lists `gomi`) — not implemented.

## Visibility without `.gomi` writing today

Until P4-1 lands there is no `pub`-style control to exercise: every
top-level definition in a single-file package is visible to that
file. Write the `.gomi` you intend and check it — it is verified
syntax-checked — but cross-package imports remain future work.
