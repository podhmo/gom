# Sketch: variant named-payload reads assert the owning ctor — verification log

Verification report for
[decision: variant-field-assert-owning-ctor](../decisions/2026-11-05-variant-field-assert-owning-ctor.md)
— `s.f` through the sum type asserts the value holds the one variant
declaring `f`; a wrong-variant value is a contract violation that
panics, same family as match's invalid-value arm
([problem](../problems/2026-11-02-variant-field-wrong-variant.md)).

## Mechanism

`check.MemberExpr.Ctor` already records the owning constructor, and
`core.FieldGet.Ctor` already compiles the read to a Go type
assertion `s.(Shape_Circle).R` — the change is entirely in
`internal/eval`'s `evalMember`: when `x.Ctor != nil`, the received
`VVariant`'s ctor is compared to `x.Ctor` before any
index/name fallback, using `matchPat`'s identity rule (symbol
equality, or a `Ctor == nil` bare-name value compared by name — the
shape host-bound stubs produce). A mismatch raises E9003
(`CodePanic`, "field access on the wrong variant") carrying the
selected field, expected ctor and the value. `x.Ctor == nil`
(hand-built ASTs, unresolved members) keeps the old
index → sym → name fallback.

## Evidence

`internal/eval/eval_test.go`, `TestADT/variant_field_read_asserts_the_owning_ctor`:

- `s.radius` on `Circle 1.5` answers `1.5` (ok path, `Ctor` set)
- `s.radius` on `Rect 2.0 3.0` fails with E9003 — previously fell
  through to index 0 and answered `2` (the divergent read)
- `s.radius` on `Empty` fails with E9003 — not "variant has no such
  field", which is the internal-invariant code
- a bare-name `VVariant{Name: "Circle"}` reads fine;
  `VVariant{Name: "Rect"}` fails with E9003

`internal/build/compare_test.go`, `TestWrongVariantFieldPanic` —
same program on both backends:

```gom
type Shape = | Circle (r: Float) | Rect (w: Float) (h: Float) | Empty
fn getr (s: Shape) -> Float = s.r
fn answer () -> String = "${getr (Rect 2.0 3.0)}"
```

- eval: `answer ()` fails with a single E9003 diagnostic
- binary: exits non-zero with
  `panic: interface conversion: main.Shape is main.Shape_Rect, not main.Shape_Circle`
- the `Empty` argument instance behaves identically on both sides
- the ok path is pinned by the existing `variant_named_field`
  compare case (`want: 42`) and `ok_variant_named_field.gom` spec
  fixture, both still passing

No generated-code or golden change — the compiled side already
asserted; only the evaluator moved.

## Residuals

- Message wording is intentionally left implementation-defined in
  spec: eval reports a structured E9003 diagnostic, generated code
  surfaces Go's native type-assertion panic text. The spec pins the
  panic itself, not the string.
- `docs/problems/2026-11-02-variant-field-wrong-variant.md`'s
  observation that check's unique-name rule leaves the runtime
  question open is answered by the decision; the problem doc is
  marked resolved.

Verified with `go test ./internal/eval`, `go test ./internal/build`
(`TestEvalMatchesCompiled`, `TestWrongVariantFieldPanic`) and
`go test ./...` on the worktree.
