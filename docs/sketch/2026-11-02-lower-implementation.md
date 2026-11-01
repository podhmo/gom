# P1-7 lowering implementation — architecture and invariants

- Date: 2026-11-02
- Package: `internal/lower` (`lower.go`, `expr.go`, `pat.go`, `doc.go`)
- Status: implemented, review fixes applied; `go test ./...`, vet,
  gofmt clean; every test asserts zero diagnostics, and `lower.File`
  validates its own output, so green means `core.Validate`-clean.

## Architecture

`File(f *check.File, env *Env)` builds a `lowerer` — a one-file
lowering context:

- **subst** `[]map[*resolve.Symbol]*resolve.Symbol` — a stack of
  symbol-renaming maps. A reached arm's pattern binders are lowered
  to fresh temporaries inside the case body; the map routes the arm's
  body-local references to them. It is scoped per arm (`pushSubst`/
  `popSubst`), so an arm duplicated to several leaves (or-pattern
  alternatives, wildcard-expanded variants) stays well-scoped.
- **boundSyms + namedBind** — a checker symbol may appear as a
  Core binder only once file-wide. A body-local binder (`let`,
  destructuring, `for`/`select` elements, lambda params) inside a
  duplicated arm body would bind the same symbol at every leaf;
  `namedBind` detects the second occurrence (already bound once, or
  already substituted) and allocates a fresh symbol, registering it
  in the leaf's subst map so references follow.
- **fn stack** — the innermost enclosing `*core.Func` for
  `EscapeResult`/`EscapeCall`/`TailCall` targets. Function
  declarations, test bodies and `Closure`s push their `*core.Func`;
  `go {}` pushes *nothing* — an escape inside a spawned block still
  targets the enclosing function, and it is the validator's spawn
  counter that rejects a boundary-crossing escape (an earlier draft
  of this note claimed `go {}` pushed a nil boundary — wrong). A
  `try` or `|?>` lowered with *no* boundary at all (a top-level
  initializer) reports one diagnostic and degrades to the unescaped
  operand rather than emitting a target-less escape node that would
  earn a second validator violation.
- **tail flag** — set only while lowering the body tail of a
  function declaration whose body is `TailLoop`-eligible. A self
  `CallDirect` found in tail position (body tail, `If` arm tail,
  `Match` leaf tail, `Select` arm tail) is rewritten to `TailCall`
  by `markTail` *after* lowering, and the enclosing function is
  marked `TailLoop`. Nested `Closure` bodies are not traversed; a
  non-tail self call stays `CallDirect`.
- **arity / ctorT / typeD** — first-pass indexes: declared arity of
  same-file functions (P0-2 `FuncRef` vs flatten-closure decision),
  constructor → declaring `TypeDecl`, and local sum-type shape for
  wildcard expansion and payload metadata.
- **builtins** — interned runtime symbols (`str_concat`,
  `str_display`, equality/compare builtins) and synthesized
  constructor references (`Ok`/`Some` in generated patterns) created
  on demand — interned per `symKey{kind, name}` so a synthesized
  `KindCtor` and a `KindBuiltin` of the same name can never collide.
- **Env** — `GoSig` oracle for Go callee signatures,
  `FuncArity` for cross-file declared arities, `Types` for foreign
  ADT facts (variant lists for wildcard expansion).
- **diags** — contract gaps (inconsistent `CallInfo`, boundary-less
  escapes, unspreadable args, foreign-package `KindVar` references,
  unenumerable variant sets, unknown record-field indexes, nil
  select binds) are `diag.PhaseLower` diagnostics (`E0901`,
  provisional code — see
  `docs/problems/2026-11-02-lower-input-contract-gaps.md`), never
  panics. Where the gap leaves a buildable tree the lowering emits a
  well-formed fallback (a `FieldGet` clamped to index 0, the
  unescaped operand, `typeName "<unknown>"`) so the diagnostic is
  the only evidence — not a pile of secondary violations.

## Desugaring order and ANF

Every expression lowers into an explicit `*seq` — the ordered
`[]core.Let` being accumulated for the current `core.Body`. `atom`
/`asAtom` forces compound operands into a fresh bound `Var` *in
source order*, so:

