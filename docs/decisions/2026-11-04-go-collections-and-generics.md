# []T → List boundary copies, Go.Slice/Go.Map, and callable generic Go functions

- 状態: 採用（P3-3 が実装・検証した — [検証](../sketch/2026-11-04-p3-3-go-collections-verification.md)）
- 日付: 2026-11-04
- Supersedes: [2026-11-03-go-types-mapping.md](2026-11-03-go-types-mapping.md) のうち「`[]T` は常に `Go.Slice`」の部分

P3-3 lands spec 8.3/8.5's collection story: `[]T` at a signature
boundary is `List T` with copies in both directions, `Go.Slice T`
stays the explicit no-copy escape, `map[K]V` is the opaque `Go.Map
K V`, and generic Go functions whose constraints are `any` or
`comparable` are callable.

## Two boundary kinds, one slice

The Gom view and the raw Go view now differ at boundary positions:

- `[]T` spelled by the *Go signature* maps to `List T` in the Gom
  view and `Go.Slice T` in the raw view (`lower.GoSig`). Lowering
  reads the view pair and emits the copy — `.ToSlice()` for a `List`
  argument, `list.FromSlice` for a `[]T` result — as explicit Core
  `Conv` nodes (`ConvSliceToGo`/`ConvSliceFromGo`). Nested `[]T`
  (inside tuples, `map[string][]T`, …) keeps the `Go.Slice` spelling.
- `Go.Slice T` spelled by *Gom source* (a user annotation or an
  `extern` declaration) maps to `Go.Slice T` in both views — the
  views agree, no `Conv` is emitted, and the value aliases the
  Go-owned backing array. The earlier "always `Go.Slice`" mapping
  made ordinary signatures opt out of the immutability guarantee;
  the copy is now the default and `Go.Slice` the deliberate escape.
- Nil behaviour falls out of the runtime: `List` is `list.List[A]`
  (a named slice), `slices.Clone(nil)` is nil, so nil crosses as nil
  in both directions.
- A `List T` or `Go.Slice T` spread into `...any` needs
  `rt.BoxSlice` (`ConvSliceBox`) — Go cannot splice `[]T` into
  `...any`; the checker's 8.1.7 any-boxing rule admits the slice
  argument and lowering emits the element-wise box.
- `map[K]V` is `Go.Map K V` in both views (opaque — no copy, no
  element access; Go functions pass it through, `go/types`
  assignability governs).

## Generic Go calls

A generic Go signature keeps its *declared* type-parameter order on
`check.Signature.TypeParams` (parallel `GoConstraints`), and
`lower.GoSig.TypeParams` records the same order for the raw view.
`instantiateSig` replaces those rigids with metas at each use site —
unlike the first-occurrence order local signatures use — so
`CallInfo.TypeArgs` lines up with the emitted `pkg.F[T1, T2]`, and
`instantiateGoSig` substitutes the solved arguments into the raw
parameter/result types before conversions are picked. A type
argument that never appears in the parameters (e.g. only in the
result) instantiates through the expected-type path like any other
inference.

## Constraint scope: `any` and `comparable` only (v1)

`callableConstraint` classifies each type parameter's constraint
into `any` (`Empty`) or `comparable` (`IsComparable`); anything
else — method sets, unions, `~` approximations — hides the symbol
and references report E0506 with the constraint's rendering. This
matches spec 8.5's bullet literally but means structural
`S ~[]E` / `M ~map[K]V` constraints — `slices.Contains`,
`slices.Index`, `maps.Equal`, most of `slices`/`maps` — stay
unexposed, including the `slices.Contains` example spec 8.1.6 shows
(recorded in the implementation-gaps ledger and
[problems/2026-11-04-generic-go-fns-hidden.md](../problems/2026-11-04-generic-go-fns-hidden.md)).
Widening to structural constraints is a spec amendment: it needs a
mapping for the constraint's core type (`~[]E` ↔ `List`/`Go.Slice`
arguments, `~map[K]V` ↔ `Go.Map`) plus per-argument validation that
cross-references sibling type parameters — deliberate follow-up
work, not an oversight.

`comparable` is validated *after* inference because the argument is
solved, not written: `validateGoGenerics` runs
`goReprComparable` over each instantiated `TypeArgs` and reports
E0508 on failure. The judgment reads the *generated* representation
— `list.List` is a slice (not comparable), records and tuples are
comparable iff every field is, sealed-interface sums and `Option`
are interfaces, `Option`/`Ref`/channels are pointer-like, Gom type
variables instantiate as `any` (not comparable), `*types.Go` handles
defer to `types.Comparable` through `ComparableSource`. Unknown
shapes answer false: a rejected call is a diagnostic, while a wrong
acceptance is a file `go build` rejects.

## Follow-up: constraint identity and container view splits

Review found two classes where the check passed but the generated Go
could not compile; both now fail earlier with a diagnostic:

- **Constraint classification is identity, not `IsComparable`.**
  `types.Interface.IsComparable()` answers whether every element of
  the constraint's type set is comparable, so `~int`, `int|string`
  and `interface{comparable; ~int}` all answered it — while their
  type sets are proper subsets `pkg.F[int64]` would violate.
  `IsMethodSet` cannot discriminate either: `comparable` itself is
  not a method-set interface (the marker is a non-method element).
  The callable test is therefore `types.Identical(iface, universeComparable)`
  on the underlying interface — it also admits the equivalent
  spelling `interface{comparable}`, which shares the type set.
- **Container-split views hide the member.** `int` is the only type
  whose two views diverge (`Int`/`Go.Int`), and conversions Core IR
  can express apply to whole values only — `[]int`
  (`List Int`/`Go.Slice Go.Int`), `map[string]int`, `[][]int`,
  `chan int`, `Ptr int` and variadic `...int` need an element-wise
  conversion that does not exist. `viewsCompatible` admits the
  whole-type numeric pair and the `List`↔`Go.Slice` copy with
  view-equal elements; everything else stays hidden under E0501 with
  an `element-wise` reason (the byte/uint8 treatment), applied to
  parameters, results, vars/consts and fields alike. The obvious
  alternative — an `rt.MapSlice`-style element conversion — would
  double the copy already present at slice boundaries and is the
  sort of silent cost spec 8.5's `Go.Slice` escape exists to avoid;
  it stays a possible follow-up, not part of v1.

## Consequences

- `os.Args`, `strings.Join`'s `[]string`, `os.Environ()` all type as
  `List String` — `strings.Join(os.Args, " ")` type-checks with the
  copy emitted on both sides (golden `go_slice_boundary`).
- `Go.Slice` annotations in Gom signatures keep identity — passing a
  `Go.Slice` through a `List`-typed boundary still copies, which is
  required for the immutability guarantee (a Go-owned value entering
  `List` must not alias Go-side writes).
- `Signature.TypeParams` is empty for Gom signatures, so local
  generic functions (P2-5) keep first-occurrence instantiation and
  no Go-constraint validation.
- Hidden-symbol reasons now carry a code: `resolve.Symbol.HiddenCode`
  selects E0506 for constraint failures, defaulting to E0501 — the
  first hidden reason that is a spec'd limitation rather than an
  unrepresentable type.
