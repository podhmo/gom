# Sketch: generated equality comparators — verification log

Verification report for
[decisions/2026-11-05-generated-equality-comparators.md](../decisions/2026-11-05-generated-equality-comparators.md):
the `core.Equal` + `File.EqDecls` pipeline replaces the `rt.Equal`
everywhere route of
[2026-11-04-structural-equality](../decisions/2026-11-04-structural-equality-emission.md).

## Generated shape (testdata/golden/builtins.golden.go)

| Gom `==` operands | plan | emitted |
| --- | --- | --- |
| `List Int` | `EqCall` → `EqSlice(native)` | `gomEq1(xs, ys)` — `len` + elementwise `==` |
| `(Int, List Int)` | `EqCall` → `EqTuple(native, call)` | `gomEq2` — `a.V0 == b.V0 && gomEq1(a.V1, b.V1)` |
| record `Pair` | `EqCall` → `EqRecord(...)` | `gomEq3` — fieldwise |
| ADT `Shape` (payload `List Int`) | `EqCall` → `EqSum` | `gomEq4` — nil check + variant type switch |
| `Int`, `(Int, String)` | `EqNative` | `a == b` |
| `xs != ys` | `Neg` | `!gomEq1(xs, ys)` |

One `gomEq<N>` per distinct composite operand type, deduplicated by
type key, emitted after the file's own declarations — including when
only test code reaches for them (the main/test split shares the
symbol table).

## The NaN triple — eval ≡ compiled

`internal/build/compare_test.go` case `structural_equality_nan`:

```gom
let z = 0.0
let n = z / z     -- NaN
let xs = [n]
n == n            -- false  (IEEE)
xs == xs          -- false  (elementwise; was TRUE under DeepEqual's
                  --        same-backing-store shortcut)
[n] == [n]        -- false
xs == [n]         -- false
```

Both the evaluator (`eqValue`) and the compiled binary answer
`false,false,false,false`. The middle case is the actual bug:
`rt.Equal`/`reflect.DeepEqual` returned true for `xs == xs`.

## Generated test file — `rt.AssertEq(t, eq, actual, expected)`

`internal/build/compare_test.go` `TestGeneratedTestFileRuns`
compiles a Tests-mode file, drops `m.go` + `m_test.go` into a
scratch module and runs `go test` — passing `assert_eq xs xs` on a
`List Float` exercises the precomputed-verdict signature end to end.

## The E0409 judgment matrix (testdata/spec)

| fixture | rule exercised |
| --- | --- |
| `E0409_eq_fn` | bare `Int -> Int` operand |
| `E0409_eq_fn_record` | named record hiding a function field |
| `E0409_eq_fn_adt` | ADT payload `(Int -> Int)` |
| `E0409_eq_fn_generic` | `Box (Int -> Int)` — type-arg substitution into `Box a = {v: a}` |
| `E0409_eq_fn_nested` | `List Box` — container arg → named expansion |
| `E0409_eq_fn_mutual` | `A{b:B} / B{a:Option A, f:fn}` — cycle visit + sibling field |
| `E0409_eq_deferred_meta` | `xs == ys` at `List ?n`; `?n := Int -> Int` re-checks at bind |
| `E0409_assert_eq_fn` | `assert_eq` shares the same judgment inside `test` |
| `ok_eq_named` | records, `Node (List Tree)` recursion, `Box Int`, alias, mutual records via `Option` — all legal |

## Deliberate residuals

- `rt.Equal` remains the recipe for named Go types, `Task`,
  `Go.Any` — its DeepEqual reading is documented on the function as
  an approximation for representations Gom does not own.
- `Ref` compares by identity (`EqNative`); content-vs-identity is
  the Ref-design decision's problem.
- `nil` GoError vs non-nil is nil-aware; two errors compare by
  `Error()` text (eval `VGoErr` parity).
