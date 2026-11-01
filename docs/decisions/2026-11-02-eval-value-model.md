# Value model of the typed-AST evaluator (internal/eval)

- 状態: draft
- 日付: 2026-11-02
- 検証: [p1-12-evaluator](../sketch/2026-11-02-p1-12-evaluator.md)

## Background

P1-12 implements `internal/eval`, a tree-walking evaluator over the
typed AST (internal/check) so that interpreted results can be compared
against generated Go (design.md section 7). The evaluator is an
independent runtime: it does not share the value representation chosen
for generated code (decision
[value-representation](2026-11-01-value-representation.md)). What has to
agree is observable behavior — evaluation order, pattern dispatch,
currying, and error/escape behavior — and a deterministic rendering of
results for comparison.

## Decisions

- Values are immutable, structurally-comparable Go structs behind a
  sealed `Value` interface: `VUnit`, `VBool`, `VInt`, `VFloat`,
  `VChar`, `VStr`, `VTuple`, `VList`, `VRecord`, `VVariant`, `VGoErr`,
  `VFunc`. Sized integer literals share the 64-bit `VInt`.
- `Result a e` is `VVariant` like every other ADT; `Ok`/`Err` is
  detected through the variant's nominal type (`Con{Name: "Result"}`,
  unqualified) or, for hand-built values with a nil `Type`, by
  constructor name.
- `VFunc` carries the declared signature plus a slot per parameter;
  partial application records bound slots, `Remaining()` lists the
  unfilled declared indices in declaration order, and
  `check.CallInfo.ArgToParam` positions are trusted as-is. `|>`/`<|`,
  `>>`/`<<`, over-application, and `_`-placeholder Go calls are all
  expressed as applications over this machinery.
- `VGoErr` models `GoError` values; a `|?>` wrap keeps the original
  error in `Wrapped` so the causal chain shape of `fmt.Errorf("%w")`
  is preserved in the value (spec section 9.2 item 3).
- `Value.String()` is the comparison format: `42`, `3.14`, `true`,
  `"s"` (Go-quoted), `'c'`, `()`, `(a, b)`, `[1, 2]`,
  `{ name = v, ... }` in declaration order when the record type is
  known, `Ctor v1 ~name: v2`, `error("text")`, `<fn name/remaining>`.
  Composite values are parenthesized in operand position so
  `Add (Num 1) (Num 2)` stays unambiguous. The generated-Go side must
  emit the same format for comparisons (a `gom fmt`-style rendering
  will need to be specified there separately).
- Runtime failures are `diag.Diagnostic`, never Go panics: `fail`
  panics internally with a `failure` value recovered at the public API
  boundary into `Result.Diags`. `try` uses a second panic value
  (`escape`) caught at the innermost function boundary (`runBody`).
- Diagnostic codes live in the reserved-but-unregistered E90xx range
  (errors.go): E9001 unsupported, E9002 match failure, E9003 Gom
  panic, E9004 invariant violation, E9005 test failure. Registering
  them in `internal/diag/codes.go` is follow-up work — this task may
  not edit other packages.
- `Env` maps `*resolve.Symbol` to `Value` with a parent chain;
  constructor and record layouts hang off a shared per-evaluator `ctx`
  so standalone `Expr` calls can derive constructors lazily from the
  Var's type when no `TypeDecl` exists.
- Go interop is stub-based: `NewHostFunc` produces `VFunc`s a harness
  binds under the Go symbol (or a module member's symbol), which is
  what makes `CallGo` — including cute `_` partial calls — evaluable
  at all. Unstubbed Go calls report E9001.

## Open ends

- The E90xx codes are evaluator-local until registered in
  internal/diag/codes.go.
- `|?>` step names for nameless right-hand sides fall back to
  `file:byteOffset`, not the spec's `file:line` (see
  [problem](../problems/2026-11-02-eval-pipe-try-step-name.md)).

## Note (review fix)

The first implementation wrapped the *incoming* `Err` with the next
step's name and evaluated the step expression before checking the
tag. Review against spec section 9.2 corrected this: an `Err` input
passes through untouched (the step is not evaluated — the
evaluation-order decision), and the wrap happens only at the step that
*produced* the `Err`, keyed on the pipe node's own `Result _ GoError`
type so a non-`Result` first value (`5 |?> f`) still wraps.
