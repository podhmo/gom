# Lowering relies on checker contracts it cannot verify — and uses an unregistered error code

- 状態: 未解決
- 日付: 2026-11-02

## Situation

`internal/lower` treats the typed AST as contract-bearing input.
Where a contract is violated it emits a `phase: lower` diagnostic
rather than panicking or producing malformed Core, and `lower.File`
runs `core.Validate` over its own output, surfacing violations as
diagnostics (compiler-bug evidence instead of a bad tree).

Two gaps showed up while doing this:

1. **The diagnostic code is provisional** — *resolved 2026-11-02 by
   the `@tailrec` task*: the `09` "Lowering" category is registered
   in `internal/diag/codes.go` (`E0901` internal-only, `E0902`–
   `E0905` user-facing) and generated into `docs/error-codes.md`.
   ~~the error-code table has no `E09xx` range yet.~~
2. **The contracts themselves are implicit.** The same observation
   already filed for eval in
   `2026-11-02-eval-typed-ast-contract.md` applies a fortiori to
   lowering, which additionally assumes:
   - `check.CallInfo` is coherent: `Applied`/`Arity`/`ArgToParam`/
     `HoleIndex`/`Unapplied`/`Variadic` agree with `Args` and the
     callee's declared arity — lowering never re-derives them;
   - pattern `Base.Typ` is populated on every pattern node (the
     decision-tree compiler needs subject/payload types);
   - a `Result`-typed operand's `Con` is the unqualified builtin
     `Result` (an explicitly re-bound local type named `Result`
     would be misclassified — same issue as eval's nominal check);
   - likewise a `for` iterator's `Con` is classified by unqualified
     name (`Chan`, `RecvChan` → channel iteration); a same-file type
     named `Chan`/`RecvChan` would be misclassified — the checker's
     own `chanElem` shares the ambiguity, so the real fix belongs to
     the checker or to a builtin marker, not to lowering;
   - `for` patterns/`let` patterns the checker marked irrefutable
     really are — lowering trusts `check` for the pure-vs-tested
     split of destructuring.
   - a Go callee's result shape (`GoSig`) is either supplied via
     `Env.GoSig` or faithfully derivable from the Gom result type;
     the derivation (`goResultsOf`) cannot distinguish a real
     `(T, bool)` pair from a comma-ok declaration — that flag must
     come from the caller.

## Why it is a problem

A checker bug that breaks one of these contracts produces a
confusing `E0901` "contract gap" diagnostic far from the real fault,
or — worse — a silent miscompile when the violated invariant is one
lowering cannot detect (e.g. a wrong `ArgToParam`).

## Possible responses

- Register a real `E09xx` range for lowering/contract-gap
  diagnostics in the error-code table.
- Write the input contract down (extend this file or a decision into
  a normative `docs/` statement of what `check` guarantees `lower`),
  mirroring how `core/validate.go` documents the *output* side.
- Have `check` itself assert the cheap invariants (`ArgToParam` is a
  permutation of filled positions, `Applied == len(filled)`, etc.) so
  bad `CallInfo` never reaches lowering.
