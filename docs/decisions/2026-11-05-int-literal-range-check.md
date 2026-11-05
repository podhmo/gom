# Integer and float literals must fit their static type's range

- 状態: 採用（実装・検証した — [検証](../sketch/2026-11-05-int-literal-range-check.md)。testdata/spec/E0417_int_lit_range と ok_int_lit_boundaries で固定。spec.md §3.3 と problems/2026-11-05-int-literal-range-check へ反映済み）
- 日付: 2026-11-05

## 決定

The checker rejects a literal whose value the static type at its own
position cannot represent — Go's constant rule, applied per literal:

- An integer literal typed `Int`, `Int64` (or `Go.Int`) must fit
  `int64`; typed `Int32` or `Char` it must fit `int32`; typed `Float`
  it must be a representable `float64` (any magnitude up to the
  float64 maximum rounds — only an absurd digit count overflows).
- A float literal must be a representable `float64` — `1e999` spells
  `+Inf` and is rejected for the same reason.
- A literal pattern's text already carries its sign; it is judged at
  the pattern's type as spelled.
- A literal that is the *direct* operand of a unary `-` is judged
  including the minus: `-9223372036854775808` is a legal `Int64`
  literal while bare `9223372036854775808` is not. Exactly one
  enclosing minus counts — the emitted Go binds `int64(-lit)` for the
  inner unary and applies outer minuses to the variable, matching
  Go's own reading of the same program (`- -2147483648` at `Int32`
  emits `tmp := int32(-2147483648); -tmp`, which compiles).
- A literal that is the *direct* operand of `Go.conv` is judged at
  the conversion's target type — `Go.conv lit : T` emits `T(lit)`, a
  constant conversion judged at `T`. A float literal operand at an
  integer target applies Go's constant-conversion rule: the value
  must be integral (`int32(1.5)` is rejected, `int32(2.0)` is legal)
  and then in range (`int32(2147483648.0)` overflows). When the
  target is not a range-bearing type (an interface, `String`), the
  literal's own type judges as usual.

Follow-up (2026-11-05, same day): named Go type targets now *are*
range-judged — `check.UnderlyingSource` consults the named type's
`go/types` underlying kind, so `Go.conv 4294967296 : os.FileMode`
(uint32 backing) is E0417 while `Go.conv 4294967295` passes. The
fallback above applies only when the underlying is non-Basic (or
the handle is instantiated/unresolved). See
[the named-target sketch](../sketch/2026-11-05-goconv-named-target-lit-range.md).

The unit of judgment is the literal, not the enclosing constant
expression: `let k : Int32 = 0 - 2147483648` is rejected even though
the whole expression would fit — the generated code emits
`tmp1 := int32(0); return tmp1 - 2147483648`, where the right literal
must convert to `int32` alone because the left operand is a variable
(`litFoldDiverges` defeats Go's constant fold so that arithmetic stays
per-operation machine arithmetic, spec 3.3 /
[numeric-arithmetic-per-operation](2026-11-05-numeric-arithmetic-per-operation.md)).

Violations are a new type-checking diagnostic E0417.

## 背景

`litType` gives every integer literal a num-constrained meta with
default `Int` and never parses the digits. The meta solves to the
annotation's type (`let n : Int32 = 3000000000`) and codegen renders
the lexeme verbatim — `var n int32 = 3000000000` cannot build. The
same acceptance produces `tmp1 - 2147483648` through
`litFoldDiverges` (`0 - 2147483648` at `Int32`) and
`var x int64 = 99999999999999999999` — all accepted, all unbuildable
Go. The problem is recorded in
[problems/int-literal-range-check](../problems/2026-11-05-int-literal-range-check.md)
and the [implementation-gaps](../implementation-gaps.md) ledger.

The evaluator already materializes integer literals by wrapping to
the type's width (`litValue`'s `int64(int32(n))` and the
ParseUint-fallback for `9223372036854775808` —
[eval-int-width-aware-ops](2026-11-05-eval-int-width-aware-ops.md)).
Under this decision that normalization is **defensive**: a checked
program never contains an out-of-range literal, so the wrap only
serves hand-built typed ASTs (eval tests, in-process callers) that
bypass the checker. The problems doc is updated to say so.

## 選択肢

1. **Reject out-of-range literals at check（採用）** — Go's own
   constant rule at the position the emitted code puts the literal.
   Every accepted program generates Go that builds; the rejected
   programs all have a spelled-in-range equivalent (`-2147483647 -
   1`, a computed value, a wider type).
2. **Wrap-materialize** — give literals the spec 3.3 wrap-around
   semantics (`3000000000 : Int32` means `int64(int32(3000000000))`).
   It requires emitting non-literal code for literal spellings —
   `int32(-1294967296)` or a bit-twiddling expression — diverging
   from "the source lexeme is the Go lexeme", and makes literal
   *spelling* dependent on type context in a way no other rule does.
   It also cannot rescue the `litFoldDiverges` shape without teaching
   the fold-defeat new emission.

(1) を採る。Rejection matches what the emitted Go would already
require, keeps literal meaning type-independent, and the one program
shape Go accepts that Gom then rejects (`0 - 2147483648 : Int32`)
is exactly the shape the per-operation semantics forbids anyway.

## 実装

`internal/check` only:

- `litType` call sites (`checkExpr`'s `*syntax.Lit` case and
  `checkPat`'s `*syntax.LitPat` case) register a `pendingLit` for
  `LitInt`/`LitFloat` — the span, normalized text, and the literal's
  type (usually its fresh meta).
- `checkUnary` marks a pendingLit whose node is the *direct* operand
  of `UnNeg`; `checkAscribe`'s `Go.conv` path marks one whose node is
  the direct operand with the conversion target.
- `finalize` runs `revisitLitRanges` after `revisitConvs` — the same
  post-default moment, when every literal meta is solved or bound to
  its default. Each pendingLit resolves its judgment type (conv
  target if range-bearing, else its own type), parses the text with
  `go/constant` (`constant.MakeFromLiteral` under `token.INT` /
  `token.FLOAT`, then `constant.Float64Val` for the float64
  judgment and `constant.ToInt` for a float-to-integer constant
  conversion), and reports E0417 with the signed value in `data`
  when it overflows.

`internal/diag/codes.go` appends E0417 (append-only; the serial was
free). `docs/spec.md` section 3.3 gains the literal-range sentence;
`docs/problems/2026-11-05-int-literal-range-check.md` records the
decision and the defensive eval wrap. No lowering or codegen change —
the emitted shapes were already Go-legal for in-range literals.

## 検証

- Prototype + sketch: `docs/sketch/2026-11-05-int-literal-range-check.md`
- `testdata/spec/E0417_int_lit_range.gom` covers annotation position,
  `litFoldDiverges`, `Go.conv` operand position, and pattern
  position; `testdata/spec/ok_int_lit_boundaries.gom` keeps
  `-9223372036854775808`, `- -2147483648`, and boundary values
  accepted.
