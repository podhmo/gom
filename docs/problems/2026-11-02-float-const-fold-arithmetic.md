# Float literal arithmetic constant-folds in generated Go but evaluates per-operation in the evaluator

- 状態: 解消（[decision](../decisions/2026-11-05-numeric-arithmetic-per-operation.md) — 数値演算は演算ごとの機械演算と決定。lower がリテラルのみの被演算子を変数経由にして生成 Go の定数式を回避する。検証は [sketch](../sketch/2026-11-05-numeric-arithmetic-per-operation.md)、knownDivergences の 2 件は退役）
- 日付: 2026-11-02

Found by the V-3 eval-vs-compiled differential test
(`internal/build/diff_test.go`, deterministic gomgen seed 52).
Registered in `knownDivergences`.

## What happens

```gom
fn answer () -> String = "${869.32 * 38.29}"
```

- The evaluator parses each literal to `float64`, then multiplies:
  `float64(869.32) * float64(38.29)` — two independent roundings, so
  the product is the `float64` nearest the *rounded* operands'
  product: `33286.262800000004`.
- Codegen emits the literal text verbatim into the generated Go
  (`strconv.FormatFloat(869.32 * 38.29, 'g', -1, 64)`). Go's untyped
  constant arithmetic is arbitrary precision: `869.32 * 38.29` is
  computed exactly and rounded **once** when converted to
  `float64` — a *different* `float64`, displayed as `33286.2628`.

Both sides then apply the same `'g' -1` display and produce
different observable output. The divergence is not in the display
function — it is in the value.

## Why this needs a spec answer, not just a fix

Float semantics is a language decision:

- **IEEE-754 per operation** (what the evaluator does today, and
  what most languages with runtime `f64` arithmetic do): every
  literal rounds to `float64`, every operator rounds to `float64`.
  Generated Go must then defeat constant folding — e.g. lower each
  arithmetic node to operate on non-constant operands (store
  literals into variables, or wrap so the operand is not a constant
  expression). Typed conversions like `float64(869.32) *
  float64(38.29)` are NOT sufficient: Go still evaluates typed
  constant expressions exactly.
- **Exact-then-round** (what generated Go does today, matching Go's
  constant semantics): a literal-only expression computes exactly
  and rounds once at its boundary. The evaluator would then have to
  fold constant subtrees the same way — doable (the typed AST marks
  literal subexpressions), but it makes "the same expression" mean
  different things depending on whether a literal or a variable
  holds the operand.

Note this only diverges on literal-only arithmetic: once a variable
enters, Go does runtime `float64` ops and both sides agree. The
generated programs that hit it are the ones doing arithmetic on two
literals — rare in handwritten code, constant in generator output.

## Interaction with the harness

The V-3 deterministic subset keeps Float in the splice/pool type set
because suppressing it would hide exactly this class of finding. The
failure signature is registered in `knownDivergences`; when the spec
question resolves, the entry retires and the repro becomes a
regression case.

Related: `docs/problems/2026-11-04-interp-composite-display.md`
covers a different interpolation gap (composite display), which is
why deterministic *mechanical*-mode splices are suppressed while
typed-mode scalar splices stay.
