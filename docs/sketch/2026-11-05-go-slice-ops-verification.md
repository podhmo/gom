# `Go.Slice` ops — verification

Verification of
[decisions/2026-11-05-go-slice-ops](../decisions/2026-11-05-go-slice-ops.md)
exercised through the spec-fixture suite (`testdata/spec/`), the
golden suite (`testdata/golden/`), the new `rt` unit tests
(`rt/slice_test.go`), and the `testdata/gocall` end-to-end package.

## Member surface

```gom
fn ops (gs: Go.Slice Int) -> Int = {
  let n = Go.Slice.length gs
  let first = match Go.Slice.get gs 0 {
    Some v -> v
    None -> 0
  }
  let sub: Go.Slice Int = Go.Slice.slice gs 1 n
  let total = Go.Slice.fold sub (fn acc v -> acc + v) 0
  let back: List Int = Go.Slice.to_list sub
  first + total + List.length back
}
```

Result: compiles clean (`testdata/spec/ok_go_slice_ops.gom`, plus
`count` exercising `fold` on a `Go.conv`-materialized `Go.Slice`).
Member misses report `` `Go.Slice` has no member `bogus` `` with
`namespace: member` — the type-member table path, not the
constructor miss (`testdata/spec/E0201_go_slice_member.gom`).

## Generated shapes

Golden output (`testdata/golden/go_slice_ops.golden.go`) shows the
expected shapes — every member forwards into `rt` instantiated with
the solved type arguments, no conversion kinds invented:

- `rt.SliceLen[int64](gs)` for `length`
- `v1, ok := rt.SliceGet[int64](gs, 0)` + `option.FromGo(v1, ok)`
  for `get` — the `(v, ok)` shape wraps into `Option`
- `rt.SliceSub[int64](gs, 1, n)` for `slice` — the shared
  `s[lo:hi]` view verbatim
- `rt.SliceFold[int64, int64](sub, tmp4, 0)` for `fold` — two type
  arguments, and the curried `b -> a -> b` Gom value emits as a
  `func(int64) func(int64) int64` literal that forwards verbatim
- `tmp5 := rt.SliceToList[int64](sub)` + `list.FromSlice(tmp5)`
  for `to_list` — `SliceToList` is pass-through; the snapshot
  materializes through the ordinary `Go.Slice → List` boundary
  conversion (one clone, owned by `list.FromSlice`)

## Runtime contracts

`rt/slice_test.go` pins the Go-semantics claims:

- `SliceLen(nil) = 0`, `SliceGet(nil, 0)` misses, `SliceFold(nil,
  f, acc) = acc`, `SliceToList(nil)` is empty — nil reads are total.
- `SliceGet` reports `ok=false` on the out-of-bounds and negative
  index; `SliceSub` panics on out-of-range and inverted bounds like
  `s[lo:hi]` — including `SliceSub(nil, 0, 0)` being legal.
- `SliceSub` shares backing (a write through the view reaches the
  source) while `SliceToList` is pass-through — the member's
  snapshot guarantee is the `list.FromSlice` clone at the
  generated call site, so the rt half must not clone.
- `SliceFold` accumulates in index order, `f(acc)(v)` — the `b ->
  a -> b` curried shape.

## End to end

`testdata/gocall` exercises the ops against a Go-owned `[]int64`
(`geom.sliceOps`): `fold` sums the slice, `slice`/`get` read the
shared tail, `to_list` returns a snapshot that stays `[2 3]` after
the Go caller mutates `owned[1]` — the read surface never copied
except where `to_list` was asked to. Go side:
`TestSlicesFromGo/SliceOps` in `geom_test.go`.

## Commands

- `go test ./internal/build -run 'TestSpec|TestGolden'` — fixtures
  minted with `-update`, regenerated output inspected above
- `go test ./rt -run TestSlice -v` — all four cases pass
- `go test ./cmd/gom -run TestEndToEnd_GoCallsGom` — generated
  package compiles and runs against the real `rt` functions
