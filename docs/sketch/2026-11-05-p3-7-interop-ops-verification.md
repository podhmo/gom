# P3-7 interop ops — verification

Verification of the three decisions drafted from the P3-7
real-program audit
([sketch](2026-11-05-p3-7-realworld-interop-audit.md)):

- [Ptr ops surface](../decisions/2026-11-05-ptr-ops-surface.md)
- [Go.Map ops](../decisions/2026-11-05-go-map-ops.md)
- [W0501 copied-List mutation warning](../decisions/2026-11-05-go-mutation-warning.md)

exercised through `gom check`, the spec-fixture suite
(`testdata/spec/`), the golden suite (`testdata/golden/`), and the
new `rt` unit tests (`rt/ptr_map_test.go`).

## `Ptr` module

```gom
fn ptr_ops () -> Int = {
  let p = Ptr.make 1
  Ptr.set p (Ptr.get p + 1)
  let z : Ptr Int = Ptr.new ()
  let n : Ptr Int = Ptr.nil ()
  if Ptr.is_nil n {
    Ptr.get p + Ptr.get z
  } else {
    0
  }
}

fn opt_of (p: Ptr Int) -> Option Int = Ptr.to_option p
```

Result: compiles clean (`testdata/spec/ok_ptr_ops.gom`). Golden
output (`testdata/golden/ptr_map_ops.golden.go`) shows the expected
shapes — `rt.PtrMake[int64](1)`, `rt.PtrSet[int64](p, tmp2)`,
`rt.PtrNew[int64]()`, `rt.PtrNil[int64]()`, and `Ptr.to_option`
destructured through `option.FromGo(v, ok)`.

The audit's three unusable shapes now work — pinned end to end by
`testdata/spec/ok_ptr_go_boundary.gom` against `goiface` helpers
(`ptrops.go`):

- `flag.Int64 "n" 0 ""` returns `Ptr Int` — `Ptr.get` reads the
  bound value back (previously dead value; `goiface.Int64Box`
  stands in, plus a `Ptr.set`-through-Go-pointer round trip).
- `tls.Dial (network, addr, Ptr.nil ())` feeds a nil `*tls.Config`
  to an optional `*T` parameter (previously uncallable;
  `goiface.UsesConfig` reports the nil crossing).
- `errors.As` becomes expressible with an explicit annotation:
  `let t : Ptr (Ptr os.PathError) = Ptr.new ()` then
  `errors.As (e, t)` and `Ptr.get t` — the pointer-to-pointer
  target the Go contract needs, opt-in by the annotation. The
  `any`-parameter out-pointer shape is pinned by
  `goiface.FillPtr`: `Ptr.new ()` passes the `any` boundary
  verbatim, the callee writes the pointee, `Ptr.get` reads it
  back.

Nil semantics verified in `rt/ptr_map_test.go`: `PtrGet`/`PtrSet`
panic on `PtrNil` like Go's own dereference, `PtrIsNil` and
`PtrToOption` are total, `PtrMake`'s copy never reaches its
argument, `PtrNew` zero-fills.

Unknown members report `` `Ptr` has no member `bogus` ``.

## `Go.Map` ops

```gom
fn map_ops () -> Int = {
  let m = Go.Map.make ()
  Go.Map.set m "k" 3
  let v = match Go.Map.get m "k" {
    Some x -> x,
    None -> 0,
  }
  Go.Map.delete m "k"
  v + Go.Map.length m
}

fn pairs (m: Go.Map String Int) -> List (String, Int) = Go.Map.to_list m
```

Result: compiles clean (`testdata/spec/ok_go_map_ops.gom`).
`Go.Map.get` materializes `Option` through
`option.FromGo(v, ok)` in generated code; `Go.Map.to_list` lowers
to `rt.MapToList` followed by the ordinary `list.FromSlice`
boundary copy.

- `k`'s `comparable` constraint bites: `Go.Map.set m ["x"] 1`
  where `m : Go.Map (List Int) Int` reports E0508 naming `MapMake`,
  parameter `k`, and `comparable`
  (`testdata/spec/E0508_go_map_key.gom`) — the same check that
  guards generic Go functions, no new machinery.
- Unknown member: `` `Go.Map` has no member `bogus` ``
  (`testdata/spec/E0201_go_map_member.gom`).
