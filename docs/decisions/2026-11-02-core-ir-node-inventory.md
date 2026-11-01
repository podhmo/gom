# Core IR node inventory and validator

- 状態: draft
- 日付: 2026-11-02

## Background

Task P1-2b defines the Core IR — the representation `internal/lower`
produces and `internal/codegen` consumes — and the validator that
enforces its contract. The invariants themselves were decided in
[core-ir-invariants](2026-11-01-core-ir-invariants.md) (design.md
section 2.1): evaluation order, call arity and convention, Go-interop
conversions, match decision trees, escape nodes, type concretization
and source provenance are all fixed before codegen. This file records
the *node-level* choices made while implementing that contract in
`internal/core`: which nodes exist, which distinctions they pin down,
and what the validator can and cannot check mechanically.

Verification: unit tests in `internal/core` (`core_test.go`,
`validate_test.go`) construct a valid file covering every node family
and hand-build one malformed file per invariant; see
[the sketch report](../sketch/2026-11-02-core-ir-validator-verification.md).

## Representation: one `internal/core` package

The Core IR is a new sealed node hierarchy in `internal/core`
(`node.go`, `expr.go`, `call.go`, `tree.go`, `decl.go`), in the
position design.md section 3 gives it between `check`/`pattern` and
`lower`/`codegen`. Like `check`, every node physically embeds an
exported `Base{Sp diag.Span, Typ types.Type}` so "every node carries
its span and type" is structural. `Base.BaseOf()` is the uniform
accessor the validator uses across the sealed interfaces, mirroring
`check.Base`.

## Evaluation order: `Body` of ordered `Let`s, atoms as operands

- `Body{Lets []*Let, Tail Expr}` is the unit of order. A `Let` is one
  ordered binding `name = x`; `Tail` is the body's result.
- `Atom` is a sealed sub-interface of `Expr`: `Var`, `Lit`, `FuncRef`,
  `CtorRef`, `GoFuncRef`, `FieldGet`. Every operand position of a
  complex expression is typed `Atom`, so a nested complex expression
  *cannot be represented* — ANF shape is enforced by the type system,
  and the validator only has to check scoping, freshness and ordering.
- `Bind{Sp, Sym, Typ, Discard}` is the single binder descriptor used
  by lets, params, case binds, receive arms and loop elements;
  `Discard` is the `_` binder.

## Calls: the four conventions are different nodes

Call classification is a node-type distinction, not a flag — a
`CallFunc` cannot silently contain a known callee.

- `CallDirect` — saturated call to a known Gom `KindFunc` symbol;
  `len(Args) == Arity`, args in declaration order (label reordering
  done at lowering).
- `CtorApp` — saturated variant construction, same shape as
  `CallDirect` with a `KindCtor` callee.
- `CallFunc` — applying a function *value* at its full remaining
  arity (`Fn` is an atom, never `FuncRef`/`CtorRef`). No curried
  intermediate calls exist (P0-2 arity-aware representation).
- `CallGo` — Go calling convention: flat `Args`, `Arity`, `Variadic`,
  `Spread`, and `Results` classifying the Go result shape
  (`one` / `err` / `err-only` / `ok` / `pair`).
- `CallBuiltin` — compiler builtins emitted from a codegen table.
- **Partial application is not a call node.** A `f x` that binds fewer
  than `Arity` arguments is an explicit `Closure{Fn *Func}` whose
  body is the saturated `CallDirect` over bound atoms and the
  closure's parameters. The same `Closure` node covers cute `_`
  placeholder calls, flatten wrappers for function-returning
  functions, compositions, and Go-function adapters — they are all
  just function literals at this level.