- arguments evaluate in written order even when `ArgToParam`
  reorders them into declaration order;
- record fields, tuple/list elements, select channel operands and
  send values each bind once, left-to-right;
- `let`-bound pattern RHSs evaluate exactly once;
- `if`/`match`/`select` conditions and subjects are bound before the
  node; guarded arms keep the guard's own lets inside `DGuard` —
  continuation work never leaks into the unconditional sequence
  (`continueWith` re-nests it under the case/leaf).

`Body.Tail` carries the value-producing tail; `Let.X` may be any
complex node (including `If`, `Match`, `ForRange`, `Spawn`) but
never a bare multi-result `CallGo` — those are wrapped at
construction by `ResultFromGo`/`OptionFromGo`/`TupleFromGo` or fused
into `EscapeCall`. Bodies are assembled strictly after lowering the
tail (`tail := l.expr(s, e); &core.Body{Lets: s.lets, Tail: tail}`):
building the tail inside a composite literal would read `s.lets`
before the lowering that fills it — the same ordering bug `core`
itself disallows elsewhere.

## CallInfo mapping

`finishCall` dispatches strictly on `ci.Kind` — no name or shape
inspection:

| `ci.Kind` | Core |
| --- | --- |
| `CallDirect`, `Applied == Arity` | `CallDirect` (ctor callee → `CtorApp`) |
| `CallDirect`, `Applied > Arity` | saturated `CallDirect` bound to a temp, remainder applied in flat-arity `CallFunc` chunks — a remainder shorter than the returned function's arity becomes a `Closure`, never a partial `CallFunc` |
| `CallPartial` | `Closure` over the unapplied params; body is the saturated call |
| `CallUnknown` | `CallFunc` consumed in flat-arity chunks of the *value's* type |
| `CallGo` | `CallGo` + result consumer; `_` hole → cute `Closure` |

`Var`/`MemberExpr` in value position: `FuncRef` only when declared
arity covers the flat arrow chain, else the P0-2 flatten `Closure`;
Go funcs → `GoFuncRef` for the unadapted 1→1 shape, else an adapter
`Closure`. `CallUnknown` on a `FuncRef`/`CtorRef` is a contract-gap
diagnostic; a `KindVar` reference carrying a foreign `Package` is
one too (cross-package lowering is out of scope).

`CallGo.Spread` reflects only the written `..xs` marker
(`hasSpread(ap.Args)`), never the callee's `Variadic` flag alone —
`f(a, b)` and `f(..xs)` are different calls. The cute `_` hole is
filled in *written* argument position (`ci.HoleIndex`) before
`declOrderArgs` reorders, so labeled arguments cannot drag the hole
to the wrong parameter slot.

Pipes desugar before calling: `x |> f a…` copies `CallInfo`, appends
`x` at `lowestUnfilled` (`ArgToParam` extended), promotes
`Partial`→`Direct` on saturation, then `finishCall`. `<|` mirrors it
with left-to-right evaluation preserved. `f >> g` / `<<` become
closures; operand evaluation is always source order, so `g << f`
evaluates `g` before `f` even though `f` applies first inside the
closure.

## Decision trees (pat.go)

`compile` builds a `DTree` over the arm list:

- structural prefixes (ctor, literal, list heads, tuples/records
  under them, `as`, or-pats) select a discriminating position;
- ctor pats → `DSwitch{SwitchType, DCtorTest}` with the subject
  narrowed into the case bind; wildcard/catch-all arms **expand to
  the remaining variants** when the type's variant list is known, so
  `Default` is always `DPanic` (invalid foreign values panic, never
  silently match);
- literal pats → `SwitchValue`/`DLitTest`; list pats → `SwitchList`/
  `DListTest{Cons}` with head/tail binds, default `DPanic`;
- or-pats splice alternatives in place, preserving source order and
  each alternative's bindings;
- guards become `DGuard` — else-side is the continuation tree of the
  remaining arms;
- binder projection: inside a reached case, each pattern var binds
  via `let` = `FieldGet`/`Var` of the narrowed subject — all pure,
  evaluated on that path only.

Two invariants the review pass made explicit:

