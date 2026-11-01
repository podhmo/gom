# P0-1: representation of recursive ADTs in generated Go code

- 目的: decide whether generated Go code represents a Gom sum type as
  (A) a sealed interface + one struct per variant, or (B) a single
  tagged struct holding every variant's payload.
- 関連: TODO `P0-1`; [decisions/value-representation](../decisions/2026-11-01-value-representation.md);
  [design.md 4.1](../design.md); spec 4.1 / 8.6 (invalid values panic at match).
- 実験コード: `experiments/p0/adt-repr/`（module
  `github.com/podhmo/gom/experiments/p0/adt-repr`, Go 1.27.1,
  linux/amd64, AMD Ryzen 7 255）

## Candidates

**A — sealed interface + generics** (`sealed/`), the design.md 4.1
scheme. One interface with an unexported marker method, one exported
struct per variant; recursive positions hold the interface.

```go
type Expr interface{ isExpr() }
type Expr_Lit struct{ V0 int64 }
type Expr_Add struct{ V0, V1 Expr }
type Expr_Neg struct{ V0 Expr }
```

**B — tagged single struct** (`tagged/`). One struct per ADT carrying a
tag plus the payload fields of every variant. All fields are
unexported (otherwise a consumer could corrupt the tag or mix
payloads), so the compiler must also emit a constructor, a `Tag()`
accessor, and `As*` destructuring accessors per variant. Recursive
payloads must sit behind a pointer because a Go struct cannot contain
itself.

```go
type Expr struct {
	tag ExprTag
	lit int64    // Lit inline
	add *exprAdd // Add payload; pointer because it contains Expr
	neg *Expr    // Neg payload; pointer because it is an Expr
}
```

Both use `rt.InvalidVariant(typeName, v)` in the `default` arm instead
of `panic("unreachable")`.

## Benchmarks

`go test -bench=. -benchmem -benchtime=1s` (identical benchmark names in
each package; `Expr_Lit` is a leaf variant, `Expr_Add`/`Expr_Neg`
recursive, `Option_Some`/`List_Cons` generic).

| benchmark | A: sealed ns/op | B: tagged ns/op | A allocs | B allocs |
| --- | --- | --- | --- | --- |
| ConstructLit (escapes to global) | 8.8 | 0.42 | ~0.5†, 8 B | 0 |
| ConstructLitNoEscape (local eval) | 9.7 | 1.7 | ~0.5†, 8 B | 0 |
| ConstructLitSlice (store into []Expr) | 8.5 | 0.21 | ~0.5†, 8 B | 0 |
| ConstructAdd (two Lit children) | 29.7 | 25.5 | 1, 40 B | 1, 64 B |
| ConstructSome | 8.9 | 0.42 | ~0.5†, 8 B | 0 |
| ConstructNone | 0.42 | 0.42 | 0 | 0 |
| ConstructCons | 19.3 | 15.2 | 1, 24 B | 1, 24 B |
| ConstructSomeBig (`[128]int64`) | 345 | 54.6 | 1, 1024 B | 0 |
| BuildExprTree (depth 8) | 10015 | 11527 | 510, 12 240 B | 510, 24 480 B |
| BuildList (1000) | 18895 | 15270 | 1000, 24 000 B | 1000, 24 000 B |
| Eval (tree depth 10) | 4097 | 4785 | 0 | 0 |
| Area | 2.53 | 2.53 | 0 | 0 |
| GetOrElse | 1.38 | 1.28 | 0 | 0 |
| Sum (list 1000) | 934 | 1038 | 0 | 0 |
| PassOption | 0.42 | 0.43 | 0 | 0 |
| PassBigOption (`Option[[128]int64]`) | 0.42 | 13.7 | 0 | 0 |

† 8-byte pointer-free boxes amortize in the tiny size class, so the
rounded `allocs/op` displays as 0 while `B/op` shows the real garbage.

Notes on the numbers:

- Go folds nested composite literals into one allocation: the two
  `Expr_Lit` children inside `Expr_Add{...}` did not cost separate
  boxes (ConstructAdd: 1 alloc, not 3).
