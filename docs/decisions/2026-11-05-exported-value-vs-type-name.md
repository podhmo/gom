# Exported `let foo` vs `type Foo`: type names claim first

- 状態: draft（prototype verified — [sketch: V-6 residual verification](../sketch/2026-11-05-v6-residual-verification.md)）
- 日付: 2026-11-05

## Context

Package-scope Go identifiers come from two different spellings in
one Gom namespace: an exported `let foo` wants `Foo`, and `type Foo`
is *always* emitted `Foo` (Gom type names are already capitalized).
When both exist — in one file, or split across siblings under the
package compilation unit — both claimed `Foo` and the generated
package did not compile (a latent single-file collision too: Gom's
value/type namespaces are separate, so `let foo` + `type Foo` is a
legal program).

## Decision

### 1. Types keep their exact name; values dedup

`codegen.indexDecls` claims names in a fixed order: **type names
first** (the file's own `TypeDecl`s plus `Unit.Types` — the sibling
type names, including ones that shadow builtins package-wide), then
**every package-scope value symbol** — own `FuncDecl`/`VarDecl`
symbols plus `Unit.Decls` — in one canonical order sorted by
(emitted base, Gom name). `let foo` + `type Foo` therefore emits
`var Foo1 Foo = Foo{...}`: the type keeps the spelling every
signature, annotation and `.gomi` member already writes.

### 2. The claiming order is total and unit-wide

Gom names are package-unique (E0202), so sorting by (emitted base,
Gom name) is a total order every member file computes identically —
`sibling x` and the file declaring `x` spell it the same, and two
distinct Gom names mapping to one emitted base (`my_fn` vs `myFn`)
resolve deterministically instead of by input-file order.

### 3. `.gomi` members must emit unambiguously (E0607)

Dedup renames the loser — but the emitted name is also a contract
the `.gomi` publishes. An importing package only sees the interface:
`lib.foo` resolves to the member symbol and codegen renders it
`lib.Foo` — the *base* name — with no access to the declaring
package's dedup result. When `let foo` loses to `type Foo`, the
implementation emits `var Foo1` while importers spell `lib.Foo`,
which names the type (or, worse, another value that claimed `Foo`
first — silently the wrong binding). The collision must therefore be
a checked error at the declaring package, not a rename:

`iface.Check` replays codegen's claiming over the merged package
surface (`core.ExportName`/`core.ValueName` — the shared contract
both sides spell from) and reports **E0607** for each interface
member whose emitted base is already claimed — by an impl type,
or by an earlier value in canonical order, private decls included
(e.g. a private `let _foo` claims `Foo` before exported `let foo`).
Two impl types sharing an emitted base are flagged the same way.

The *importer* gets a second net: the `.gomi` loader marks each
member whose `core.ExportName` collides with another member's name
(`Symbol.EmitClash`), and `resolve` reports E0607 at the reference
— `lib.foo` fails to resolve rather than silently emitting
`lib.Foo`. The mark only sees member-vs-member clashes (private
decls are invisible through `.gomi`), so the declaring package's
conformance check remains the primary net.

The local dedup (section 1) is not dead: it still resolves
collisions among *private* decls, and remains the mechanism for
direct `build.Compile` callers that supply an export set without a
`.gomi` — but a `.gomi` member that would dedup is a checked error,
so a published package's emitted names always match its interface.

## Consequences

- `let foo = ...` + `type Foo` in one file emits `var Foo1` +
  `type Foo` — a legal, diff-minimal package while it stays out of
  the `.gomi` (the type's name, which appears in every reference
  site, is preserved; only the value's var name moves).
- The same collision split across siblings emits identically —
  `Unit.Types`/`Unit.Decls` supply the sibling side, so no member
  file needs to know which file declared what.
- A `.gomi` member in such a collision is E0607 at `gom check` /
  `gom build` of the declaring package, and — when the clashing
  pair is visible in the member table — E0607 again at each
  `pkg.member` reference in an importer. The failure moves from a
  cryptic `go build` error (or a silent wrong binding) in the
  *consumer* to a named diagnostic at the source and at the use.
- `.gomi` members that do not collide emit exactly their base name
  and consumers' `pkg.member` spellings agree with the package's own
  emission, as before.

## Alternatives

- *Rename the type instead (`FooT`, `FooType`).* Rejected: the type
  name is load-bearing — it is spelled in every field type, cast,
  and `.gomi` member; moving it rewrites far more generated surface
  than renaming the one value.
- *Propagate claimed names to importers* (record each member's
  emitted name in the `.gomi` or have the loader consult it).
  Rejected for now: it adds a naming channel to the interface format
  for a collision the language can simply reject — E0607 is the
  smaller contract. If a future need for stable odd spellings
  appears, propagation can supersede the diagnostic.
- *Reject `let foo` + `type Foo` outright.* Rejected for
  non-interface code: Gom namespaces are legitimately separate and
  the collision is a Go emission artifact — the local dedup keeps
  private collisions compiling. Rejection applies only where the
  emitted name is a published contract: the `.gomi` member set
  (E0607).