- **Every survivor state is a clone.** `compile` discharges a
  state's structural prefix destructively (`todo` pops, `binds`
  append), so `statesForCtor`/`statesForLit`/`statesForLitDefault`/
  `statesForList`/`statesForCons` must return `w.clone()` for
  cannot-fail survivors and exhausted states — sharing the pointer
  let one case consume obligations the next case still needed
  (`match (s, n) { (_, 5) -> … }` lost its literal test under the
  second surviving variant).
- **`[..rest]` never fails.** A list pattern with no element
  patterns binds the whole subject on *both* outcomes: it survives
  the nil case binding `rest` to the subject, and survives the cons
  case without ever indexing `Elems[0]` of an empty list.

Lesser points in the same file: literal cases dedup by
`(kind, text)` — defence-in-depth, since checker-side exhaustiveness
already rejects duplicate literal arms; `DPanic` inside a nested
continuation reports the *next discriminated subject's* type
(`nextSubj`) rather than the enclosing switch's; a destructure over
an or-pattern packs the bound tuple in the pattern's *declared*
binder order (`boundVars`), not the matched path's order — `let
(A x y | B y x) = v` would otherwise swap the payload values under
B. The `Qual == ""` builtin-name assumptions (`Result` for tag
switches, `Chan`/`RecvChan` for `for` kind) are contract gaps filed
in `docs/problems/2026-11-02-lower-input-contract-gaps.md`, not
redesigned here.

Destructuring (`let` pats, fn params, `for` pats) reuses the same
machinery: *deep-pure* patterns (only var/wild/tuple/record/`as`
under pure) lower to direct projections — top-level `let (a, b) = p`
emits one hidden `VarDecl` plus a `VarDecl` per bound var whose body
is a pure `FieldGet`/`Var` walk. Any refutable pattern compiles a
`Match` over a fresh temporary; the bound names project from the
match's result (single var → value; several → tuple temp). This
fixes the earlier `destructureBinds` design that projected every
pattern as `FieldGet` and silently dropped ctor/list/literal
patterns.

## Escape lowering

`try e` → `EscapeResult` on the bound operand, or fused
`EscapeCall` when `e` is a direct Go `(T, error)` call. `x |?> f` →
inflow `EscapeResult{Wrap:""}` (or `EscapeCall` on a direct call),
step application, step `EscapeResult{Wrap: stepName}` /
`EscapeCall{Wrap: stepName}`, `Ok` rewrap. Targets are always the
innermost function boundary; with no boundary at all (`try`/`|?>`
in a top-level initializer) lowering reports one diagnostic and
degrades to the unescaped operand, and escapes crossing `go {}` are
left for the validator's spawn check rather than hidden by a nil
boundary entry.

## Validator-clean guarantee

`File` calls `core.Validate` on the produced file and appends any
`Violation` as `phase: lower` diagnostics — a malformed tree can
never be returned silently. The test suite drives this: `mustLower`
fails on any diagnostic, so every fixture proves both lowering and
validator agreement (ANF, scope, once-bound binders, escape targets,
`DPanic` defaults, `TailCall` positioning, `FuncRef` arity).

## Coverage notes / hand-off

- Covered by tests: eval order, ANF lets, labeled reorder,
  direct/over/partial/unknown/Go calls, cute `_` (incl. the labeled
  reorder shape), all Go result shapes, ctor/lit/list switches,
  wildcard expansion, guards, or-pats, `try`, `|?>`, tail calls
  (+negative), select ordering, for/go, flatten refs, str interp,
  compose (both directions, incl. `<<` operand order), top-level
  tuple/ctor destructuring — plus the review regressions: per-case
  survivor isolation, duplicated arm-body binders (wildcard and
  or-pattern), non-leading and standalone `[..rest]`, variadic
  `try` with and without `..`, over-applied remainders landing on a
  partial return, or-pattern destructure binder order, and the
  contract-gap diagnostics (non-nominal ctor subject, foreign
  variable, unknown record field, nil select bind).
- For P1-8: `Env.GoSig`/`FuncArity`/`Types` oracles must be fed by
  the driver for cross-file/interop correctness; the comma-ok flag
  is not derivable from types alone (filed).
- Known limits: `record update` spreads, `Spread` args in
  non-variadic calls, and exotic `check` node kinds diagnose rather
  than guess.