- BuildExprTree allocates the same count in both styles (the internal
  nodes), but A allocates half the bytes: in B every child is a full
  32-byte `Expr` copied into the parent's payload, in A a child is a
  16-byte interface pair pointing at a shared box. The ns/op ordering
  is within run-to-run noise — repeated runs disagreed on which side
  is faster — while the allocs/bytes side is robust.
- Eval/Sum are ~10–15% faster in A: each recursion copies a 16-byte
  interface instead of a 32-byte struct, and `AsAdd` returns two more
  32-byte copies.
- B wins only where payloads are huge and never boxed (SomeBig
  construction) and loses it all back on every pass-by-value
  (PassBigOption: 0.42 ns vs 13.7 ns).

## Findings per evaluation axis

### 1. nil handling

- A: `var e Expr` is a nil interface — the *only* invalid value a Go
  caller can produce, and the `default` arm catches it:
  `gom: invalid sealed.Expr value <nil>: not a declared variant`.
  A typed-nil pointer `(*Expr_Lit)(nil)` also lands in `default`
  because cases match the value structs, not pointers. A legally
  shaped `Expr_Add{V0: nil}` is constructible, but the nil surfaces
  at the child's `match` — still a clean `InvalidVariant` panic.
  Variants must stay **value structs**: had `*Expr_Lit` been the
  variant type, a typed nil would match `case *Expr_Lit:` and
  nil-deref inside the arm.
- B: there is no nil `Expr`; the closest invalids are the zero struct
  (tag `Invalid`, caught by `default`) and a corrupted tag (caught,
  but only reachable from inside the package since fields are
  unexported). Dereferencing `*Expr` nils panics Go-style before the
  match runs. Misusing `AsAdd` on a `Lit` produces a raw
  `runtime error: nil pointer dereference`, not `InvalidVariant`.
- Verdict: equivalent for generated code (single `default: panic` arm
  in both), slightly better failure surface in A.

### 2. Allocations

See table. B avoids boxing leaf/scalar variants entirely (~20–40×
cheaper, zero garbage — including stores into slices of values,
8.5 ns + 8 B vs 0.21 ns + 0 B); A pays one small box per escaping
variant (tiny-alloc amortized, ~8 ns / 8 B) — but recursive payloads
force one allocation in B anyway, so ADT-heavy workloads end up equal
in alloc count and better in bytes/time for A.

### 3. Recursive types

- A handles recursion naturally: `Expr_Add{V0, V1 Expr}` — children
  are already interfaces, shared freely.
- B cannot embed itself; every recursive position forces a pointer
  indirection (`add *exprAdd`, `neg *Expr`) and a decision the
  compiler must make per variant (which fields are inline vs.
  indirected). Works, but the representation is no longer "one flat
  struct" — it is a tagged header plus per-variant heap payloads,
  which is most of A's cost structure with extra steps.

### 4. Generic ADTs

- A: `type Option[A any] interface{ isOption() }` + `Option_Some[A]`.
  **Gotcha found**: a type parameter that appears only in the
  instantiated interface's name is *phantom* — inference cannot bind
  it through `interface{ isOption() }`, because it unifies through
  the type's structure and `A` is not part of the method signature.
  The failure shape depends on the call:

  1. `IsSome(o)` / `IsSome(Option_Some[int64]{...})` — nothing else
     binds `A` → compile error `cannot infer A`. Loud and fine.
  2. `GetOrElse(o, 9)` with `o : Option[int64]` — **compiles**, with
     `A` silently inferred as `int` from the untyped constant. It
     type-checks only because `Option[int64]` is assignable to
     `Option[int]` when the argument is phantom (same method set).
     At run time `case Option_Some[int]` never matches the
     `Option_Some[int64]` inside `o`, so control falls to the
     `default` arm: an `InvalidVariant` panic where the default
     panics, a silently wrong result where it does not. This is the
     worst failure mode — silent misinference, verified on
     go1.27.1.
  3. `acc += GetOrElse(o, 9)` with `acc : int64` — compile error
     `mismatched types int64 and int`: the only shape where the
     misinference surfaces at compile time.

  Fix (verified): mention the parameters in the marker signature,
  `interface{ isOption() A }` (multi-param:
  `interface{ isResult() (T, E) }`). Inference then binds `A` from
  the method signature and works normally; the method is never
  called. This costs one line per variant and should be folded into
  the design. It also makes instantiations genuinely different
  types — `Option_Some[int64]` no longer satisfies `Option[int]` —
  so any residual misinference becomes a compile error instead of
  the runtime fall-through of shape 2. (P0-3 independently uses the
  equivalent `interface{ isList(List[A]) }`; `isOption() A` is
  terser. Reconcile in P0-6.)
