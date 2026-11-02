# Comparator minting is bounded and file-unique; ctor lets keep the declared interface

- 状態: draft
- 日付: 2026-11-05
- Supersedes: [generated-equality-comparators](2026-11-05-generated-equality-comparators.md) のうち「比較器は必要なだけ mint する」という暗黙の前提 — 上限と超過時の扱いを追加する（比較器を型ごとに生成する枠組み自体は変えない）

## Context

The generated-comparator design minted a `gomEq<N>` per distinct
operand type, sequenced by a per-file counter. Three defects:

- **Package-scope collisions.** `gom build dir/` compiles each
  `.gom` file independently but emits them into one Go package.
  Two files each mint `gomEq1` — a redeclaration, or worse, a call
  resolving to the other file's comparator of a different type.
- **Divergent minting.** `type W a = {next: List (W (W a))}` — a
  self-application whose expansion grows at every level — mints a
  fresh instantiated type per comparator step and never converges.
  `gom check` accepts it; `gom build` hung.
- **Uncompilable native `==`.** `let a = A; let b = B 1; a == b`
  and `(Some 1) == None`: `:=` re-infers the concrete variant
  struct, so all-native sums rendered `Sh_A == Sh_B` — comparing
  two different concrete types, which Go rejects. Match-scrutinee
  temporaries were the same shape (the `match (Some 1)` type-switch
  bug tracked in TODO.md).

## Decision

### Comparator names carry a file tag (internal/lower)

`cmpDecl` names comparators `gomEq<N>_<filetag>` where the tag is
`fnv32a` of `check.File.Sp.File`, baked into the synthesized symbol
at mint time — declaration sites and reference sites share the
symbol, so codegen needs no second lookup. Deterministic per file
(same file, same names), unique per package member (different
files, different tags). A readable marker, not a random nonce:
`gomEq1_e9feb593` (codegen's name convention renders it
`gomEq1E9feb593`).

### Minting has a shared budget; overflow is a diagnostic

`eqDeclLimit` (128) bounds the comparators one file mints. Because
`cmpDecl` is the only minting path — recursive plans included — the
budget is a file-wide session limit, not a per-walk heuristic like
the old `eqWalkLimit` classification bound.

On overflow the recipe *degrades* to `EqRuntime` — the plan stays
well-formed — but `eqPlan` also reports **E0906** at the `==`
site, once per file. The diagnostic, not the recipe, stops the
build.

A silent `rt.Equal`/`reflect.DeepEqual` fallback was considered
and rejected: it is exactly the reading this redesign exists to
escape — DeepEqual's same-backing-store shortcut answers
`[NaN] == [NaN]` true where elementwise comparison says false.
Degrading quietly would reintroduce the NaN inconsistency on
precisely the exotic types that hit the limit.

### Narrowed ctor lets keep the declared interface (internal/codegen)

A `name := rhs` let whose RHS is a sealed-ADT constructor
application emits `name := ADT(Variant{..})` — the binder's
declared type as an explicit conversion — alongside the existing
untyped-constant `intLitConv` case, unified as `letConv`. `:=`
re-infers otherwise; the conversion keeps operand types at the
ADT interface so `a == b`, `!=`, and `switch v := x.(type)` are
all the interface-shaped Go the semantics needs. Builtin `Result`
is exempt: its constructors emit calls (`result.Ok(...)`) already
typed at the declared type.

This is the same mechanism `task/fix-diff-bugs` uses for its
constructor-let fix, applied here because the narrowed-`==`
defect needed it; it also covers the `match (Some 1)` scrutinee
temporary tracked as a separate TODO bug. The *check*-side half
of fix-diff-bugs (do not stamp `Go.Any` on branching nodes) is a
different bug and stays there.

## Consequences

- `gom build` on `W`-style self-applied equality terminates with
  one E0906 instead of hanging (`timeout 20` regression: the spec
  fixture at `testdata/spec/E0906_eq_budget.*`).
- Generated comparator names are no longer bare `gomEq<N>` —
  goldens carry the file tag.
- E0906 registered under a new "09" lowering category; E0901
  remains the unregistered contract-gap code.
