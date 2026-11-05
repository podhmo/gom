# `Go.conv` between `Go.Slice T` and `List U` requires equal element types

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-goconv-slice-list-element-mismatch.md)。testdata/spec/E0507_go_conv_slice_elem_mismatch で 4 件の拒否を固定。spec.md §8.4 へ反映済み）
- 日付: 2026-11-05

## 決定

A `Go.conv` whose sides are `Go.Slice T` and `List U` (in either
direction) is legal only when `T` and `U` are the same Gom type —
the same element equality the implicit boundary conversion of spec
8.5 already requires. A mismatched pair is E0507. This holds at any
nesting depth of the element types' spelling: the judgment is on
`types.Equal` of the two element arguments, so
`Go.Slice (Go.Slice Int64) ↔ List (List Int)` mismatches too.

## 背景

`Go.Slice T` and `List T` copy at the boundary
([goconv-list-slice-copies](2026-11-04-goconv-list-slice-copies.md)).
The lowering's copy cases are gated on equal element types
(`goConvKindOf`: `toSlice && fromList && Equal(fe, te)` etc.). A
mismatched pair — `Go.Slice Int64` → `List Int`,
`Go.Slice Int32` → `List Char` — fails that gate and falls through
`convKind` to `ConvNamed`, emitting a raw named cast
`list.List[Int](xs)` / `[]rune(xs)` that *shares backing* instead of
copying. The alias runs both ways and both are unsound:

- `Go.Slice → List`: Go-side mutation through the slice (or the
  original `[]int64`) reaches the value Gom treats as immutable.
- `List → Go.Slice`: an in-place mutation the callee performs on the
  `Go.Slice` lands on the `List`'s backing storage.

The checker accepts these today because `convOK` asks the
`ConvertSource` oracle (`goimport.Loader.GoConvertible`), which maps
both sides to their Go *representations* — `[]int64` → `[]int64` for
`Int64`/`Int`, `[]int32` → `[]int32` for `Int32`/`Char` — and
`types.ConvertibleTo` then answers true for what are, at the Go
spelling, identical underlying types. The Gom-level element types
differ even though the renderings agree: `List Int` and
`List Int64` are different types in Gom's own reading.

The same pun exists through a *named* Go slice element or target:
`Go.conv xs : List Int` where `xs : Go.Slice Int64` is the case this
decision covers; pairs whose element types are merely
representation-compatible (`Ptr _`, `Go.Any`, a declared ADT) are
already refused by the oracle's `gomOnly` fallback or by Go's own
slice-element conversion rule — `[]*T` and `[]*U` are not
convertible — so element equality is the only hole.

## 選択肢

1. **Reject mismatched element pairs at `convOK`（採用）** —
   the minimal sound rule, and the one the language already states
   for every other List↔slice crossing: the implicit boundary
   conforms only `List T`/`[]T` pairs whose element types agree (an
   `E0401`/`E0507` shape otherwise). `Go.conv` should not admit a
   conversion the implicit boundary forbids.
2. **Element-wise copy** — `Go.Slice Int32 → List Char` could copy
   with a per-element `Char(v)` conversion, preserving the copy
   semantics while allowing the spelling. More permissive, but it
   invents a value-changing conversion `Go.conv` nowhere else
   performs (numeric `Go.conv` keeps the Go rule of a direct type
   conversion), needs emission machinery the boundary does not have,
   and silently wraps (`Int64 → Int32` truncates). The rejected
   program has an explicit spelling for the same effect —
   `List.map Char (Go.Slice.to_list xs)` — so the permissive form
   buys convenience at the cost of a hidden per-element coercion.

(1) を採る。The rule is one clause in `convOK`: when one side is
`Go.Slice` and the other the builtin `List`, compare the element
arguments with `types.Equal` before the oracle runs — the oracle's
representation-level convertibility cannot see the mismatch.

## 影響・回帰面

- Programs the decision rejects were previously accepted and emitted
  uncompilable-or-aliasing Go: `Go.Slice Int64 → List Int`,
  `Go.Slice Int32 → List Char`, nested `Go.Slice (Go.Slice Int64) →
  List (List Int)`. All now E0507.
- Equal-element pairs are unchanged: `Go.Slice Int ↔ List Int` still
  copies (`ConvSliceToGo`/`ConvSliceFromGo`), and `Go.Slice`↔`Go.Slice`
  conversions (a verbatim pun by design — `Go.Slice` is the mutable
  Go-owned spelling on both ends) are unaffected.
- The check lives in the checker's shape table, before the
  `ConvertSource` oracle — a file-local `type Int` inside the element
  types still routes through `containsShadowed` to the fallback
  first, so the ordering keeps that earlier fix.

## 実装

`internal/check/expr.go` `convOK` only: after the numeric pair rule
and before `containsShadowed`, recognize the `Go.Slice`/`List`
constructor pair (respecting `shadowedBuiltin("List")` like every
builtin read) and return false when the element types are not
`types.Equal`. `internal/lower` needs no change — the rejected
pairs can no longer reach `convKind`'s `ConvNamed` fall-through.

## 検証

- Prototype + sketch: `docs/sketch/2026-11-05-goconv-slice-list-element-mismatch.md`
- `testdata/spec/E0507_go_conv_slice_elem_mismatch.gom` covers both
  directions and a nested mismatch; `testdata/spec/ok_go_conv.gom`
  keeps the equal-element spellings accepted.
