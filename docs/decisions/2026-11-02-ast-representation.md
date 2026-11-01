# Placement and shape of the pre-Core intermediate representations

- 状態: draft
- 日付: 2026-11-02

## Background

P1-2 defines the data model for the four representations before Core
IR: the surface AST, the resolved AST, the type representation, and
the typed AST (TODO.md P1-2; the Core IR itself is a follow-up task).
Two questions needed a decision: where each representation lives, and
whether "resolved AST" and "typed AST" are separate trees or
annotations on one tree.

## Placement

- **Surface AST → `internal/syntax`.** The package already owns lexing
  and parsing; its AST is produced there and trivia support
  (File.Comments, File.Blanks) lives next to it.
- **Resolved AST → `internal/resolve`.** The resolver's output is a
  `resolve.Resolved`: the shared `*syntax.File` plus `Defs`/`Uses`
  tables keyed on `*syntax.Ident`, and the `Symbol` type.
- **Types → `internal/types`.** A shared building block usable from
  every phase; nothing in it depends on syntax, so it stays a leaf.
- **Typed AST → `internal/check`.** The checker's output is a real
  tree (`check.File` of `check.Decl`/`check.Expr`/`check.Pat`); later
  packages (pattern, lower, core) import check for it — consistent
  with the one-way dependency `syntax → resolve → check → pattern →
  lower → codegen`. No new package is added to the design.md section 3
  layout.

## Resolved AST: shared tree + side tables (not a second tree)

`resolve.Resolved{File, Defs, Uses}` keeps the surface tree and binds
every `*syntax.Ident` — definitions in `Defs`, references in `Uses` —
in the style of `go/types.Info`. Alternatives considered:

- A second, parallel node hierarchy where `Var` carries `*Symbol`
  directly: statically stronger, but doubles the AST and every
  traversal, and the typed AST is already a second tree for
  unavoidable reasons (its nodes carry types and structured call
  info).
- In-place annotation (a `Sym` field on `syntax.Ident`): would make
  `syntax` depend on `resolve` (or push `Symbol` into a package that
  does not own it) and mutates the tree `gom fmt` shares.

`*syntax.Ident` is a name token used in both binding and reference
positions — go/ast does the same — so one key space serves both maps.

## Typed AST: a separate tree

`check.Expr`/`check.Pat`/`check.Decl` are new sealed families. Reasons:

- Every node physically carries `types.Type` and `diag.Span` (a shared
  exported `Base` field), which a side table cannot enforce — the
  invariant "every node carries its type" is structural, not
  conventional.
- `check.Apply` carries `CallInfo` as a field, satisfying "the
  structure is the contract" (AGENTS.md, design.md 2.1): `Kind`
  (unknown / direct / partial / Go convention), `Callee`, `TypeArgs`,
  `Arity`, `Applied`, `Unapplied`, `ArgToParam` (label reordering),
  `HoleIndex` (cute `_`), `Variadic`. `CallInfo.Validate` checks the
  invariants — including arity/ArgToParam bounds — and
  `Apply.Validate` checks the info against the argument list, so
  hand-built typed ASTs in later-phase tests stay honest.
- Sugared forms (pipes, `|?>`, `try`, `select`, test blocks) are still
  present — desugaring is phase 6 — but with resolved symbols and
  types attached. `Args` of a typed `Apply` are the semantic call
  arguments in source (evaluation) order; for a `CallGo` node the
  surface argument tuple is unpacked into the Go argument list.
- The sugared operators (`|>`, `<|`, `|?>`, `>>`, `<<`) stay `Binary`
  nodes, not `Apply`: they carry no `CallInfo`, and their call
  convention is fixed only when lowering expands them.
- Member access `recv.sel` is `MemberExpr` — renamed from `Select` so
  it cannot be confused with the channel `SelectExpr`. `Ascribe.Conv`
  marks `Go.conv` sites so lowering can emit the explicit conversion.
- `TypeDecl.TypeParams` records the declared type-parameter order —
  the order `Con.Args` for the declared type are given in. `TField`
  carries `Sp` and `Sym` so field patterns and selections reach the
  declared field symbol and position.
- Type expressions collapse into `types.Type`; imports become
  `check.Import`, extern blocks are consumed by resolution and do not
  appear.

## Type representation (internal/types)

- `Con{Qual, Name, Args}` is the single nominal form for builtins,
  builtin constructors (List, Option, Result, Chan, Go.Slice, ...),
  and user-declared nominal types. Flattened application (not
  curried `App`) keeps "is this `List`?" a two-field check.