- `FuncRef` (Gom), `CtorRef`, `GoFuncRef` are the "function used as a
  value" atoms. `FuncRef.Arity` records the declared arity so the
  validator can enforce P0-2 rule 3 (a direct reference only when the
  declared arity covers the type's arrow chain). `GoFuncRef` is
  direct only for `Arity == 1 && Results == one` — anything else must
  arrive as an adapter `Closure`.
- `TailCall` — a self-tail call to the enclosing `FuncDecl`'s `Func`
  (`To`), legal only in tail position of a `Func` marked `TailLoop`
  (design.md 4.5).

## Interop: consumer nodes + `Conv`

A `CallGo` with `Results != one` produces a Go multi-value and may
appear **only** as the `Call` operand of an explicit consumer node:

- `EscapeCall` — the fused `(T, error)` → early-return form used by
  `try` / `|?>`.
- `ResultFromGo` — materializes `Result T GoError` from
  `(T, error)` / `error` calls that are stored or returned.
- `OptionFromGo` — `Option T` from a comma-ok call.
- `TupleFromGo` — `(A, B)` from comma-ok or other pair calls.

All other boundary adjustments are `Conv{Kind, X}` with
`ConvKind` = `num | named | box-any | iface | slice-to-go |
slice-from-go`. Nothing converts implicitly at codegen.

## Escapes: `To *Func` is the named boundary

`EscapeResult` (`try` / `|?>` on a Result value), `EscapeCall` (fused
`(T, error)` variant) and `TailCall` all carry `To *Func` — the
*innermost enclosing function*. `Func` is shared by `FuncDecl`,
`TestDecl` and `Closure`, which makes "test blocks are function
boundaries" and "closures are boundaries" structural. `EscapeResult.Wrap`
records the `|?>` step name for the `fmt.Errorf("step: %w")` wrap;
the validator checks it is only used on `GoError` steps. `Spawn`
(`go {}`) is not a boundary — the validator tracks it separately and
rejects escapes and tail calls crossing it (a `Func` *is* a boundary,
so a `try` inside a lambda nested in `go {}` escapes through the
lambda — the `go{}`-depth resets at every `Func`).

The boundary legality of an escape is judged on the *residual type*
— `Func.ResidualType()`: `Sig` is the canonical flat arrow chain
while `Params` is the declared arity, so for an arity-split function
like `fn mk_add (a: Int) -> (Int -> Int)` the emitted result is the
remaining arrow `Sig.Params[len(Params):] -> Sig.Ret`, not `Sig.Ret`.
A `try` in the outer body of such a function is rejected because its
residual is a function type, while the innermost full-arity `Func`
still sees `Result _ e`. The same residual check applies to the
body-type agreement (`len(Params) <= len(Sig.Params)` always;
exceeding it is malformed).

## Decision trees: `DTree` sealed family

`Match{Tree DTree}` is the only match node — no patterns remain.

- `DSwitch{Subj Atom, Kind, Cases []DCase, Default DTree}` with
  `SwitchKind` = `type` (sealed-interface type switch, P0-1), `tag`
  (`subj.Tag()` on the flat2 Result, P0-4), `value` (literal switch),
  `list` (empty/non-empty slice test, P0-3).
- `DTest` = `DCtorTest` (variant for type/tag switches) / `DLitTest`
  (literal for value switches) / `DListTest` (cons/nil for list
  switches). The test kind must match the switch kind.
- `DLeaf` (arm body), `DGuard` (guard continuation), and `DPanic`
  (the invalid-Go-side-value arm — `rt.InvalidVariant`). For
  `SwitchType`/`SwitchTag` the `Default` **must** be a `DPanic`:
  wildcard arms are expanded into variant cases at lowering so a nil
  or corrupt value can never silently match. `SwitchList` keeps the
  panic filler even though nil is a valid empty list — a uniform
  rule.

## Validator scope: `Validate(f *File) []Violation`

The validator checks the mechanically enforceable subset of the
contract — the things a wrong `lower` could produce that codegen must
not see:

- **ANF/scope**: binder freshness file-wide, `Var` in scope and
  correctly ordered (use-before-binding rejected by construction),
  binder type vs bound-expression type agreement.
- **Spans and types**: every node has a valid span and non-nil type;
  node-type vs signature/body/branch/payload consistency where
  checkable.
- **Call classification**: saturation of `CallDirect`/`CtorApp`,
  `CallFunc` arity and non-known callee, `CallGo` arity/variadic/
  spread/result-shape agreement, `FuncRef`/`GoFuncRef` direct-use
  rules.
- **Interop**: multi-result `CallGo` only inside its consumer nodes;
  `Conv` kind vs operand/result type shapes for the slice directions.
- **Decision trees**: kind/test agreement, bind counts, `DPanic`
  defaults for type/tag switches, guard `Bool` conditions.
- **Boundaries**: `To` is the innermost enclosing `Func`, the
  boundary result is `Result _ e` with compatible error type,
  `Wrap` only on `GoError`, no escape or tail call across `go {}`,
  `TailCall` only in tail position of a `TailLoop` `FuncDecl`.
- **Control-flow shapes**: `if` condition `Bool`, else-less `if`
  `Unit`, `select` arm-type agreement and at most one default,
  receive binds `Option`-typed, `for` kind vs iterable type, `go {}`
  bodies `Unit`, sugared binary operators absent.

**Not checked mechanically**: case exhaustiveness against the type
declaration (the variant set lives in `TypeDecl`, not on the tree),
argument *types* at call sites (settled at check time; the symbols
carry no schemes — the symbol→scheme table is a P1-5 concern), and
`Conv` legality for `num`/`named`/`box-any`/`iface` (Go conversion
and assignability rules already settled upstream). These are noted on
`Validate`'s doc comment.

Violations are `Violation{Span, Node, Msg}` errors, not
`diag.Diagnostic`: they report compiler bugs in `lower` or test
fixtures, not user errors, and the diagnostic code table has no
internal-invariant category. If that changes, `Violation` carries a
span already and can be lifted.

## Consequences / open points

- `internal/lower` (P1-7) must produce this exact shape from
  `check.File`: `check.Apply.CallInfo` maps to `CallDirect` /
  `CallFunc` / `CallGo` / `Closure` per `Kind`; `pattern`'s decision
  output maps to `DSwitch` trees; escapes need the enclosing `Func`
  pointer, which is natural when lowering is inside-out.
- `internal/lower` also fills `Func.Sig` (canonical flat form) and
  `Func.Params` (declared arity) separately — the residual-type rule
  above is what codegen relies on for the emitted function's result
  type, so no node stores it explicitly.
- `internal/codegen` (P1-8) reads node types directly: no symbol-name
  or AST-shape inspection for call semantics, conversions, or escape
  boundaries.
- The `ArgToParam`/`HoleIndex`/`Unapplied` fields of `check.CallInfo`
  have no Core counterpart — reordering and holes are fully compiled
  away by lowering. If a need resurfaces, it means lowering did not
  finish.
- Symbol→type-scheme table remains a P1-5 open point: `FuncRef.TypeArgs`
  and `Closure` instantiation sites assume it exists.
