# Verification report: name resolution (P1-4)

- 目的: verify the `internal/resolve` implementation against the
  contract in [decisions/name-resolution](../decisions/2026-11-02-name-resolution.md)
  and the handoff expectations of `internal/check`'s typed AST.
- 確認日: 2026-11-02. Toolchain: go1.27.1.

## What was verified

`~/go/bin/go1.27.1 test ./internal/resolve/` — table-driven tests
over hand-built `syntax.File` literals (no parser yet, P1-3):

| area | test | result |
| --- | --- | --- |
| scope chain | file → param → block-let shadowing, distinct symbols per binder | pass |
| sequential let | rhs cannot see the binder; next stmt can | pass |
| match arms | arm binders scope guard+body; sibling arms isolated | pass |
| ctors | unqualified `Circle`, qualified `Shape.Empty`, builtin `Option.Some`, shadowing of builtin `Some` | pass |
| ctor fields | `{ r = x }` resolves `r` to `KindField`; misspelled field → E0201 at the ident | pass |
| or-patterns | `A x \| B x` shares one symbol across both Defs and the body use | pass |
| type variables | `fn id (x: a) -> a` — first `a` binds (Defs), second uses; type-decl params; undeclared → E0201 "typevar" | pass |
| duplicates | `fn f x x`, `let x` + `fn x`, `type A/B` sharing ctor `Leaf` → E0202; inner shadowing is not a dup | pass |
| imports | `import go "fmt"` via `MapLoader` → member resolves with `Package`/`IsGo`; no loader → E0203 at the path literal, alias still bound; loader miss → E0203 | pass |
| members | `mypkg.G` missing → E0201 "member" at the selector | pass |
| @test | `fn` body referencing `@test fn` → E0802; `test` block reference resolves; `@test let` binders marked | pass |
| select/for/lambda | recv-arm, loop, and lambda binders scope their bodies | pass |
| extern | `extern go "io"` `Writer` resolves to the io member; entries are `IsGo`/`Package` funcs | pass |
| completeness | oracle walks every `*syntax.Ident`: Defs ∪ Uses ∪ exempt-position ∪ diagnosed-span = everything | pass |

The completeness oracle (`identWalker`) is the strongest guarantee:
after `File` returns, no ident in a binding or reference position
is silently dropped.

## Bugs found and fixed during verification

- **`fn f x x` and top-level `let x; let x` missed E0202** — the
  duplicate table was fresh per `bindPat` call. Fixed by sharing the
  table across a parameter list and adding a same-scope-level check
  in `bindVar` (after the or-pattern merge check, so `A x | B x`
  still shares).
- **failed-package member selects cascaded E0201** — a `nil` member
  table fell through to the member-miss diagnostic; now suppressed
  uniformly via `memberTable` (the import site already reported
  E0203).
- **`Option.Some` unresolvable** — the builtin `Option` module
  shadows the type in term position; member misses now fall back to
  the same-named type's constructor table (expression and pattern
  position).
- **`@test let` binders not marked** — pattern-bound vars now get
  `TestOnly` via `binderIdents`.
- **`go-check-sumtype` violations** — partial switches with working
  defaults were flagged (`-default-signifies-exhaustive=false`);
  declaration switches are now exhaustive, and the two deliberately
  partial ctor-head matches are `if` chains with a comment saying
  why.

## Known boundaries (by design, in the decision doc)

- `p.name`, `{ name = e }`, record-pattern field names, foreign-ctor
  payload names, and call-site `~labels` are type-directed — check
  resolves them; the resolver leaves no Uses entry.
- `a.b.c` resolves only the first hop.
- same-file ctor name collisions are E0202 (shared term namespace),
  not a separate ambiguity diagnostic; `T.C`/`pkg.C` disambiguate.
- wrong-kind reports ("not a constructor", "not a package", "has no
  field") ride on E0201 with `namespace`/`resolved_kind` data —
  filed as a problem since a dedicated code may read better.
- E0204 "unknown attribute" cannot fire — the AST carries only
  `TestOnly`; filed as a spec/AST gap.

## Commands

```sh
~/sdk/go1.27.1/bin/gofmt -l .     # empty
~/go/bin/go1.27.1 vet ./...       # clean
~/go/bin/go1.27.1 test ./... -count=1
make check                        # incl. go-check-sumtype
```
