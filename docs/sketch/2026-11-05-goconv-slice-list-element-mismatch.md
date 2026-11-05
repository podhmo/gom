# Sketch: `Go.conv` slice/list element-mismatch gate — verification log

Verification report for
[decision: goconv-slice-list-element-mismatch](../decisions/2026-11-05-goconv-slice-list-element-mismatch.md)
— rejecting `Go.Slice T ↔ List U` `Go.conv` pairs whose element
types differ, closing the raw-cast fallback that aliased mutable Go
slice storage into immutable `List` storage.

## Mechanism

`convOK` (`internal/check/expr.go`) gains one clause ahead of the
`ConvertSource` oracle: `sliceListPair` decomposes the sides when
one is the builtin `List` (respecting `shadowedBuiltin`, so a
file-local `type List` ADT still falls through to the oracle) and
the other is `Go.Slice`; `types.Equal` on the two element arguments
decides. The clause sits after the numeric-pair rule and before the
`containsShadowed` fallback, so neither earlier fix is disturbed.
`internal/lower` needs no change — rejected pairs can no longer
reach `convKind`'s `ConvNamed` fall-through, which was the unsafe
emission (`list.List[Int](xs)` / `[]rune(xs)` sharing backing).

## Evidence

Prototype run (`gom check`) on one file covering both directions, a
nested mismatch, and the three pairs that must stay accepted:

```gom
fn freeze (xs: Go.Slice Int64) -> List Int = Go.conv xs : List Int
fn thaw (xs: List Int64) -> Go.Slice Int = Go.conv xs : Go.Slice Int
fn chars (xs: List Char) -> Go.Slice Int32 = Go.conv xs : Go.Slice Int32
fn thru (xs: Go.Slice (Go.Slice Int64)) -> List (List Int) =
  Go.conv xs : List (List Int)
fn ok1 (xs: Go.Slice Int64) -> List Int64 = Go.conv xs : List Int64
fn ok2 (xs: List Char) -> Go.Slice Char = Go.conv xs : Go.Slice Char
fn ok3 (xs: Go.Slice Int64) -> Go.Slice Int = Go.conv xs : Go.Slice Int
```

Result — each mismatched pair reports E0507 at its `Go.conv` span;
all three `ok*` functions are silent:

```
E0507  cannot convert Go.Slice Int64 to List Int            (line 3)
E0507  cannot convert List Int64 to Go.Slice Int            (line 5)
E0507  cannot convert List Char to Go.Slice Int32           (line 7)
E0507  cannot convert Go.Slice (Go.Slice Int64) to
       List (List Int)                                      (line 9)
```

Notes on the outcomes:

- `ok3` (`Go.Slice Int64 → Go.Slice Int`) is accepted — the
  `Go.Slice ↔ Go.Slice` direction is the documented verbatim pun
  (Go-owned mutable spelling on both ends), unaffected by the gate.
- The nested case shows the judgment is on the *element* type
  constructor, recursively: `Go.Slice Int64` vs `List Int` inside
  the element position also mismatches.
- Before the change the same file checked clean and lowered
  `freeze` through `ConvNamed` — the emitted
  `list.List[Int](xs)` shares `xs`'s backing.

Regression coverage:
`testdata/spec/E0507_go_conv_slice_elem_mismatch.gom` pins the four
rejections; `testdata/spec/ok_go_conv.gom` already exercised the
equal-element `List String ↔ Go.Slice String` copies, still
accepted (`TestSpec` green, no expected changes outside the new
fixtures).
