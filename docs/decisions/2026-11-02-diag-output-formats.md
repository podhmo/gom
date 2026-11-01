# Diagnostic output formats: JSON Lines and text emitters (P1-10)

- 状態: draft
- 日付: 2026-11-02
- 検証: [diag-output-verification](../sketch/2026-11-02-diag-output-verification.md)

## Background

P1-10 implements the output layer of spec section 12: the JSON Lines
stream (the default format) and the human-readable text format
(`--format text`). The JSON shape, the `kind` vocabulary, the trailing
`summary` object and the exit codes are fixed by the spec. What the
spec leaves open — the exact text layout, how emitters reach source
text, empty-field semantics — is decided here.

## Decisions

### Placement and surface

- The emitters live in `internal/diag` (emit_json.go, emit_text.go,
  report.go, resolve.go). The package already owns the internal model
  and the error-code table; compiler phases never import it for
  formatting — they keep producing `diag.Diagnostic` values only.
- `Report{Diagnostics, Files, Outputs}` is the unit a command renders.
  `Report.WriteJSONLines(w, res)` and `Report.WriteText(w, res, opts)`
  are the emitters; `Report.ExitCode()` maps severity to the spec's
  exit code; `MarshalDiagnostic(d, res)` exposes the single-object
  encoding for custom streams (`gom test`, `gom fix`, gomtest).
- `Resolver` abstracts span→line/column resolution: `LineIndex(file)`
  per file. `NewMapResolver` serves in-memory sources (the driver's
  case); `NewDirResolver` reads lazily from a directory (test harnesses,
  tools). `ResolveSpan` is the convenience wrapper.
- `Kind` constants cover the whole spec vocabulary (`diagnostic`,
  `summary`, `test`, `unformatted`, `applied`, `explanation`) so later
  commands share the tokens.

### Emission rules (both formats)

- Diagnostics emit on a copy sorted by `List.Sort` — byte-identical
  output regardless of collection order; the report's slice is never
  reordered.
- Every diagnostic must pass `Validate`, and every located span
  (including fix edits) must resolve through the resolver. Any failure
  stops the write and is returned: an unresolved source or an invalid
  diagnostic is a driver/compiler bug, reported through exit code 2 —
  never silently degraded.
- `summary` is always the last line, even with zero diagnostics.

### JSON Lines specifics

- Field order follows the spec example: `kind`, `code`, `severity`,
  `message`, `span`, `source`, `data`, `notes`, `fixes`. `span`,
  `source`, `data`, `notes`, `fixes` are omitted when empty.
- `files` and `outputs` in `summary` always marshal as arrays (`[]`,
  never `null`). `files` is deduplicated and sorted; `outputs` is
  deduplicated keeping the caller's order. `ok` is true exactly when
  `errors == 0` — warnings do not fail a run.
- No HTML escaping: messages carry `->`, `|>`, `&&` and must reach the
  wire readable (`json.Encoder` with `SetEscapeHTML(false)`; map keys
  in `data` sort per `encoding/json`).

### Text specifics

- Layout follows the spec section 4 example, which is rustc's with one
  extra leading column: `|`, `=` and the numbered-source `|` sit in a
  gutter one column wider than the largest displayed line number —
  `  --> file:l:c`, `   |`, `2  | source`, `   |   ^^^`,
  `   = note: …`.
- The primary span renders its source lines with `^` underlines:
  single-line spans underline the covered columns; multi-line spans
  underline each covered line (start-column→EOL on the first, full
  width in the middle, column 1→end on the last). rustc's `____^`
  bracket style was considered and postponed.
- The span end is exclusive: a span ending at a line break underlines
  through it, and an end landing mid-rune underlines the whole rune
  (the underline ends after the rune containing `End-1`).
- `\t` displays as four spaces and counts four cells for alignment;
  every other rune counts one cell — the spec's code-point column unit.
  Wide glyphs (CJK) may look misaligned; matching rustc's Unicode-width
  padding is future work.
- Notes render as `   = note: …` (continuation lines align under the
  text); fixes render as `   = help: <title>` with ` (placeholder)` /
  ` (unsafe)` appended for non-safe applicability. Structured `data`
  and fix edits are not rendered — text is the lossy human view;
  phases should put human-facing detail into `notes`.
- Diagnostics are separated by a blank line; a blank line precedes the
  trailer. The trailer is rustc-style: `error: aborting due to
  previous error` / `… due to N previous errors`, `; N warning(s)
  emitted` when warnings exist, `warning: N warning(s) emitted` for
  warning-only runs, `success` when clean. `files`/`outputs` are not
  shown in text.
- `TextOptions.Color` enables ANSI styling (severity color on headers
  and carets, blue structural marks, bold messages). The writer is
  never inspected: TTY detection and `NO_COLOR` belong to the caller.

### gomtest unification

- gomtest's temporary `wireDiagnostic`/`renderDiagnostic`/`fileResolver`
  are replaced by `diag.MarshalDiagnostic` and `diag.NewDirResolver`,
  so `-update` writes the exact bytes the compiler emits (indented
  with `json.Indent`, which preserves string contents verbatim).

## Consequences

- `cmd/gom` (P1-11) needs: build a `Report` from the pipeline's
  diagnostics + input/output file lists, pick a `Resolver`
  (`NewMapResolver` over read sources), call `WriteJSONLines` (default)
  or `WriteText` (`--format text`, `Color` from TTY/`NO_COLOR`), then
  `os.Exit(report.ExitCode())`. Emit errors map to exit code 2.
- A `docs/problems/` entry records that the spec section 4 text
  example's `| patterns not covered:` block cannot be produced from
  the current model — it would need labeled spans or a notes
  convention; a spec-level question, not an output-layer one.
- When `gom test`/`gom fix`/`gom fmt --check` land (P4), they compose
  `MarshalDiagnostic`, their own `kind` objects, and `Summary`
  (which marshals with `kind`) — no emitter changes needed.
