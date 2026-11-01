# Survey: diagnostics implied by spec.md → seeded codes (D-2)

- 日付: 2026-11-02
- 対象: [decision: error-code-table](../decisions/2026-11-02-error-code-table.md),
  `internal/diag/codes.go`, generated `docs/error-codes.md`

This report lists every rule in docs/spec.md that produces a diagnostic,
the code proposed for it, and the rules that produce no diagnostic.
It is a record, not a normative document.

## Codes named by existing documents

| Code | Rule | Source |
| --- | --- | --- |
| `E0301` | non-exhaustive `match` is a compile error, with counterexamples | spec 4, spec 12 example |
| `E0305` | select-specific exhaustiveness — **retired** | decisions/2026-11-01-select-receive-arms.md mentions 「旧 E0305」 |

## Diagnostics implied by spec rules (proposed codes)

| Code | Spec rule | Where |
| --- | --- | --- |
| `E0001` | go build / go vet errors mapped back to .gom, `"source": "go"`; every diagnostic needs a code | spec 12.6 |
| `E0101` | generic parse failure (unexpected token, brace disambiguation "anything else → syntax error", newline-rule failures) | spec 1.5–1.6, grammar.ebnf §8 |
| `E0102` | unterminated string / `${` interpolation | grammar.ebnf 2.3 |
| `E0103` | unterminated `/* */` comment | grammar.ebnf 2.1 |
| `E0104` | invalid escape sequence | grammar.ebnf 2.3 |
| `E0201` | undefined variable / type / constructor / module; punning `~to` without variable `to` | spec 2.7 |
| `E0202` | duplicate top-level name; `@test` shares the package namespace | spec 11.3 |
| `E0203` | import cannot be resolved (Go package missing; Gom package without .gomi) | spec 8.5 |
| `E0204` | attribute other than `@test` | grammar.ebnf 1 (rejected in resolve) |
| `E0302` | unreachable / redundant match arm is an error | spec 4 |
| `E0303` | `let` / `for in` / fn parameter requires irrefutable pattern | spec 1.1.5, 4 |
| `E0304` | or-pattern alternatives must bind the same variables with the same types | spec 4.1 |
| `E0401` | generic type mismatch (covers `\|?>` operands, `if`/`match`/`select` arm agreement, `==` operand mismatch, `for` over non-iterable) | spec 3, 9.2, 10.4 |
| `E0402` | mid-block expression of non-Unit type | spec 1.1.2 |
| `E0403` | `if` without `else` on non-Unit branches | spec 1.1.3 |
| `E0404` | missing labeled argument | spec 2.3 |
| `E0405` | duplicate labeled argument | spec 2.3 |
| `E0406` | unknown label | spec 2.3 |
| `E0407` | overloaded arithmetic unresolved → annotation required | spec 3.3 |
| `E0408` | labeled call while callee type is a type variable → annotation required | spec 2.1 |
| `E0409` | `==` on function-containing type or type variable; same rule for `assert_eq` | spec 3.3, 11.4 |
| `E0410` | `try` in a function not returning `Result _ e` | spec 6.2 |
| `E0411` | tried error type ≠ enclosing function's error type | spec 6.2 |
| `E0501` | referencing a Go symbol Gom cannot represent (reason in data) | spec 8.5 |
| `E0502` | method call `x.M(...)` on a Gom value | spec 8.1.3 |
| `E0503` | implicit numeric conversion that loses information | spec 8.1.5 |
| `E0504` | `_` placeholder: more than one, or outside a Go call | spec 8.2 |
| `E0505` | `..xs` spread not last / not in a Go call | spec 8.1.4 |
| `E0506` | generic Go function whose constraint is not `any`/`comparable` | spec 8.5 |
| `E0507` | `Go.conv e : T` that Go's conversion rules reject | spec 8.4 |
| `E0601` | library package without `.gomi` | spec 7.4 |
| `E0602` | `.gomi` containing expressions or other disallowed declarations | spec 7.1 |
| `E0603` | implementation type does not match `.gomi` | spec 7.6 |
| `E0701` | `go { e }` with `e` non-Unit | spec 10.1 |
| `E0702` | `try` directly inside `go { }` escapes across the goroutine boundary | spec 6.3 |
| `E0703` | more than one `default` arm in `select` | spec 10.4, grammar.ebnf |
| `W0701` | channel with mutable element type (lint) | spec 10 |
| `W0702` | `go { }` capturing a mutable variable (lint) | spec 10 |
| `E0801` | test block type not `Unit` / `Result Unit e` | spec 11.1 |
| `E0802` | `@test` definition referenced from non-test code | spec 11.3 |

## Spec rules that produce no diagnostic

- `match` on an invalid Go-supplied value (e.g. `nil` ADT): contract
  violation → **panic** at match time, not a diagnostic (spec 4, 8.6).
- `Ptr T` / Go interface nil dereference: panics like Go (spec 8.6).
- Self-tail recursion becoming `for`, name mangling for Go reserved
  words, `.gomi` hash-based rebuilds, `|?>` auto-wrap text: normal
  behavior, no diagnostic.
- Local `let` not generalized (spec 3.3): a typing rule — failures
  surface as `E0401`.

## Notes

- Categories are semantic groups, not pipeline phase numbers: `03` is
  pattern matching because `E0301` already occupies it.
- Several entries could later split (e.g. `E0101` generic syntax error
  into per-situation codes) — new codes are always new numbers; a code
  is never re-purposed.
- `N` (note) codes are well-formed but none are seeded; the format is
  validated in the table tests.
