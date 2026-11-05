# Sketch: width-aware integer ops in the evaluator — verification log

Verification report for
[decision: eval-int-width-aware-ops](../decisions/2026-11-05-eval-int-width-aware-ops.md)
— resolving the registered divergence where the evaluator computed
every integer operation in int64 while spec 3.3 (and generated code)
wraps `Int32` at 32 bits
([ledger](../implementation-gaps.md), TODO.md Bugs).

## Mechanism

`internal/eval` keeps `VInt` as `int64` but makes it *canonical*: a
VInt produced for an `Int32`-typed node always holds a sign-extended
int32 value. Width is read from the typed AST at the op sites:

- `evalPrim` (arith + ordered comparisons): `x.X.Type()` gives the
  unified operand type (`checkArith` unifies both operands). Operands
  normalize through `wrapTo(w, n)` and results wrap back — normalized
  operands compare identically to int32 operands, and
  `MinInt32 / -1` runs as int64 `2147483648` then wraps to
  `-2147483648`, the exact Go int32 answer.
- `evalUnary` (`-`): result wraps to the operand width.
- `litValue` (`check.Lit` + `check.LitPat`): an `Int32`-typed literal
  materializes as `int64(int32(text))`; magnitudes beyond int64 parse
  through `uint64` so `-9223372036854775808` produces MinInt64 — the
  evaluator previously reported an internal "invalid int literal"
  where the binary answers MinInt64.

`==`/`!=`/`assert_eq`/display need no width information: canonical
values compare and render correctly as int64. A file-local
`type Int32` ADT is excluded from the width read by the `localT`
guard (its Con spells the same name but is not an integer type — the
checker's `num` constraint already rejects it, the guard only keeps
eval's defensiveness consistent).

`Char` is not numeric (the `num` constraint reports E0401 on
`c + 1`), so no Char arithmetic exists to size. `Int32` ↔ `Int`
mixing is rejected by unification, so operand widths never disagree
inside one node.

## Evidence

Eval-vs-compiled agreement, byte-for-byte, on the boundary cases
(scratch run of both sides before the harness cases existed):

```
${f 2147483647}/${f (-2147483648)}/${a + 1}/${m}/${m - 1}/${a * 2}
 /${(a + 1) > a}/${(a + 1) == m}/${-m}/${m / neg1}/${m % neg1}

eval:   -2147483648/-2147483647/-2147483648/-2147483648/2147483647/-2
        /false/true/-2147483648/-2147483648/0
binary: (identical)

${m}/${m / neg1}/${m % neg1}/${-m}   -- Int64 min spellings
eval:   -9223372036854775808/-9223372036854775808/0/-9223372036854775808
binary: (identical)
```

Before the change eval answered `2147483648` for `f 2147483647`,
`2147483648` for `m / neg1`, `2147483648` for `-m`, `true` for
`(a + 1) > a`, and E9004 for `let m : Int64 = -9223372036854775808`.

Regression surface:

- `internal/build/compare_test.go` gained `int32_width_arith` (all
  ops at/around the boundary, plus width surviving a let binding, a
  function alias, and partial application) and `int64_min_literal`
  (the MinInt64 spellings — literal materialization, `/`, `%`, `-`).
- `internal/eval/eval_test.go` gained Int32 cases in
  `TestLiterals`/`TestArithmeticAndComparison` (wrap edges, MinInt32
  `/ -1` and `% -1`, `-MinInt32`, a wrapped result feeding `>` and
  `==`, the 2^63-magnitude literal).
- `go test ./...` is clean; no generated output changed (the whole
  change is eval-internal).

## Adjacent finding (recorded, out of scope)

check performs no integer-literal range check, and the emitted Go
cannot express an out-of-range literal in a typed context:
`let n : Int32 = 3000000000` is accepted but `var n int32 =
3000000000` does not build, and the fold-avoidance turns
`let k : Int32 = 0 - 2147483648` into `tmp - 2147483648` which also
fails `go build` (while Go itself accepts the folded spelling).
Spec is silent on literal ranges — recorded as
[problem](../problems/2026-11-05-int-literal-range-check.md).

Verified with `go test ./internal/build` (`TestEvalMatchesCompiled`),
`go test ./internal/eval`, and `go test ./...` on the worktree.
