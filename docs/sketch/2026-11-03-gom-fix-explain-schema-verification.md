# Verification: gom fix / explain / schema (P4-4)

- Related: [decisions/gom-fix-explain-schema](../decisions/2026-11-03-gom-fix-explain-schema.md), TODO P4-4, spec §12.

## What was verified

The three subcommands plus the first `safe` fix producer (E0402 →
`ignore (...)`), all exercised end to end through `run()` and the
real pipeline.

### gom fix

Manual probe (`/tmp/gomfix/mid.gom`, an E0402 mid-block `1 + 2`):

- `gom check` reports E0402 with
  `"fixes":[{"title":"discard the value with `ignore`","applicability":"safe", ...}]`
  — two insertions at the statement's edges.
- `gom fix` rewrites the file to `ignore (1 + 2)`, emits
  `{"kind":"applied","code":"E0402",...}` + `{"kind":"summary","ok":true,...,"outputs":["…/mid.gom"]}`,
  exits 0.
- `gom check` on the fixed file is clean; a second `gom fix` emits
  only the summary (nothing left to apply).
- An E0301 file (placeholder fix only) is untouched — stream is
  just the summary, exit 0.
- `gom fix -format text` prints `applied[E0402]: …` + `--> file:l:c`
  per edit and the `success` trailer.

Automated (cmd/gom/cli_test.go): TestFixAppliesSafeFix (apply →
file rewritten → check clean → second run empty),
TestFixSkipsNonSafe (placeholder untouched), TestFixText.

### gom explain

- `gom explain E0301` / `e0301` → one `explanation` object (code,
  severity, title, phase, spec, explanation, example) + summary.
- `explain -format text E0301` → `error[E0301]: non-exhaustive
  match` header, `phase: pattern · spec: §4`, prose, `Example:`.
- bare `gom explain` → one explanation per table entry + summary;
  `-format text` → the `CODE  title` index.
- `explain E9999` → `no diagnostic code E9999 is registered` on
  stderr, exit 2; `explain bogus` → `malformed code`, exit 2.

Automated: TestExplainCode, TestExplainCodeErrors, TestExplainList.

### gom schema

- `gom schema` → the indented draft-2020-12 document on stdout
  (`$id: "gom:schema:message:1"`, `"version": 1`), exit 0, parses as
  JSON; extra args are a usage error.
- `internal/diag/schema_test.go` keeps it consistent: oneOf covers
  all six `diag.Kind` constants, required-field sets match the wire
  encoders, the `code` pattern agrees with `WellFormed` on every
  registered code (and rejects `e0301`, `X0301`, …), all `$ref`s
  resolve.

### diag helpers

- `ApplyEdits` (apply_test.go): replace/insert/delete, unordered
  edits applying simultaneously, same-point insertions keeping
  declaration order, edge insertions not overlapping, src not
  mutated; overlap / out-of-bounds / invalid spans rejected.
- `MarshalApplied`/`WriteApplied` pin the `applied` wire shape and
  the text block form (apply_test.go).
- `MarshalExplanation`/`WriteExplanations`/`WriteCodeIndex` pin the
  `explanation` shape, `retired` handling, and the index format
  (explain_test.go).

### Spec fixture

`testdata/spec/E0402_midblock_ignore.{gom,expected.json}` pins the
new `fixes` field on E0402 (regenerated via `go test -update`).
`docs/error-codes.md` regenerated after the E0402 explanation
update; `docs/decisions/README.md` regenerated for the new decision.

## Gaps noted during verification

- `go run` masks exit code 2 as 1 (its own convention) — manual
  probes of explain's usage errors were confirmed via `run()` in
  tests instead.
- Skipped fixes (non-safe, overlapping, foreign-file) leave no
  trace in the stream — recorded as future work in the decision.
