# P1-5 checker implementation report

Verification: `gofmt -l .` clean, `go vet ./...` clean,
`go test ./...` all packages pass, `make check` (fmt + vet + test +
go-check-sumtype + experiments) green.

## What was built

`internal/check` converts a `resolve.File` (resolved surface AST) into
a typed `check.File` plus `[]diag.Diagnostic`:

- `check.go` — entry point, declaration walk, `fn`/`let`/`test`/type
  decls, `try` boundary stack, builtin signatures.
- `env.go` — `SignatureSource`/`MapSource`/`Chain`, `Signature{Type,
  Variadic, DeclArity}`, builtin env (`Option`/`Result` ctors,
  `assert`/`assert_eq`/`todo`), disjoint builtin var-id range.
- `typenv.go` — type declarations → `typeInfo` (record fields with
  syms/indices, sum variants with payload fields, aliases, abstracts),
  builtin type constructors, `typeExpr` evaluation with scopes.
- `expr.go` — every `syntax.Expr` checker, `checkSelect` member
  resolution (module member, record field, sum payload field, Go
  method, Go.conv ascription).
- `apply.go` — application checking, label/positional matching,
  `CallInfo` construction and validation, Go tuple unpacking,
  variadics, spreads, `_` placeholder.
- `pat.go` — all pattern forms against an expected type, record/ctor
  payload typing, `select` arm patterns.
- `unify.go` — structural unification over `types.Type`, meta records
  (`num`/`flo`/`intg`/`ord`/`eq`/`dflt`), `satisfies`, deterministic
  `finalize` (span, id) with E0407/E0409 reporting.

Roughly 90 hand-built `resolve.File` tests cover call kinds,
diagnostic codes, `CallInfo` invariants, Go interop shapes, and the
control-flow/pattern surface.

## Findings worth noting

- **Declared arity vs. flattened shape** — `types.Fn` flattens a
  function-typed result into the parameter list, which would make
  `fn mk () -> (Int -> Int)` look arity-2 at `mk ()`. The checker
  builds `*types.Func` literally in `fnSig` and carries `DeclArity`
  through `Signature`; `CallInfo.Arity` is always the declared count
  and extras become over-application.
- **Constrained metas pick the semantic code** — a numeric literal
  mid-block or under an else-less `if` is provably non-`Unit`, so the
  checker emits E0402/E0403/E0701/E0801 rather than a generic E0401
  mismatch against `Unit`. The same "one diagnostic per root cause"
  rule makes blocks take the expected type as their own (the inner
  statement already reported) and binds recovery metas instead of
  leaving E0407s.
- **`ArgToParam` nil means two things** — identity mapping *and*
  unrepresentable mapping (over-application, unknown labels, repeated
  variadic slots). Documented; see the problems file.
- **E0404 is Go-only in practice** — a Gom call missing labels is
  `CallPartial`. See the problems file.
- **go-check-sumtype enforces listing every variant** —
  `default: panic("unreachable")` alone is not accepted; catch-all
  behavior moves into explicit case groups ending in the panic
  default. ~20 switches in `internal/check` were made explicit.
- **`test` boundaries** — a `test` body may be `Unit` or
  `Result Unit e`; `try` inside escapes to the test, and a still-free
  error channel defaults to `GoError`.

## Handoff

- P1-6 (pattern): patterns carry resolved syms/types; exhaustiveness
  reads `CtorPat.Ctor`/`variantInfo` data — nothing re-resolved.
- P1-7 (lower): consume `CallInfo` directly (`Kind`, `ArgToParam`,
  `HoleIndex`, `Variadic`, `Arity`/`Applied`/`Unapplied`); never infer
  call convention from `Fn`'s shape. Variadic tail mapping rule lives
  in the problems file.
- Known gaps are filed under docs/problems/2026-11-02-*.md; the
  checker design notes are docs/decisions/2026-11-02-monomorphic-checker-design.md
  (draft).
