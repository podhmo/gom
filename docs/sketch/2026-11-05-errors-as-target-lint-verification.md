# Verification: `errors.As` target lint (W0502)

Date: 2026-11-05. Covers
[errors-as-target-lint](../decisions/2026-11-05-errors-as-target-lint.md).

## What the prototype ships

- `internal/check/lint.go`: `lintErrorsAsTarget` joins the
  post-inference sweep beside `lintChanMake`/`lintGoSliceCopy`. It
  recognizes the call by the callee symbol (`errors.As`, `CallGo`)
  and inspects parameter-1's argument through `ArgToParam`, so
  reordered named applications still find the target.
- Three provable-violation arms warn:
  - non-`Ptr` argument — "`errors.As` requires a non-nil pointer
    target — an argument of type T panics at run time";
  - literal `Ptr.nil` (through ascribes; the rt-backed `PtrNil`
    symbol, not the spelling) — the non-nil half of the contract;
  - `Ptr E` where `notAsTargetElem(E)` proves `*E` is neither an
    interface nor able to implement `error`: builtin concrete
    types, `types.Func`, `types.Tuple`, declared records and
    `Go.Slice`/`Go.Map`. `Option` and declared sums stay silent —
    their Go representations are interfaces — and so do
    `types.Go`, `GoError`, `Go.Any`, `Ptr` elements, metas and
    variables.
- `internal/diag/codes.go`: the `W0502` entry (Phase lint, Spec
  8.4).
- spec §8.4: `errors.As` keeps its derived `(GoError, Go.Any) ->
  Bool` surface under the lint.

## Evidence

`testdata/spec/W0502_errors_as_target.gom` — verified output:

```
W0502  an argument of type Int panics at run time          (scalar arg)
W0502  — Int is neither and panics at run time             (Ptr (List Int)... via Ptr Int)
W0502  — Rec is neither and panics at run time             (declared record)
W0502  `Ptr.nil` panics at run time                        (nil literal)
E0407  type argument for `a` could not be determined       (same line — the Go.Any
                                                            param gives `a` nothing)
```

Silent by design: `Ptr GoError` (`*error` — an interface target)
and `Ptr Sum` (a declared sum emits a sealed interface — legal
`errors.As` element).

## Boundary notes found while verifying

- `type Alias = List Int` is **not** a transparent alias — the
  grammar has no alias production for it; it parses as the
  single-variant sum `| List Int` (a variant named `List` with
  one `Int` field). Its `*Alias` target is therefore a sealed
  interface — a legal target — and the lint correctly stays
  silent. `let al: Alias = [1]` is an ordinary E0401 for the same
  reason, pre-existing.
- The `notAsTargetElem` `kindAlias` arm exists for declared
  aliases reached through imported (`.gomi`) type tables; in-file
  Gom has no alias production.
- A bare `Ptr.nil ()` in the target position always co-reports
  E0407 (`a` is unconstrained): the lint arm is belt-and-braces
  for the same guaranteed panic, and fires only where the
  checker's own error already lands.