- **`Con.Qual` is the declaring package's import path** — the
  canonical package identity, not the local binding name, so
  `import "x/util" as a` and `import "x/util" as b` produce identical
  Cons. Resolution fills it from `Symbol.Package`. Two reserved
  qualifiers are not import paths: `""` is the builtin scope plus
  the package being compiled, and `"Go"` is the compiler-owned
  interop namespace (`Go.Slice`, `Go.Map`).
- `Option`, `Result`, `List` and friends are builtin-scope types:
  spec 3.1 lists them under builtin types, they are in scope with no
  import, and builtin-scope symbols carry `Package == ""`. Their
  `Con` therefore has `Qual == ""` — exactly what `types.Option` et
  al. construct — even though their *implementations* will live in
  stdlib `.gom` sources. (A stdlib path qualifier was the
  alternative; rejected because the stdlib layout is undecided and
  the spec calls these builtin types.)
- `Var{ID, Name}` is a rigid type variable (identity = ID);
  `Meta{ID}` is the mutable unification variable for P2, chased by
  `Equal`/`Subst`/`String` once bound. `Meta.Bind` panics on a
  second bind — double binding is a caller bug, not last-wins.
- `Func{Params []FuncParam, Ret}` keeps the parameter list flat with
  labels, matching arity and label handling at call sites. `Equal`
  implements spec 2.1: consecutive labeled parameter runs compare up
  to reordering; runs separated by positional parameters do not
  merge.
- **Canonical form: `Func.Ret` is never a `*Func`.** `Int -> Int ->
  Int` and `Int -> (Int -> Int)` are the same curried type — the
  parenthesization is semantically insignificant — so only the flat
  spelling is stored. `Fn()` flattens at construction and `Subst`
  routes through `Fn`; hand-built `Func` literals must keep the
  invariant. A bound `Meta` in `Ret` position can still hide a
  function type (the meta node is inference's to keep — `Fn` does
  not chase it), so `Equal` flattens after chasing before
  comparing: both spellings are always `Equal`, and `String` prints
  them identically either way.
- `Go{Ref NamedRef, Args}` is the Go-interop handle.
  **`NamedRef{Path, Name}` is the whole nominal identity** — the
  earlier sealed `GoRef` interface is gone, because `isGoRef` being
  unexported made the promised goimport-side implementation
  impossible. Whatever a path+name cannot express (method sets,
  underlying types, assignability) lives in goimport-side tables
  keyed on `Path`+`Name`.
- Types are immutable after construction; exported slices are shared
  and never mutated. `Meta.Bind` is the single mutation point.
- `Subst` maps `Var.ID → Type`; `FreeVars` collects rigid vars.

## Surface-AST conventions

- Every node family is a sealed interface (`isExpr`, `isPat`, ...);
  concrete nodes carry `Sp diag.Span` plus a `Span()` method.
- Trivia is file-level: `File.Comments` (all comments, source order)
  and `File.Blanks` (one span per blank line). `gom fmt` reattaches
  comments by position like go/printer does; `///` docs additionally
  link onto declarations via the `Doc` field for .gomi generation.
- No `Paren` node: parentheses are not semantically significant and
  `gom fmt` re-derives them. `f(a, b)` is `Apply` of `f` to a
  `TupleLit`; `f (a)` applies `f` to `a`; `()` is the `Lit{LitUnit}`
  literal.
- `_` is a `WildPat` in patterns and a `Hole` expression only as a
  Go-call argument (cute placeholder). `..e` is a `Spread` expression
  inside list literals and Go-call argument tuples.
- Punning keeps a flag, not a desugared shape: `Arg.Punned` preserves
  `~to` for the formatter while `Arg.Value` already holds the
  identifier, so `Uses` binds it uniformly; `FieldPat.Pat == nil`
  marks `{ name }`.

## Consequences / open points for follow-up work

- P1-2b (Core IR + validator) consumes `check.File`; `CallInfo` is the
  call contract — `lower` must not inspect `Fn`'s shape to decide the
  calling convention.
- `resolve.Symbol` is the currency for names across resolve, check,
  pattern, lower, core and codegen; its `Kind` enumeration may grow
  (e.g. `extern` conversion rules in P3). A `*resolve.Symbol` does
  not yet carry its declared type scheme — P1-5 will need a
  symbol → scheme table (checker environment or a `check.File` side
  table) to instantiate `CallInfo.TypeArgs`. Open point.
- `.gomi` files parse to the same `syntax.File`; signature-only
  declarations use the same nodes with nil bodies.
- Validation: `gofmt`, `go vet`, `go test` pass on Go 1.27.1 with unit
  tests constructing every node family, span/type propagation, sealed
  type switches, `Subst`/`Equal`/`String`, and `CallInfo` scenarios
  (saturated, reordered labeled, partial, Go variadic, cute hole,
  unknown-function application). The decision stays `draft` until
  P1-3..P1-5 exercise the model end to end.
