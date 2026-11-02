# Builtin type identity is decided by declaration, not by name

- 状態: 採用
- 日付: 2026-11-02

## Context

`types.Con{Qual: "", Name: "Result"}` cannot spell the difference
between the builtin `Result a e` and a file-local `type Result`
declared in the file being compiled — the same holds for `Option`,
`List`, `Chan`, `Int`, `Unit`, and every other unqualified builtin
name. Compiler phases that tested builtin-ness by name alone made a
file-local `type Result = | Ok Int | Err String` take builtin
paths: `match` lowered to the flat `rt.Result` representation
(`r.Tag()`, `r.Value`), `try`/`|?>` accepted it as an escape
operand, and the generated Go could not compile because a local ADT
is a sealed interface carrying only `isResult()`.
[docs/problems/2026-11-02-local-result-vs-builtin-escape.md](../problems/2026-11-02-local-result-vs-builtin-escape.md)
records the bug and its escape-boundary twin.

Two candidates were on the table: give builtin types an identity
that survives shadowing (a reserved `Qual`, or a symbol
back-reference on `types.Con`), or reject local declarations that
collide with builtin names.

## Decision

**Declaration-aware builtin identity.** A builtin type is "the
builtin" only when the file does not declare a `type` under the
same unqualified name. Every phase that asks "is this `Con` the
builtin X?" consults the phase's own declaration index before the
name — never the name alone:

- **check**: `checker.tyCons` entries carry `typeInfo.sym`, non-nil
  for file-local declarations. `shadowedBuiltin(name)` answers
  "the file declares `type name`"; `isBuiltinCon(t, name)` is the
  name test plus `!shadowedBuiltin`. `builtinTypeArity` gates the
  check so only known builtin names can be shadowed at all.
- **lower**: `lowerer.typeD` indexes the file's `check.TypeDecl`s;
  `declaredT(c)`/`builtinName(c, name)`/`isResultCon`/
  `isOptionCon`/`resultArgs` are the builtin-shape tests. The
  equality-plan walk (`eqKindOf`/`eqDeclPlan`) and `variantsOf`
  consult `typeD` before the builtin tables — local declarations
  first, builtin shapes second, the foreign oracle last.
- **core validator**: `validator.localT` collects the names of the
  file's `core.TypeDecl`s; `conIs`/`resultType` take the file's
  declaration set, so hand-built Core cannot smuggle a local Result
  into the escape rules either.
- **codegen**: the existing `g.localT` guards keep a local
  `Result`/`Option`/`List`/`Unit` off the `rt.Result`/`option`/
  slice/`Unit` renderings — Core carries the semantic decisions
  (variant symbols, `EscapeResult` operand types, EqPlan recipes),
  so codegen renders, never re-derives.
- **eval**: `interp.localT` collects the same names from
  `check.TypeDecl`s; `VVariant.IsResult(localT)`, `resultTag`, and
  `errIsGoError` agree with the compiler's reading, keeping the
  evaluator and the generated Go in lock-step.

A name is shadowed **file-wide**: inside a file declaring
`type Result`, every unqualified `Result` denotes that declaration —
the builtin is unreachable, not merely second in a lookup order.
That is the resolution model
[name-resolution](2026-11-02-name-resolution.md) already gives
inner-over-outer scopes, applied here to the builtin scope itself.

When a construct specifically needs the shadowed builtin the
checker reports **E0415** instead of silently reading the local
type as the builtin or emitting uncompilable Go:

| Construct | Why it needs the builtin |
| --- | --- |
| `try` operand, `\|?>`, Result escape boundaries | unwrap `Result a e`'s flat shape |
| builtin `Some`/`None`/`Ok`/`Err` references | their signatures mention `Option`/`Result` |
| `[...]` literals, `[p, ..ps]` patterns, `..x` spreads | produce/consume `List a` |
| `for x in iter` over `List`/`Chan`/`RecvChan` | iteration is a builtin shape |
| `select` recv-arm bind (`Option a`) | wraps the receive in `Option` |
| literals `()`/`true`/`'c'`/`"s"`, numeric/ordered constraints | denote `Unit`/`Bool`/scalar builtins |
| `if` condition, match guard | require `Bool` |
| test body `Unit`/`Result Unit e` | the boundary observes the builtin shape |
| mid-block statement, `go {}` body | must produce `Unit` |
| foreign/`extern`/builtin-table signatures | unqualified spellings resolve to the local decl |

Signature-sourced E0415s are deduplicated per (symbol, builtin
name) — the complaint is about the callee's unrepresentable
signature, not each call site.

## Consequences

- `type Result`/`type Option`/`type List`/... stay **legal** — the
  shadowing programs compile, and the local ADT lowers, validates,
  generates, and evaluates exactly like any other sum type
  (`testdata/golden/local_result.*` shows the sealed-interface
  output).
- `types.Con` keeps its shape — no reserved qualifier or
  back-pointer; each phase's existing declaration table is the
  identity source.
- Qualified names are never shadowed: `Go.Int`, `pkg.Result` keep
  their meaning (the Go boundary's `Go.Int` numeric conversion
  survives a local `type Int`).
- The rule is total: a file cannot mix "builtin Result" and "local
  Result" readings of the unqualified name. Where a builtin is
  genuinely needed — a `(T, error)` Go call's signature, `try` —
  the file must either not declare the colliding name or take the
  E0415.
- The validator refuses the reading on hand-built IR too, so the
  invariant is enforced even where the checker's E0415 never ran.
