# P1-13: P1 completion verification report

- 目的: verify P1 end to end — a small program using
  `import go "strconv"` must build through `gom build`, the
  generated Go must pass `go vet` and `go build`, and the binary
  must run with the expected output. Evaluator-vs-compiled
  comparison coverage must exist per
  [design.md](../design.md) section 7.
- 検証日: 2026-11-04, go1.27.1 linux/amd64, worktree `task/p1-13`.
- 結果: **P1 is complete enough to start V-1.** Verification
  exposed five real bugs; all are fixed with regression tests.

## Verification matrix

| check | where | result |
| --- | --- | --- |
| `import go "strconv"` program: `gom build` → scratch `go.mod` → `go vet` → `go build` → run → `42` | `cmd/gom/cli_test.go` `TestEndToEnd_Strconv` | PASS |
| plain program through the same real toolchain | `cmd/gom/cli_test.go` `TestEndToEnd_BuildAndRun` | PASS |
| evaluator vs compiled binary, `answer () -> String` compared | `internal/build/compare_test.go` `TestEvalMatchesCompiled`, 8 cases | PASS (8/8) |
| spec diagnostics suite `testdata/spec/` | `internal/build` `TestSpec` | PASS 9/9, 0 skips, 0 failures |
| golden suite `testdata/golden/` | `internal/build` `TestGolden` | PASS 1/1 (hello), 0 skips |
| dump suite `testdata/dump/` + phase dumps | `internal/treedump` `TestDumpCore`, `cmd/gom` `TestDumpPhases` | PASS, 0 skips |
| `go vet ./...`, `go test ./... -count=1`, `make check` (root + race + experiments + sumtype canary) | top level | PASS |

Skips observed: only `internal/gomtest`'s self-test fixtures that
deliberately exercise the harness's skip mechanism
(`TestRunDumpTestsSkip/later` and three manifest/discovery
siblings). The real spec, golden, and dump suites have **zero
skips and zero failures**.

Comparison-suite cases (`compare_test.go`) double as the P1
verification corpus: interpolation, currying, nested lambdas,
`|?>`/`try` escapes, bound curried values, fn-returning-fn, list
recursion, record update, match guards.

## Bugs found by end-to-end verification — all fixed

Unit tests passed before this task; these only surfaced by
building and running real programs.

1. **Validator did not chase bound metas** — `|?>` escaping
   through lambda boundaries produced a false E0901 ("signature
   return type is a nested `*types.Func`") because an inferred
   `?k -> ?r` hid the nested Func behind a bound meta in `Ret`.
   Fix: consistent `types.Chase` in `internal/core/validate.go`;
   `sigOf`/`flatSig` in `internal/lower/expr.go` chase and flatten
   hidden tails for `Func.Sig` and `flatArity`.
2. **Codegen panicked on `display`/`str_concat`** — string
   interpolation lowered to builtins codegen did not implement.
   Fix: `internal/codegen/expr.go` emits `+` chains and exact
   scalar rendering matching `internal/eval` (Bool→`FormatBool`,
   Int→`FormatInt`, Float→`FormatFloat 'g' -1 64`, Char→`string`,
   Unit→`"()"`, String→identity). Composites fall back to
   `fmt.Sprint` — see Filed gaps.
3. **`let x = 6 * 7` emitted `x := 6 * 7`** — `:=` defaults the
   untyped constant to Go `int`, but Gom `Int` is `int64`; later
   `strconv.FormatInt(x, 10)` failed to compile. Fix:
   `intLitConv`/`untypedConst` in `internal/codegen` wrap only
   fully-constant RHS in `int64(...)`/`int32(...)`.
4. **Partial-application result types were flattened** —
   `checkGomApply` built the residual with `types.Fn`, so
   `let g = add3 40` typed `g : Int -> Int -> Int` while the
   emitted value is `func(b) func(c)`. Applying the bound value
   (`g 2 0`) then generated the invalid call `g(2, 0)`. Fix:
   `applyResidual` in `internal/check/apply.go` rebuilds the
   residual from the callee's *nested* declared structure
   (`Int -> (Int -> Int)`), so `callFuncChunks` splits at the
   immediate arity; the Go `_`-placeholder residual in
   `checkGoApply` was fixed the same way, and `closureOf` in
   `internal/lower/expr.go` now types each Closure by its emitted
   shape (`params -> fn.ResidualType()`) rather than the flat sig.
5. (same root cause as 4, validator half) — inferred `fn`
   signatures are normalized through `sigOf` so `Func.Sig` stays
   canonical-flat even when `Ret` was a bound meta.

## Filed gaps

- `docs/problems/2026-11-04-interp-composite-display.md` —
  evaluator interpolation renders composites via
  evaluator-specific `String()`, generated code via
  `fmt.Sprint`/`%v`. Scalars are aligned (and covered by the
  comparison suite); composite divergence is a documented
  problem, not a silent mismatch.
- `docs/problems/2026-11-04-ignore-expression.md` (pre-existing,
  reconfirmed) — spec's `ignore e` form is not implemented; the
  working discard is `let _ = e`.

## TODO hygiene

- P1-1 … P1-14: all `[x]` except **P1-13, left `[ ]`
  deliberately** — the parent task checks it at merge.
- P2/P3 items remain `[ ]`; nothing was marked done prematurely.
- `## Bugs`: empty — every bug found here was fixed and carries
  regression coverage instead of a backlog entry.

## Recommendation

Start V-1 (executable tutorial). The `gom build` → `go vet` →
`go build` → run path is exercised by real e2e tests including
`import go`, and the evaluator/agreement suite gives V-1 a safety
net for the core language surface. The known composite-display
divergence and the `ignore` syntax gap are filed problems; neither
blocks writing honest tutorial programs.
