# Name resolution: scopes, shadowing, and constructor lookup

- 状態: draft
- 日付: 2026-11-02

P1-4 implements `internal/resolve`: `resolve.File` walks a
`*syntax.File` and returns `*resolve.Resolved` (the Defs/Uses side
tables of [ast-representation](2026-11-02-ast-representation.md))
plus `diag.List`. The spec fixes the rules users see (sequential
`let` scopes, `T.Ctor` constructor qualification, `@test`
visibility); everything the spec leaves open is decided here.

## Scope model

Lexical scopes form a linked chain. Each level owns two name tables
— **terms** (variables, functions, constructors, module aliases,
compiler builtins) and **types** (declared and builtin type names) —
because Gom's value and type namespaces are separate (`List` the
type and `List` the module coexist). The chain, outermost first:

1. **builtin scope** — builtin types (spec 3.1), `Some`/`None`/`Ok`/
   `Err` as ordinary `KindCtor` symbols of `Option`/`Result`,
   compiler functions (`assert`, `assert_eq`, `todo`, `not`), the
   stdlib module names (`List`, `Option`, ..., `Task`), and the
   compiler-owned `Go` namespace (`Package == "Go"`, matching the
   reserved `types.Con` qualifier).
2. **file scope** — every top-level name, bound in a collection
   pass before any body resolves, so top-level names are mutually
   visible regardless of source order. Imports are bound in the same
   pass so `pkg.Ctor` pattern heads resolve.
3. **inner scopes** — function parameter scope, one scope per
   `let`-group in a block, per match arm, per `for` loop, per select
   receive arm, per lambda.

A block is one fresh scope, and each `let` pushes a further child
scope holding only that let's binders — this is how spec 1.1.4
("let は以降のスコープを導入する") is modeled: the binder is visible
to exactly the following statements, and its initializer is resolved
in the enclosing scope so `let x = x` refers to an outer `x` (or is
undefined).

Type variables do **not** live in these scopes. A `tvScope` chain
parallel to `scope` tracks them; see below.

## Shadowing and duplicate definitions

- Inner scopes silently shadow outer ones — no warning; spec is
  silent and standard lexical shadowing applies.
- Two binders of the same name in the **same scope level** are
  E0202: two `let`s (or a `let` and an `fn`) at top level, two
  parameters `fn f x x`, two type parameters, two constructors of
  one file, two fields of one variant/record.
- One scope level per `let` in a block means `{ let x = 1; let x = 2
  }` shadows rather than duplicates — consistent with "再代入は不可
  （シャドーイングは可）".
- A pattern that binds the same name twice along one alternative
  path — `(x, x)`, `A x x` — is E0202. Parameters of one signature
  are sibling binders on one path.
- **Or-pattern alternatives merge by name**: in `A x | B x` both
  binders' Defs entries point at *one* shared `KindVar` symbol — the
  arm body sees a single variable, and "every alternative must bind
  the same set" stays E0304's job in check. The union of all
  alternatives' names is bound so the body still resolves when the
  sets differ. Nested or-patterns reuse the enclosing merge table,
  so sharing is by name across the whole outermost or-pattern.

## Constructor resolution

Variant constructors are bound in two places: the file term scope
(unqualified `Some x`, `Circle r`) and a per-type table
(`ctorsOf[typeSym][name]`) backing the qualified forms. This makes
same-file constructor name collisions ordinary duplicates (E0202) —
the term namespace is shared — while `Type.Ctor` always
disambiguates.

`q.C` is resolved by trying, in order:

1. `q` as a term: a **module** resolves `C` in the member table —
   the member must be `KindCtor` in pattern position, any kind in
   expression position (`mod.Type` used as a value is check's
   problem).
2. else `q` as a **type**: `C` must be one of its constructors
   (`Shape.Circle`, `Option.Some`).

So `Option.Some` works even though the builtin `Option` **module**
shadows the type in term position: the member miss falls back to
the type of the same name. The same fallback applies when `q`
resolves to a term that is not a module (a `fn Shape` shadowing
`type Shape`): `q.C` then reads as type-qualified and the
qualifier's Uses entry records the *type*, so qualifier and
selector stay consistent. A qualifier that is bound but names
neither reports "is not a package" (E0201); an unbound qualifier
reports "undefined name". Pattern heads require a constructor —
`Some x` where `Some` is a function reports "`Some` is not a
constructor".

`{ name = pat }` fields and `~label:` arguments of a constructor
pattern resolve against the constructor's declared payload names —
but only for file-local constructors, whose variant decls are
available; foreign and builtin constructors' field names are left
for check (the constructor's type is needed to find them). The same
goes for `{ name = pat }` record patterns with no constructor head
at all — the expected record type names the fields.

Ambiguity between an unqualified ctor reference and an imported
package member cannot arise: unqualified names only see the file and
builtin scopes.

## Type-variable scoping

The spec says only that type variables are lowercase; where they
bind is unspecified. The resolver treats a **signature** — the
parameter types plus the return type of one `fn`, one `let : T`, or
one extern entry — as one implicit type-variable scope: the first
occurrence of `a` binds it (a `Defs` entry) and later occurrences
use it (`fn id (x: a) -> a` shares one variable). `type X a = ...`
declares its parameters instead, so undeclared variables in a type
declaration body are E0201, never implicitly bound.

Annotations nested inside a body (ascriptions, `let : T`, lambda
parameter types) see the enclosing signature's variables through the
tvScope parent chain — `(x : a)` inside `fn id (x: a) -> a = ...`
names the same `a`, like a lightweight ScopedTypeVariables — and
bind new implicit variables locally to the annotation.

## @test visibility

`@test` fn/let declarations produce symbols with `TestOnly` set
(let-declaration binders are marked by walking the pattern).
References from non-test code report E0802 but still resolve —
recovery keeps the tables complete. `test "..."` blocks and `@test`
bodies resolve in a test context. Since the AST has no general
attribute list (only `TestOnly`), unknown-attribute handling is
impossible today — see problems/2026-11-02-attribute-representation.md.

## What resolve leaves in the tables — and what it does not

Every `*syntax.Ident` gets exactly one Defs entry per binder
(pointer identity is semantic identity). Sharing one symbol across
two binder idents happens in three places: or-pattern alternatives
(`A x | B x`), a `~name` parameter label on a same-named VarPat,
and a variant/record Field whose Name and Label both exist. A
bodiless signature — a `.gomi` `fn` or an extern entry — still
binds its parameters into Defs; the scope they form is simply never
used by a body. Reference-position idents get Uses entries when
resolved — including resolved-but-wrong-kind references, where the
kind mismatch is diagnosed separately on top of the resolution.
Positions deliberately left out — the "absent" third state
documented on `resolve.File`:

- `f x` call-site `~labels`, `~to: e` argument labels, and
  `fn-type`'s `~name:` parameter names — external names, resolved
  against the callee's signature at check time.
- `p.name` member selectors and `{ name = e }` record-literal field
  names — type-directed, resolved by check (the expected type names
  the field).
- members of a package that failed to load — already diagnosed by
  E0203 at the import site.
- the `package` clause name — not a symbol occurrence in a
  single-file resolver.

A param label `~x` on a `VarPat` binder of the same name shares the
variable's Defs symbol; on any other pattern it gets its own
`KindVar` symbol so the ident is still resolved.

Member selection deeper than `pkg.name` (`a.b.c`) resolves only the
first hop; nested qualification is left for check, as member tables
are flat in P1-4.
