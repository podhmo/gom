# Error-code table and numbering scheme (D-2)

- 状態: 採用
- 日付: 2026-11-02
- 検証: implemented as `internal/diag/codes.go` + tests; survey in
  [error-code-survey](../sketch/2026-11-02-error-code-survey.md)

## Background

Spec section 12 fixes the diagnostic wire format: every diagnostic
carries a stable `code` that never changes meaning and is never reused,
and `gom explain <code>` / `gom schema` (P4-4) must read the code's
explanation from a table. D-2 asks for that table to be defined in one
place inside `internal/diag` and synced to `docs/`.

## Decisions

### The table lives in `internal/diag/codes.go`

A single `[]CodeInfo` literal is the source of truth, kept in the diag
package so the compiler, `gom explain`, and `gom schema` all read the
same data (design.md section 3 already assigns the code table and the
JSON Schema to `internal/diag`).

Entry fields:

| Field | Purpose |
| --- | --- |
| `Code` | the stable identifier, e.g. `"E0301"` |
| `Severity` | the severity diagnostics with this code are emitted with; must agree with the code's letter |
| `Title` | one-line English description |
| `Explanation` | longer Markdown prose for `gom explain` — what the rule is, why it fires, how to fix it |
| `Phase` | the emitting component (`driver`, `syntax`, `resolve`, `check`, `pattern`, `lower`, `codegen`, `iface`, `goimport`, `lint`) |
| `Spec` | optional spec.md section reference, e.g. `"8.2"` |
| `Example` | optional .gom fragment that triggers the diagnostic |
| `Retired` | the code is no longer emitted but stays reserved forever |

`LookupCode`, `CodeTable`, `Categories`, `WellFormed` and
`ValidateCodeTable` are the access API.

### Numbering scheme: `LCCNN`

- `L` — severity letter: `E` error, `W` warning, `N` note. The letter is
  part of the code, so a diagnostic's severity can never change without
  changing the code.
- `CC` — a two-digit **category** naming the language area, not the
  pipeline phase (E0301 already fixes "03" for pattern matching, which
  is pipeline phase 5 — categories are semantic groups and are never
  renumbered). Initial set: `00` compiler/toolchain, `01` syntax,
  `02` name resolution, `03` pattern matching, `04` type checking,
  `05` Go interop, `06` packages and `.gomi`, `07` concurrency,
  `08` tests.
- `NN` — a serial inside the category, allocated lowest-first.

Allocation rules: take the lowest free serial in the right category;
never reuse a number (retired entries remain in the table); if a
category is exhausted, add a new `CC` rather than renumbering.
`ValidateCodeTable` enforces format, letter/severity agreement,
declared category/phase, metadata presence, sortedness, and
uniqueness; `codes_test.go` runs it.

Category `00` covers driver-level diagnostics. It exists partly because
spec section 12 item 6 requires Go toolchain messages (go build /
go vet) mapped back to .gom to carry a code — that is `E0001`.

### docs/ sync: generated file + sync test (option b)

`RenderCodeDoc()` in `internal/diag/codes_doc.go` renders the table to
Markdown. `TestErrorCodesDocInSync` compares the rendering against
`docs/error-codes.md` and rewrites it under `-update`
(`go generate ./internal/diag` runs exactly that). Chosen over
`gom explain --dump`-style generation because the CLI does not exist
yet, the test prevents drift on every `go test ./...`, and P4-4's
`gom explain` can read the same Go table without a separate artifact.

### Seeded codes

`E0301` (non-exhaustive match) is the only code spec.md names.
`E0305` is seeded as **retired** — the select-specific exhaustiveness
check it once named was removed by
[select-receive-arms](2026-11-01-select-receive-arms.md), and its number
stays reserved. All other entries propose codes for diagnostics the
spec implies without naming codes (parse errors, undefined names,
non-Unit block expressions, label errors, `.gomi` rules, concurrency
lints, …); see the survey for the full rule-to-code mapping.

## Consequences

- Adding a diagnostic: pick the lowest free serial in the category, add
  a table entry, regenerate `docs/error-codes.md`. Emitting code uses
  the literal code; the table is the checkable registry.
- The seeded codes are a proposal, not spec: where the spec implies a
  diagnostic without a code, the chosen number is ours to keep or
  change before it ships in a diagnostic. Once a code is emitted by a
  released compiler it follows the never-reuse rule.
- `Diagnostic.Validate()` still checks only format-level invariants
  (non-empty code); whether emitted codes must be registered is a
  possible future strengthening left open here.
