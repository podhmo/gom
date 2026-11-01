# The evaluator assumes undocumented typed-AST contracts

- 状態: 未解決
- 日付: 2026-11-02

## Situation

`internal/eval` interprets `internal/check` nodes directly and, doing
so, assumes several contracts that are not written down anywhere:

- `check.Lit.Text` is the raw source lexeme — quotes included for
  strings/chars, `0x`/`0b`/`_` forms for ints — and `check.StrText.Text`
  is likewise raw text still containing escape sequences. The
  evaluator decodes Gom escapes itself; if check ever stores decoded
  text the evaluator would double-decode `\n`-shaped content.
- A `let`/`for`/parameter pattern the checker marks irrefutable is
  trusted to match; failure is reported as an internal-error
  diagnostic, not a dispatch failure.
- `Result a e` runtime shape: the evaluator recognizes a `VVariant` as
  a Result by nominal type (`Con{Name: "Result"}`, unqualified) and
  constructor names `Ok`/`Err`. A user-defined type *named* `Result`
  with `Ok`/`Err` variants would be treated as the builtin Result.
- `check.CallInfo` is trusted as the single source of call semantics
  (arity, `ArgToParam` positions, hole index); the evaluator never
  re-derives behavior from syntax. Malformed hand-built CallInfo is
  reported only where it produces an impossible value (non-function
  applied, position out of range).
- `main` is found by *name* (`Evaluator.Global("main")`); the
  evaluator does not distinguish `fn main` from a `let main`.

## Why it is a problem

These are the seams where a change in `check` silently changes
evaluator behavior without any compile-time signal — precisely the
divergences the comparison harness is supposed to catch, but between
two trees on the same side of codegen.

## Related

- internal/eval/doc.go "Contracts assumed of the typed AST"
- internal/check/typed.go, tpat.go (no documented contracts today)

## Possible responses

- Document the contracts on the check AST types (Lit.Text lexeme,
  CallInfo semantics, irrefutable-pattern guarantee) as part of P1-13.
- Keep the fallback paths (bare-name ctor identity, lazy ctor
  derivation) for hand-built ASTs and treat violations as E9004.
