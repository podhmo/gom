# Structural `==`/`!=` emission: `rt.Equal` except provably comparable scalars

- 状態: draft
- 日付: 2026-11-04
- Superseded by: [generated-equality-comparators](2026-11-05-generated-equality-comparators.md) のうち composite 型への一律 `rt.Equal` ルート — per-type comparators landed; `rt.Equal` remains only as the documented fallback for Go-view types.

## Context

spec section 3.3 gives Gom structural equality: `==` on any
non-function type must agree with the evaluator's `eqValue`. The
generated Go used to map `==`/`!=` onto Go's own `==`/`!=`
unconditionally, which fails two ways: `list.List` and `Go.Slice`
are slices and do not compile under `==` at all, and an ADT value is
an interface whose `==` *panics* at runtime the moment a variant
holds a slice, instead of answering `false`.

`rt.AssertEq` already answers the same question for tests with
`reflect.DeepEqual` "until compiler-generated per-type compare
functions exist" (design.md section 4.8), and the runtime carried
that interim story in comments. This decision extends it to `==`
itself.

## Decision

Lowering picks the emission on the operand's static type — the Core
node records the choice; codegen only renders it:

- `rt.Equal(x, y)` / `!rt.Equal(x, y)` — everything whose Go
  representation is not *provably* comparable: `List`, `Go.Slice`,
  `Go.Map`, named Go types, channels, pointers, `GoError`, every
  nominal type (ADTs, records, aliases, abstract types), and tuples
  containing any of those. `rt.Equal` is `reflect.DeepEqual`.
- Go `==`/`!=` — the scalar builtins (`Int`, `Int32`, `Int64`,
  `Float`, `Bool`, `Char`, `String`, `Unit`) and tuples built
  entirely from them (`rt.TupleN` is a flat struct, so native `==`
  compiles and is structural).

Records and ADTs always take `rt.Equal` today even when every field
is scalar: knowing a record's fields takes the type oracle, and
interface values stay panic-prone under `==` regardless of their
payloads. Per-type generated comparators — the design.md 4.8 plan —
can narrow both later without changing observable semantics.

`reflect.DeepEqual` agrees with the checker-restricted domain:
functions and rigid type variables cannot be `==` operands (E0409),
so DeepEqual's nil-only rule for funcs is unreachable in practice;
on `Chans`/`Tasks` it compares identity exactly like `==` would.

## Interim `DeepEqual` warts

While `rt.Equal` is a DeepEqual wrapper, its answers diverge from
eval's `eqValue` at these edges — recorded so the per-type
comparators know what to fix:

- **nil vs empty `List`**: a nil `list.List` *is* the empty list
  per the representation contract, and `eqValue` compares
  element-wise, so eval answers `[] == nil-list` true — while
  `DeepEqual` keeps nil and non-nil-empty apart. Nil reaches
  compiled code through the Go boundary (`FromSlice`, `Go.Slice`,
  Go-constructed zero values); pure-Gom empties are always non-nil.
  `rt.Equal` therefore normalizes the *top level*: two empty slices
  of the same type compare equal. A nil list nested inside a
  record, ADT payload, or outer list still follows DeepEqual — a
  residual divergence the generated comparators will close.
- **`GoError`**: eval's `VGoErr` compares the message text
  (`a.Text == b.Text`); compiled `DeepEqual` compares the concrete
  error values, so two errors with an identical `.Error()` string
  but different constructions answer false. The interim keeps
  DeepEqual's stricter answer; the comparator design should pick
  message-text or value equality deliberately.
- **`Ref`**: `Ref a` has no value operations yet, so no reading is
  forced today. If the eventual representation is a pointer or
  cell, `DeepEqual` dereferences it — two distinct cells holding
  equal contents compare equal (content equality, the consistent
  reading of "structural"). Whether `==` on a Ref should be content
  or identity is left to the decision that designs Ref.

`rt.AssertEq` routes through `rt.Equal` rather than calling
`reflect.DeepEqual` itself, so `assert_eq` answers exactly what
`==` emits.

## Alternatives considered

- **Always emit `rt.Equal`**: simpler, but loses the readable
  `a == b` in generated code for the overwhelmingly common scalar
  case, and boxes every operand through `any`.
- **Per-type `Equal` functions now**: the design.md 4.8 end state —
  deferred; DeepEqual is the documented interim and this fix is a
  correctness patch, not the comparator generator.

## Verification

`docs/sketch/2026-11-04-structural-equality.md` — lowering unit
coverage of the type split, golden output for every shape, and an
end-to-end run where list/record/ADT equality answers correctly.
