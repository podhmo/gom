# Sketch: integer/float literal range checking — verification log

Verification report for
[decision: int-literal-range-check](../decisions/2026-11-05-int-literal-range-check.md)
— rejecting literals whose value the static type at their own
position cannot represent (E0417), so every accepted program emits
Go constants that build.

## Mechanism

`internal/check` records a `pendingLit` for each `LitInt`/`LitFloat`
expression and pattern at `litType` time; `checkUnary` marks a
literal that is the *direct* operand of `-` (the emitted `-lit` is
one Go constant expression) and `checkAscribe`'s `Go.conv` path
marks one whose node is the direct operand (the emitted `T(lit)`
converts at the target). `finalize` runs `revisitLitRanges` after
`revisitConvs` — the same post-default moment — so each literal's
meta has solved or bound its default before the judgment. Ranges
come from `go/constant`: signed two's-complement bounds for
`Int`/`Int64`/`Go.Int` (64-bit) and `Int32`/`Char` (32-bit), a
float64-representability check for `Float`, and — for a `LitFloat`
reaching an integer target through `Go.conv` — Go's
constant-conversion rule via `constant.ToInt` (a non-integral value
is rejected outright; an integral one joins the range judgment).
Named Go types,
interfaces, unsolved metas and file-local ADT spellings skip the
check (`litRange` returns not-ok) — a named target's width is the
oracle's knowledge, recorded in the decision as the accepted
residual.

## Evidence

Prototype run (`gom check`) on the boundary matrix — every reject
fires at the literal's own span, every accept stays silent:

```gom
fn main () -> Unit = {
  let n : Int32 = 3000000000          // E0417 (annotation position)
  let k : Int32 = 0 - 2147483648      // E0417 at 2147483648 (litFoldDiverges)
  let x : Int64 = 99999999999999999999 // E0417
  let m : Int64 = -9223372036854775808 // ok — direct `-` folds the sign
  let w : Int32 = - -2147483648        // ok — one minus folds, outer runs on tmp
  let b : Int32 = -2147483648          // ok — signed min
  let mx : Int32 = 2147483647          // ok — signed max
  let mn : Int64 = 9223372036854775807 // ok
  let c = Go.conv 3000000000 : Int32   // E0417 — operand judges at Int32
  let f : Float = 99999999999999999999 // ok — float64 rounds, not overflows
  let g = 1e999                        // E0417 — +Inf is not a float64
  let h : Float = 1e308                // ok
  ...
}
```

Emitted-Go spot check on the accepted shapes confirms the constants
the judgment protects (`gom build`):

```go
b := int32(-2147483648)              // one constant expression — legal
m := int64(-9223372036854775808)     // Go's own unary-minus reading
tmp1 := int32(-2147483648); w := -tmp1   // `- -lit`: inner binds once
tmp2 := int32(0); k := tmp2 - 2147483647 // litFoldDiverges shape
```

The rejected `0 - 2147483648` would have emitted `tmp2 - 2147483648`
— `2147483648` converts alone at `int32` and cannot build, which is
exactly why the judgment unit is the literal, not the enclosing
expression.

Pattern position verifies through `LitPat` (its text already spells
the sign): `match n { 3000000000 -> ... ; _ -> ... }` at
`n : Int32` reports E0417; `match m { -9223372036854775808 -> ... }`
at `Int64` is accepted.

Regression coverage:
`testdata/spec/E0417_int_lit_range.gom` pins the rejections
(annotation, `litFoldDiverges` shape, Int64 overflow, `Go.conv`
operand, float overflow, pattern position) plus the wave8-review
float→integer conversion cases — `Go.conv 1.5 : Int32`/`Char`
(non-integral), `Go.conv 1e100 : Int64` and `2147483648.0 : Int32`
(integral but out of range), a signed `-1e999` overflow, and a
`-1.5` float pattern at an `Int32` scrutinee;
`testdata/spec/ok_int_lit_boundaries.gom` pins the accepts (signed
minima in expression and pattern position, maxima, int-spelled
literal at Float, `Go.conv` operands at a named Go target and at
range-bearing targets, and the integer-valued float operands
`Go.conv 2.0 : Int32` / `2147483647.0 : Int32` — both verified to
emit `int32(2.0)`/`int32(2147483647.0)`, which `go build` accepts).
`TestSpec` is green with no expected changes
outside the new fixtures — no previously-accepted literal in the
suite was out of range.
