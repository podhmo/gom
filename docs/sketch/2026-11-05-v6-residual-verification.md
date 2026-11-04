# Sketch: V-6 residual items — verification log

Verification report for the residual round on the package
compilation unit — the boundaries the V-6 vertical slice left
recorded as deferred ([package-compilation-unit
decision](../decisions/2026-11-05-package-compilation-unit.md),
"Deferred boundaries"). Covers:

- `pkg.Type.Ctor` for imported types
  ([decision](../decisions/2026-11-05-pkg-type-ctor-spelling.md))
- `.gom`-level implementation import cycles
  ([decision](../decisions/2026-11-05-gom-implementation-import-cycles.md))
- package-aware `gom dump`
- same-named sibling `test` blocks
  ([decision](../decisions/2026-11-05-test-name-file-tag.md))
- `gom build` output withholding on interface failure
  ([decision](../decisions/2026-11-05-interface-failure-withholds-output.md))
- exported `let foo` vs `type Foo` generated-name collision
  ([decision](../decisions/2026-11-05-exported-value-vs-type-name.md))
- the `.gomi` member-surface audit (let members, type members,
  `SelfImportPath`)

All items were verified through `go test ./...` on the worktree;
the notable per-item evidence follows.

## `pkg.Type.Ctor`

`internal/gomimport/ctor_test.go` builds a temp module
(`example.com/m`) whose `shapes` package exports the `Shape` ADT
through `shapes.gomi`, then compiles a `package main` consumer that
both *constructs* `shapes.Shape.Circle 1.0` and *matches* on
`shapes.Shape.Rect w h` in pattern position. Generated code carries
`shapes.Shape_Circle{Radius: 1.0}` and a `case
shapes.Shape_Rect:` type-switch arm — identical symbols to the flat
`shapes.Circle` spelling. An unknown member
(`shapes.Shape.Nope`) reports E0201 naming the type namespace
(`Shape has no constructor Nope`) rather than the package
namespace; a non-sum type answers "no constructor table" instead of
an empty-map miss.

## Implementation import cycles

`internal/gomimport/cycle_test.go` lays out modules on disk and
asserts the typed `*resolve.ImportCycleError`:

- mutual `a ↔ b` → chain `a -> b -> a`;
- transitive `a -> b -> c -> a`;
- self-import `a -> a`;
- a `.gomi` that names the importing package back is **not** a
  cycle — interface-only references create no code edge;
- `Compile` maps the error to E0606 at the import span with
  `data.cycle` carrying the chain.

Before the check, `gom check`/`gom build` accepted `a.gom ↔ b.gom`
and `go build` reported "import cycle not allowed" on generated
files.

## Package-aware `gom dump`

`cmd/gom` CLI test: `gom dump shapes/ops.gom` compiles the whole
`shapes` directory (both `.gom` files) and dumps only the requested
file's tree — sibling declarations resolve (no E0201 on
`pi`/`Shape`), diagnostics stay on stderr, the dump on stdout.

## Same-named sibling `test` blocks

`internal/build/files_test.go`: two sibling files each declaring
`test "same"` emit `TestSame_<tag_a>` and `TestSame_<tag_b>` — the
fnv32a-of-path tag, deterministic and distinct — while a unique
name stays bare `TestX`. Single-file output is unchanged.

## Interface-failure output withholding

`cmd/gom` CLI tests: a package failing `.gomi` conformance (E0603)
or missing its `.gomi` (E0601) reports the diagnostic and writes
*zero* output files; a clean package in the same invocation still
writes. `-o` misuse stays a usage error (exit 2) ahead of
compilation.

## Exported `let foo` vs `type Foo`

`internal/build/files_test.go` + single-file compile: both the
sibling-split and the single-file collision emit `type Foo …` +
`var Foo1 Foo = Foo{…}` — the type keeps the name every reference
already spells; the value takes the suffix. Deterministic across
input order (canonical sorted claim).

## `.gomi` member-surface audit

- **Generalized `let` members**: `.gomi` `let empty : List a` *did*
  load as a `KindVar` member typed `List a`; a consumer emitted
  `return p.Empty` while the only conforming implementation — a
  generalized `let` — emits `func Empty[T any]() list.List[T]`.
  Conformable but unimplementable → **bounded**: the `.gomi`
  grammar now rejects type variables in `let` signatures with
  E0602 (`internal/syntax/parser.go`, `mentionsTypeVar` —
  `internal/syntax/typeexpr.go`); monomorphic `let` members are
  unchanged. Proper thunk-member support remains open.
- **`.gomi` type members in `internal/stdlib`**: still skipped
  (`MemberType` never enters the `Modules` table) — inert: no
  stdlib `.gomi` declares types. Recorded as a remaining bound.
- **`SelfImportPath`**: confirmed `gomstd.SelfPath` matches any
  `stdlib/<pkg>/` path segment pair; a user module under that
  layout gets toolchain self-path treatment (bare identifiers
  emitted, toolchain import elided). Recorded:
  [problems/2026-11-05-self-import-path-layout-convention](../problems/2026-11-05-self-import-path-layout-convention.md).

## Regression

`gofmt -l .` clean; `go vet ./...` clean; `go test ./...` green
including the generated-doc sync tests
(`internal/diag.TestErrorCodesDocInSync`,
`internal/tool/decisionsdoc.TestDecisionsReadmeInSync`).

## Follow-up (review fixes)

- **Exported-name collision across packages**: the `Foo1` dedup
  alone was not enough — an importer spells `lib.foo` as `lib.Foo`
  regardless of what the declaring package deduplicated. Now a
  two-sided contract: `iface.Check` replays codegen's claiming
  (`core.ExportName`/`core.ValueName`) over the package surface and
  reports **E0607** for interface members whose emitted base is
  already claimed (impl types, earlier canonical-order values
  including private decls, and impl type-vs-type). The `.gomi`
  loader additionally marks the loser of each member-visible clash
  (`Symbol.EmitClash`) so `resolve` reports E0607 at `pkg.member`
  use sites too — covering importers whose declaring package was
  never checked. Verified: `gom check lib` reports exactly E0607
  (`cmd/gom` `TestCheckEmittedNameCollision`); `lib.foo` in an
  importer reports E0607 at the use (`TestImportedMemberEmitClash`,
  `…ValueValue`, `…NoEmitClash`).
- **E0606 scoped to cycle members**: the loader kept one global
  `implCycleErr` and returned it for every load — a clean import
  of an unrelated package reported E0606 with `data.path` pointing
  at the clean import. Now `implCycleSet` holds the distinct chain
  nodes and `Load` returns the error only for paths on the loop.
  Verified: `TestImplImportCleanDepOutsideCycle` (a → b → c → b
  plus clean a → d) reports E0606 only at `import "b"` with
  `data.path=example.com/m/b` and `data.cycle=b->c->b`; d loads.
- **`.gomi` back-edge test strengthened**:
  `TestImplImportNoCycleWhenOnlyIfaceDepends` now has `b.gomi`
  actually `import "example.com/m/a"` (with `a/a.gomi` so the
  nested load succeeds) — and asserts zero diagnostics; the old
  fixture's `b.g` body was also a latent E0401 (`b.g ()` now).
- Cleanups: `selectMemberCtor` uses `r.ctorTable` like the
  expression path; dead `isIdentRune`/`capitalize` removed from
  codegen names (`valueName`/`exportName` delegate to `core`);
  `noSuchCtor` distinguishes Go types — `time.Time.X` reports
  "Go type `Time` has no constructor `X` — Go types cannot declare
  Gom constructors".
