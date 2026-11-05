# `Go.Slice` operations: `Go.Slice.<op>` read-only member table backed by `rt`

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-go-slice-ops-verification.md)。spec.md §8.3/§8.5 へ反映済み）
- 日付: 2026-11-05

## Context

`Go.conv` between `Go.Slice T` and `List T` now copies
([decisions/goconv-list-slice-copies](2026-11-04-goconv-list-slice-copies.md)),
which is the semantics the implicit boundary already used — but it
removes the last free way to look *inside* a `Go.Slice`: without an
operation surface, a `Go.Slice` value can only be passed onward or
copied wholesale into a `List`. `Go.Map` resolved the same problem
by hanging a compiler-owned, `rt`-backed member table off the type
member
([decisions/go-map-ops](2026-11-05-go-map-ops.md)); `Go.Slice` was
always expected to follow the same hook (`"Go.Slice"` was reserved
in the go-map-ops decision text).

## Decision

`Go.Slice.<op>` resolves through a compiler-owned member table —
the same `rt`-backed mechanism and the same *type-member* path as
`Go.Map`: `Slice` resolves as a `Go`-namespace type member, then
the operation in that type's member table, injected under the
`Config.Modules` key `"Go.Slice"`. `goTypeModules` in
`internal/resolve/scope.go` lists `"Slice"` so an unwired compile
reports the wiring note rather than "no member surface".

Five members, all read-only — the container comes first like every
`Go.Map` member:

| member | Gom signature | Go meaning |
| --- | --- | --- |
| `Go.Slice.length` | `Go.Slice a -> Int` | `len(s)` — nil is 0 |
| `Go.Slice.get` | `Go.Slice a -> Int -> Option a` | index — `None` out of bounds or negative |
| `Go.Slice.slice` | `Go.Slice a -> Int -> Int -> Go.Slice a` | `s[lo:hi]` — shared sub-view, panics like Go on bad bounds |
| `Go.Slice.fold` | `Go.Slice a -> (b -> a -> b) -> b -> b` | left fold in index order — nil folds to acc |
| `Go.Slice.to_list` | `Go.Slice a -> List a` | snapshot copy — the only copying member |

- Semantics are Go's where Go has them: `length` and `fold` are
  total on nil, `slice` panics on out-of-range or inverted bounds
  exactly like the slice expression it wraps, and the sub-view
  shares backing — later writes on either side reach the other.
- `get` returns `Option a`, not a panic: indexed access in the Gom
  surface is `Option`-shaped everywhere else (`List.get`,
  `Go.Map.get`), and the caller who wants Go's `s[i]` panic can
  check `length` first. A panicking form can be added later the way
  `Ptr` pairs `get` with `to_option`.
- `fold`'s callback is the canonical `b -> a -> b` — one Go func
  per arrow — so a Gom function value forwards verbatim into
  `rt.SliceFold[T, B]`'s `func(B) func(T) B`.
- There are deliberately no write members (`set`, `append`,
  `fill`). `Go.Slice` is Go's own mutable view: writes belong to
  the Go side that owns the slice, and a Gom-side `set` would give
  an immutable-looking language value mutation semantics. To write,
  convert to `List` and pass a `Go.Slice` back, or mutate on the Go
  side.
- The table is rt-backed so the same machinery as `Go.Map` emits
  `rt.SliceLen[T](s)`, `rt.SliceGet` through `option.FromGo`, and
  `rt.SliceToList` through `list.FromSlice` — no new conversion
  kinds were needed.

## Alternatives

- *Container-last argument order like `List.get`/`List.fold`*:
  would make `gs |> Go.Slice.fold f acc` pipeline, but breaks the
  `Go.*` convention — every `Go.Map` member is container-first
  (`Go.Map.get m k`), and `Go.Slice` is the sibling surface where
  "member = method on the Go-owned value" reads most naturally.
- *`slice` returning `Option (Go.Slice a)`*: hides the bounds
  contract behind an Option every range read pays for; `s[lo:hi]`
  has no comma-ok form in Go, so panic is the faithful meaning and
  `length` is the pre-check.
- *`get` panicking like `s[i]`*: Go-faithful but inconsistent with
  the rest of the Gom read surface; `Option` was chosen and the
  panic form stays addable without breaking anyone.
- *Iteration via `for` only*: `for` does not accept a `Go.Slice`
  today — `checkFor` admits only builtin `List` and channels, so
  `for x in gs` is E0401 (whether it should is a separate open
  question; core's `ForList` already models the slice-family
  range). `fold` is the only in-place iteration, and even if
  `for` later gains `Go.Slice` it stays needed for the
  accumulating read (`sum`, `count`) — `for` returns `Unit`.
- *A `.gomi` stdlib surface*: impossible for the same reason as
  `Go.Map` — `fold` is generic over `(a, b)` and the members need
  rt-backed Go symbols.
