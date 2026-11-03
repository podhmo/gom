# P3-3 Go collections and generic-call verification

- 日付: 2026-11-04

Verification for
[decisions/2026-11-04-go-collections-and-generics.md](../decisions/2026-11-04-go-collections-and-generics.md):
the `[]T`→`List` boundary copy, `Go.Slice`/`Go.Map`, and generic Go
calls under the `any`/`comparable` constraint set.

## Method

- `internal/goimport` module bridge (`moduleLoader`): a real Go
  module loaded through `replace`, a `.gom` bridge compiled through
  the full pipeline, and a generated `_test.go` run by `go test` —
  both directions, Go→Gom→Go.
- Spec fixtures (`testdata/spec`) through `build.Compile`; golden
  output (`testdata/golden`) reviewed line by line.

## Boundary copies (spec 8.5)

`testdata/golden/go_slice_boundary.golden.go` emits the intended
readable code:

- `let args = os.Args` → `args := list.FromSlice(os.Args)` (copy in)
- `strings.Join(args, " ")` → `tmp1 := args.ToSlice()` (copy out)
- `fmt.Println(..args)` → `tmp2 := rt.BoxSlice(args)` (elementwise
  box for `...any`)
- `os.Environ()` result → `env := list.FromSlice(tmp3)`

Nil: `echo(nil)` over `model.EchoSlice` stays nil in both directions
(`slices.Clone(nil)` is nil) — asserted in the generated bridge
(`TestModuleInteropGenerics`, `TestModuleInteropBothDirections`).
Mutation isolation: a Go-side write after the call does not reach
the Gom list, and a `Go.Slice`-annotated parameter crossing a
`List`-typed boundary copies (required for immutability) —
`xs[0]="b"` leaves `xs[0]` `"a"` in
`TestModuleInteropBothDirections`. The field-read path
(`c.Names []string`) copies the same way
(`TestInterfaceAssignmentAndFieldsBothDirections`).

## Go.Slice / Go.Map

- `Go.Slice` spelled in Gom signatures keeps `Go.Slice` in both
  views — no `Conv`, identity preserved (the earlier
  `Go.Slice → Go.Slice` same-type `sliceConv` match that inserted a
  double copy was removed; `types.Equal` short-circuits before any
  kind selection).
- `map[K]V` → `Go.Map K V` in both views:
  `model.NewMap`/`Lookup`/`Keys` pass `map[string]int64` through
  untouched and `Keys`'s `[]K` result becomes `List K`
  (`TestModuleInteropGenerics`, loader view assertions in
  `TestGenericSignatureViews`).

## Generic calls

- Inferred: `goiface.Identity n` emits `goiface.Identity[int64](n)`.
- Declared order: `goiface.Second[A, B](b B, a A)` called
  `Second s n` emits `goiface.Second[int64, string]` — declaration
  order, not first-occurrence — asserted on the generated source and
  at runtime (`TestModuleInteropGenerics`).
- `comparable`: `goiface.Count` accepts `String`/`Int` arguments;
  `List Int` (a named slice, not comparable) reports E0508
  (`testdata/spec/E0508_generic_comparable`,
  `TestModuleInteropGenericDiagnostics`).
- Structural `~[]E` (`goiface.Index`, `slices.Contains`) reports
  E0506 with the constraint's rendering
  (`testdata/spec/E0506_hidden_go_apply`, same test).
- Regression on the fix: `cmpWalk` initially answered false for
  builtin scalars (`String` included) — every `comparable` argument
  failed E0508; the scalar case was added and the bridge re-verified.

## P3-2 preservation

Named types, interfaces, and alias transparency
(`TestModuleSessionIdentity`, `TestModuleInteropBothDirections`,
`TestInterfaceAssignmentAndFieldsBothDirections`) are unchanged —
one `go/types` session still answers `AssignableTo`/`Comparable` and
`ComparableSource` plugs into `goReprComparable`.

## Review fixes (constraint identity, container splits)

- `callableConstraint` verified against a go/types probe:
  `comparable` and `interface{comparable}` are `Identical` to the
  universe interface; `~int`, `int`, `int|string`,
  `interface{comparable; ~int}`, `interface{comparable; M()}` and
  `~[]E` are not — and `IsMethodSet` is false for all of them,
  including `comparable` itself, so the reviewer-suggested
  `IsComparable() && IsMethodSet()` would have admitted nothing.
  `types.Identical` on the underlying is the discriminator.
- `viewsCompatible` applied at every boundary-type site (signature
  parameters and results, vars/consts, struct fields):
  `model.Sum []int`, `model.Tally () map[string]int`, `model.Clamp`,
  `model.G` all hidden with their reasons
  (`TestModuleInteropGenericDiagnostics`,
  `TestGenericSignatureViews`); spec fixtures
  `E0501_go_container_view_split` and `E0506_approx_constraint` pin
  the messages. `First[T interface{comparable}]` stays callable and
  `Dup [][]string` stays exposed — positive coverage on both edges.

## Remaining scope notes

- Structural `~[]E`/`~map[K]V` constraints are deliberately out —
  the decision file records the follow-up surface.
- Spec 8.1.6's `slices.Contains` example remains non-callable; the
  drift is logged in `implementation-gaps.md` and the updated
  problem file.