- Nil-map semantics are Go's, verified in `rt/ptr_map_test.go`:
  get/delete/len on nil are total, `MapSet` on nil panics.
- `MapToList` on a nil map yields an empty pair slice — the
  `for x in pairs` surface composes with `List.map`.

## W0501 copied-List warning

```gom
import go "sort"
import go "strings"

// Redeclare the parameter as `Go.Slice` — the no-copy escape hatch.
extern go "sort" {
  fn Strings (xs: Go.Slice String) -> Unit
}

fn calls () -> Unit = {
  let fs = [2.0, 1.0]
  sort.Float64s fs                       // warns — List copies into []T
  let xs = ["b", "a", "c"]
  sort.Strings xs                        // warns — List → declared Go.Slice still copies
  ignore strings.Join xs ","             // silent — non-Unit result
  let gs: Go.Slice String = Go.conv xs : Go.Slice String
  sort.Strings gs                        // silent — Go.Slice → Go.Slice
  ()
}
```

Result (`testdata/spec/W0501_go_slice_copy.gom`): three warnings,
each at the argument span with `data.callee` naming the Go callee
and `data.argument` the argument's own type — `sort.Float64s fs`,
`sort.Strings xs`, and `goiface.MutSlices [1, 2]`. The
`sort.Strings xs` call still warns even though the extern
signature declares `Go.Slice` — what decides the copy is the
*argument's* type, and a `List` converts into the declared
`Go.Slice` parameter by copying.

The verbatim `Go.Slice` paths stay silent — the copy test counts a
`Go.Slice`-typed argument only when the checker materialized it
through the `Slice`-marked Ascribe:

- `goiface.MutSlices gs` — `gs : Go.Slice Int` fills one
  `...[]int64` element and emits `MutSlices(gs)` verbatim;
  `MutSlices(..gs2)` — `gs2 : Go.Slice (Go.Slice Int)` — emits
  `MutSlices(gs2...)`; mutations through the tail reach the
  caller's backing in both, so neither warns (pinned by the
  fixture's `verbatim` fn; the golden probe showed the emitted
  calls carry no conversion).
- `ok_chan_elem_types.gom`'s `goiface.SendSlice(ch, v)` — the
  ascribe path — still warns: `v`'s `Go.Slice` is materialized
  into a `List` (`FromSlice` copy), so the warning and its
  conditional wording remain correct. Its expected JSON keeps the
  warning — the only pre-existing fixture the lint changes.

The other false-positive controls hold:

- `strings.Join xs ","` — a non-Unit result — stays silent; the
  result-shape test removes the read-only `[]T` consumers.
- `..xs` spreads of a `List` warn (the copy happens there too) —
  `TestCallGoSpread` and `TestDeferredSpread`'s Unit-result stubs
  pin `W0501` in their expected codes.

## Companion fixes

- `check`'s lint sweep lives beside W0701/W0702 in
  `lintMutableSharing`; `ArgToParam` is honored so labeled-arg
  reorderings map arguments to the right parameter.
- The `Go` namespace type-member path (`ctorTable`) now services
  `"Go.Map"` the same way module tables serve `Chan`/`Task`;
  misses keep the "compiler-owned member table" wiring note.

## Non-regression

`gofmt -l .` clean; `go vet ./...` clean; `go test ./...` green —
`TestSpec`/`TestGolden` in `internal/build`, the `gom` binary
end-to-end tests, `rt` tests, and the skill-snippet verification.
Golden output changed only by the new `ptr_map_ops` fixture.

## Known limitations (accepted in the decisions)

- W0501 cannot detect mutation *semantically* — `signal.Notify`-
  family calls warn; a mutator that returns a real value escapes
  the shape. Nested `List`s inside tuple/record arguments do not
  warn (top-level only).
- `==`/`!=` on `Ptr` are Go pointer-identity — already shipped
  (`eqNative` in `internal/lower/equal.go`, spec 3.3); nil tests
  go through `Ptr.is_nil`/`Ptr.to_option`.
- `errors.As`-style "an `any` that must be a non-nil pointer"
  contracts remain unstated; misuse panics at runtime. A typed
  binding or lint is future work.
- `Go.Slice` has no read-only member surface yet — `"Go.Slice"` is
  the next `Config.Modules` key under the same mechanism.
- `Go.Map.to_list` iteration order is unspecified (Go's range);
  a real `for`-over-map form is future work.
