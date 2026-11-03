# gom fix / explain / schema: wire shapes and the fix-application policy (P4-4)

- 状態: 採用
- 日付: 2026-11-03
- 検証: [gom-fix-explain-schema-verification](../sketch/2026-11-03-gom-fix-explain-schema-verification.md)

## Background

Spec section 12 assigns three commands their message kinds — `gom
fix` emits `applied` + `summary`, `gom explain <code>` emits
`explanation`, `gom schema` emits the format's JSON Schema — without
pinning the `applied`/`explanation` field shapes, the schema's
layout, or how `gom fix` chooses and lands edits. The internal model
(`diag.Fix{Title, Applicability, Edits}`, spec 12 item 5's
safe/placeholder/unsafe classification) already exists; until P4-4
no diagnostic carried a `safe` fix, so the apply path had nothing to
operate on either.

## Decisions

### The `applied` object: one per applied fix

```json
{"kind":"applied","code":"E0402","title":"discard the value with `ignore`",
 "edits":[{"file":"a.gom","start":{...},"end":{...},"text":"ignore ("}, ...]}
```

- One `applied` object per applied *fix* (not per edit): a fix is the
  atomic unit — consumers undoing or attributing a change want the
  group, and the edit list already carries per-file spans.
- `code` is the code of the diagnostic the fix answered; `title` is
  the fix's title as it appeared in `fixes`.
- `edits` reuse the diagnostic fix-edit shape (file + resolved
  start/end + text) and are resolved against the **pre-edit**
  sources — the coordinate space the diagnostic's own spans use.

### Fix application policy (`gom fix`)

- Only `ApplicabilitySafe` fixes apply (spec 12 item 5).
- A diagnostic's fixes are alternatives: the first safe fix whose
  edits all apply wins; at most one fix per diagnostic.
- A fix applies atomically — skipped when any of its edits overlaps
  an already-accepted edit or names a file not among the inputs
  (`gom fix` only rewrites files it was given; cross-file edits are
  future work if a producer ever needs them).
- Edits apply simultaneously to the original source
  (`diag.ApplyEdits`): overlapping edits are an error, insertions at
  a replaced range's edge are not, and same-point insertions keep
  declaration order.
- Apply order is `diag.List.Sort` output order, so the accepted set
  is deterministic.
- Single pass: fixes that become possible only after an edit appear
  on the next `gom fix` run.
- The compilation's diagnostics are not part of fix's stream (the
  spec's kind table assigns fix only `applied`/`summary`), so a run
  that applied what it could exits 0 even when errors remain —
  `gom check` reports them. `summary.outputs` lists the files
  rewritten; skipped fixes are not reported (surfacing them, e.g.
  under a `-v` flag, is future work).

### The first safe fix: E0402 → `ignore (...)`

`internal/check` now attaches a safe fix to E0402 (non-Unit
expression in a block): insert `ignore (` at the statement's start
and `)` at its end. The wrap makes the discard the code already
performs explicit — the spec-sanctioned idiom — so it is semantics
preserving and `safe` under spec 12 item 5's definition. The parens
are required: `ignore` binds at unary level, so `ignore a + b`
parses as `(ignore a) + b`. `gom fmt` re-derives operand parens from
precedence, so the wrap formats correctly afterwards.

### The `explanation` object and `gom explain`

```json
{"kind":"explanation","code":"E0301","severity":"error",
 "title":"non-exhaustive match","phase":"pattern","spec":"4",
 "explanation":"...","example":"..."}
```

- The wire object is the error-code table entry verbatim: `code`,
  `severity`, `title`, `phase`, `explanation` required; `spec`,
  `example` present only when the table has them; `retired: true`
  only on retired codes.
- Bare `gom explain` lists the table: the JSON stream holds one
  `explanation` object per code (retired included), the text format
  prints a compact `CODE  title` index — moon's `explain` precedent
  for a bare flag listing everything (sketch/moon-cli-command-structure).
- Code arguments are uppercase-normalized (`explain e0301` works);
  a malformed or unregistered code is a usage error on stderr with
  exit 2 — explain emits explanations, not diagnostics, so lookup
  failures are not `diagnostic` objects.
- Text format for an explicit code renders the table entry
  rustc-style: `error[E0301]: <title>`, a `phase: … · spec: §…`
  attribute line, the prose, then the example.

### `gom schema`

- Emits the JSON Schema of spec section 12 to stdout as an indented
  document; it is an artifact (like `gom fmt`'s output), not a
  stream row, so no `summary` follows it.
- The schema (draft 2020-12) describes **one message object**: a
  `oneOf` over the six kinds with shared `$defs` (`pos`, `span`,
  `edit`, `fix`). The stream is a sequence of such objects ending
  with `summary` — noted in the schema's `description`.
- Versioning (spec 12 item 7): `$id: "gom:schema:message:1"` plus a
  top-level `"version": 1`; both bump on incompatible changes. New
  kinds or optional fields are compatible and do not bump.
- The `test` kind is a provisional def constrained only to
  `kind: "test"` — `gom test` will tighten it when implemented.
- The schema lives in `internal/diag/schema.go` as a literal
  document (design.md assigns the JSON Schema to `internal/diag`);
  `schema_test.go` keeps it consistent — kind coverage, required
  fields, code pattern vs `WellFormed`, and `$ref` resolution.

## Consequences

- `internal/diag` gains `apply.go` (Applied + MarshalApplied +
  WriteApplied + ApplyEdits), `explain.go` (Explanation +
  MarshalExplanation + WriteExplanations + WriteCodeIndex), and
  `schema.go` (MessageSchema). `cmd/gom/tooling.go` holds the three
  command bodies; `main.go` dispatches them.
- Spec section 12 gains the `applied`/`explanation` object shapes.
- E0402 diagnostics carry a `fixes` field — spec-visible output;
  `testdata/spec/E0402_midblock_ignore.*` pins it and
  `docs/error-codes.md` regenerates.
- Out of scope / future work: reporting skipped fixes, cross-file
  edits, a fixpoint loop, a `-dry-run` diff mode, tightening the
  `test` schema def, and applying fixes discovered under `gom test`
  or other commands.