- B: `type Option[A any] struct{ tag; some A }` — inference works
  out of the box, no trick needed.
- Both: `List[A]` needs one allocation per Cons cell either way
  (B keeps `tail` inline in the cell; A stores a 16 B interface).

### 5. Go-side ergonomics (`example/`)

- A constructs with plain composite literals
  (`sealed.Expr_Add{V0: ..., V1: ...}`) and destructures with a type
  switch — the standard Go idiom for closed type sets. Sealed by the
  unexported marker.
- B constructs via `tagged.Expr_Add(l, r)` and destructures via
  `switch e.Tag()` + `e.AsAdd()` — workable but foreign to Go
  readers; the tag constants and accessors are boilerplate a human
  would not write.
- `==`: A gets **structural equality for free** — interface equality
  deep-compares the boxed structs, so two separately built
  `Add(Lit 1, Lit 2)` are equal (spec 3.3 wants structural `==` on
  function-free types). B's `==` compares payload *pointers* by
  identity: equal trees compare **un**equal. B would need a generated
  `Equal` method on every ADT — more codegen, more runtime cost, and
  `==` would still silently mislead a Go consumer.
- `fmt`: `%#v`/`%v` on A prints variant fields readably; B prints a
  tag number and raw pointers (unexported fields hide payloads from
  external fmt anyway).

### 6. Code size / what the compiler must generate

Non-test source lines for the three ADT groups:

| ADT | A: sealed | B: tagged |
| --- | --- | --- |
| Shape (3 variants) | 40 | 68 |
| Expr (3 variants, recursive) | 47 | 72 |
| Option + List (generics) | 85 | 100 |

B needs ~40% more surface (240 vs 172 lines): tag type + constants
per ADT, a constructor and a `Tag()`/`As*` accessor per variant,
payload sub-structs for recursive variants — all extra API surface
the compiler must emit, name, and keep consistent.

## Recommendation

**Adopt A: sealed interface + variant structs**, i.e. design.md 4.1
as written, with two refinements:

1. Generic ADTs declare the marker as `isOption() A` (parameters in
   the signature) so call-site type inference works.
2. Variants are always *value* structs in the interface, never
   pointer types, so a typed-nil pointer from Go lands in the
   `default` arm and panics with `rt.InvalidVariant`.

Reasons: idiomatic Go on both the construct and the destruct side;
structural `==` for free (a hard spec requirement that B cannot
meet without generated `Equal` methods); ~40% less generated code;
a single, narrow invalid-value surface (nil) with uniform
`InvalidVariant` panics; and equal-or-better performance on the
dispatch-heavy workloads measured (traversal, recursion with 16 B
handles vs 32 B copies, big-payload passing) — while B keeps a real
construction-cost edge wherever leaf variants escape (into globals,
slices, or interfaces), so the verdict tilts toward B only if
profiling later shows variant construction dominating.

Keep B (tagged single struct) in mind only if a future profiling
task proves leaf-variant boxing is a hotspot, or for the `Result`
question in P0-4 where a fixed `struct{Value, Error, Ok}` layout is
the specific alternative — that experiment should compare against
this A as the baseline.

## Proposed update to decisions/2026-11-01-value-representation.md

- Move the "ADT" row from 「P0 で決めること」 to 「決まったこと」 with:
  ADT は sealed interface＋バリアント struct（design.md 4.1）。ジェネリックな
  ADT はマーカーメソッドのシグネチャに型パラメータを含める
  （`isOption() A`、`isResult() (T, E)`）—呼び出し側の型推論を効かせるため。
  バリアントはポインタ型ではなく値 struct とする（Go 側の typed nil が
  `default` で `rt.InvalidVariant` に落ちるように）。タグ付き単一 struct は
  構造的 `==` を素朴には満たせず、生成コード量も多いため不採用。
  根拠: docs/sketch/2026-11-02-p0-1-adt-representation.md。
