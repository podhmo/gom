# Wave-15 lane 2: structural-constraint generics — survey + decision draft

## Survey (go/types oracle, Go 1.27 stdlib)

Method: `go/types` source importer over `slices`, `maps`, `cmp`, `sort`,
`iter`, `container/*`, `strings`, `bytes`, `sync`, `sync/atomic`, `time`,
`errors`, `fmt`, `unique`, `weak`, `math`, `math/rand/v2`, `encoding/json`;
each exported/unexported `*types.Func` with type params classified by its
constraint interface's embedded terms. Program: /tmp probe (not committed).

| Constraint class | Exported callables behind it |
|---|---|
| `S ~[]E; E comparable` | slices.Contains, Equal, Index, Compact (4) |
| `S ~[]E; E any` (+ extra `T any`) | slices.Clip, Clone, CompactFunc, Concat, ContainsFunc, Delete, DeleteFunc, Grow, IndexFunc, Insert, IsSortedFunc, MaxFunc, MinFunc, Repeat, Replace, Reverse, SortFunc, SortStableFunc, BinarySearchFunc (18) |
| `S ~[]E; E cmp.Ordered` | slices.BinarySearch, Compare, IsSorted, Max, Min, Sort (6) |
| `Slice ~[]E; E any` (iter results) | slices.All, AppendSeq, Backward, Chunk, Values (5) |
| `S1 ~[]E1; S2 ~[]E2` | slices.CompareFunc, EqualFunc (2) |
| `M ~map[K]V; K comparable; V any` | maps.All, Insert, Keys, Values, Clone, DeleteFunc (+ M1/M2 variants: Copy, Equal, EqualFunc) (9) |
| `T cmp.Ordered` | cmp.Compare, cmp.Less (2) |
| `union(~int|~int16|~int32|~int64|~int8|~uint*8)` | math/rand/v2.N (1) |
| `E comparable` alone | already callable (P3-3) |
| `E methods(1)` | errors.AsType — stays hidden |
| `union([]byte\|string)` | time internals — unexported, moot |

Payoff: **66 exported functions** unlock if `~[]E`, `~map[K]V` and
basic-union constraints are admitted — essentially all of `slices`/`maps`/
`cmp`. `maps.Keys/Values` etc. may still hide downstream of the constraint
check (result `iter.Seq[K]` is an instantiated generic type the mapper
cannot spell — unrepresentable result, separate residual).

## Decision draft

Admit three structural classes by instantiating each type parameter to the
**Go representation of the Gom type the call site solved** — the same
channel `any`/`comparable` already use (option (c) of the lane prompt):

- `~[]E` (E = another type param of the same signature): admitted.
  The solved arg's repr must be `[]T` where `T` = repr of the solved `E`
  arg — i.e. the Gom arg is `List E`/`Go.Slice E`. `slices.Contains xs v`
  with `xs : List Int`, `v : Int` emits
  `slices.Contains[[]int64, int64](xs, v)`.
- `~map[K]V`: admitted. Solved arg repr must be `map[reprK]reprV` —
  `Go.Map` args.
- Basic-term unions (`cmp.Ordered`, `~int`-unions, `[]byte|string`):
  admitted when every term is a builtin basic type (tilde or not).
  The solved arg's repr must be a member: tilde `~T` admits reprs whose
  underlying is `T` — for Gom's emitted builtins that is repr identity
  (`Int`→int64 satisfies `~int64`, **not** `~int`); non-tilde terms
  admit identical reprs. `[]byte` admits `List Byte`/`Go.Slice Byte`.

Still hidden (E0506, with their own reason strings): method-set
constraints (`errors.AsType`), unions containing non-basic or
type-param-referencing terms beyond the `~[]E`/`~map[K]V` patterns,
intersections, `~` on named non-slice/map types. The E0506 message names
the class so a hidden list never silently widens.

Emission needs nothing new: `CallInfo.TypeArgs` already carries the
solved Gom types and codegen renders their Go view. Validation is a
post-inference pass beside `validateGoGenerics`' comparable check — new
constraint kinds on `check.GoConstraint` (struct replacing the int enum)
+ membership judgments on solved args, reported as E0508 (existing code,
constraint string in `data`).

Residuals recorded: named-type tildes whose member is a Gom-side
`types.Go` handle (underlying comparison needs a go/types oracle —
rejected honestly); `~int` admitting Go's `int` but not Gom's `Int`
(int64) — same honesty; `iter.Seq`-returning generics hidden on result
mapping, unrelated to the constraint class.

## Prototype (2026-11-17, this branch)

Implementation settled on a refinement of the draft: instead of binding
`S` to the argument's own type and judging the repr afterward, the
constraint widens the parameter's *surface* — `instantiateSig`
substitutes `S ~[]E` with `Go.Slice ?E` (and `M ~map[K]V` with
`Go.Map ?K ?V`) across the whole signature, sharing the operand's meta.

Why the substitution rather than repr-judging:

- **Mutation soundness.** `S` spans the callee's mutable parameter —
  `slices.Sort`, `slices.Reverse`, `slices.Delete` write into `s`.
  Binding `S := List Int` would emit `Sort[list.List[int64], int64](xs)`
  verbatim and let Go write into a `List`'s backing. With the `Go.Slice`
  surface a `List` argument takes the ordinary boundary copy
  (`xs.ToSlice()`), `List`'s immutability is preserved for free, and
  `W0501` warns on the copied-in-place shape — the lint resolves a
  `Var` parameter through `CallInfo.TypeArgs` to see it.
- **CTI for free.** `maps.Equal`'s K/V occur only inside M's
  constraint; the container surface binds them through ordinary
  unification — no separate constraint-type-inference pass.
- **Honest results.** `slices.Clip`'s `S` result surfaces `Go.Slice E`
  — the emitted slice *is* Go-owned, which is what `Go.Slice` means.

Verified:

- `ok_go_structural_generics.gom`: `slices.Contains`/`Index` on
  `List Int` (copy emitted), `maps.Equal` on `Go.Map` (shared),
  `cmp.Less` — clean.
- Emission: `goiface.Index[[]int64, int64](xs.ToSlice(), n)`,
  `maps.Equal[map[string]int64, map[string]int64, string, int64](a, b)`
  — bridge test compiles and runs.
- `slices.Clip`/`Sort`/`Reverse` check clean; `Sort`/`Reverse` on a
  `List` warn W0501 (sorted copy is discarded — honest no-effect).
- Element mismatch (`List Int` + `String` element arg) is E0401
  argument unification, not a repr-level report — better diagnostic.
- Honest residuals: `maps.Keys` E0501 (result `iter.Seq` unmappable),
  generic-Gom-fn forwarding (`xs : List a`) E0508 on `Go.Slice a` /
  `a` comparable — a `Var` has no Go repr, same as `comparable`'s
  existing rejection; `~int`-unions reject `Int` (repr int64);
  `errors.AsType` E0506 "carries a method set".
