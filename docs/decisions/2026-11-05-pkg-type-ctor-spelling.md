# `pkg.Type.Ctor`: a per-type constructor table for imported types

- 状態: draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)）
- 日付: 2026-11-05

## Context

`Type.Ctor` qualifies a constructor by its declaring type; `pkg.Ctor`
qualifies it by its package. The two-hop form `pkg.Type.Ctor` — the
spelling a user reaches for when a package exports several ADTs —
did not resolve for *imported* types: the resolver's ctor tables are
keyed by unit-local type symbols, so after `pkg.Type` resolved to a
member, the outer selector was left silently unresolved and a stray
member-selection error surfaced in `check`. Pattern heads were worse:
`ctor-name` accepted at most one qualifier, so `shapes.Shape.Circle x`
was a parse error (E0101) before resolution ran.

## Decision

### 1. `Config.Ctors` gives every type symbol a per-type ctor table

`resolve` grows an optional source hook — `CtorSource` /
`Config.Ctors func(*Symbol) map[string]*Symbol` — consulted by a new
`ctorTable` helper after the unit's own `ctorsOf` map misses. The
resolver now treats the inner `pkg.Type` select as a namespace hop
(under the same `inQual` flag bare modules use), and when `pkg.Type`
resolved to a `KindType` member it resolves the outer selector through
that type's ctor table — local types answer from the unit table,
imported types from the loader. A miss is a constructor-specific
E0201 (`<Type> has no constructor <C>`), not a silent fallthrough.

`internal/gomimport`'s loader populates `typeCtors` while extracting
each loaded `.gomi` type's variants — the same requalified ctor
symbols its member table hands out, so `pkg.Ctor` and `pkg.Type.Ctor`
resolve to identical symbols and check needs no new metadata.
Record, alias, and abstract types answer no table.

### 2. `ctor-name` accepts a qualified path in patterns

The grammar's `ctor-name` gains a middle segment —
`[ ( ident | UIdent ) , { "." , UIdent } , "." ] , UIdent` — so
`pkg.Type.Ctor` parses as a left-leaning `Select` chain identical to
the expression spelling, and the existing `ctorRef`/`ctorOf` paths
(which already resolve a `Select` head through `Uses[sel]`) pick the
ctor symbol up unchanged. A lowercase mid-path segment still does not
parse as a ctor name (`a.b.C` is not a type qualifier), and a case
mismatch stays a resolve/check error.

### 3. `check` treats `pkg.Type` as a namespace hop, not a value

`checkSelect` builds the same namespace `MemberExpr` for the inner
`pkg.Type` receiver of a `pkg.Type.Ctor` select that it already builds
for a module or type qualifier — no runtime receiver is synthesized,
and the ctor call gets its signature from the resolved symbol the
`.gomi` tables registered.

## Consequences

- `shapes.Shape.Circle 1.0` and `match s { shapes.Shape.Circle r -> ... }`
  compile to `shapes.Shape_Circle{Radius: 1.0}` / `case
  shapes.Shape_Circle:` — the same generated names `pkg.Circle` uses.
- `T.C` ambiguity rules are unchanged: the outer sel resolves against
  the *type's* ctor table only; `mod.Type` still resolves through the
  member table first.
- Select chains deeper than two (`a.B.C.D`) resolve each namespace hop
  and error at the first selector that is not a type member or ctor —
  no new grammar production was needed.

## Alternatives

- *Keep the flat member table and alias `pkg.Type.Ctor` to `pkg.Ctor` at
  parse time.* Rejected: it loses the type the user meant, so
  `pkg.OtherType.SomeCtor` could resolve to `SomeCtor` of a different
  type and the diagnostic would name the wrong namespace.
- *Resolve the outer selector in `check` instead of `resolve`.*
  Rejected by the layering rule: name resolution lives in `resolve`;
  `check` only reads `Uses`.
