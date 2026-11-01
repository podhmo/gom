# Shape of the internal diagnostic model (internal/diag)

- 状態: 採用
- 日付: 2026-11-02
- 検証: [diag-model-verification](../sketch/2026-11-02-diag-model-verification.md)

## Background

P1-1 implements `internal/diag`, the source of truth for compiler
messages from which JSON Lines, text and LSP renderings are derived
(spec section 12, design.md section 2.3, decision
[json-diagnostics](2026-11-01-json-diagnostics.md)). The spec fixes the
*output* shape; the choices below concern the internal Go API only.

## Decisions

- `Diagnostic` mirrors the spec's diagnostic object: `Severity`,
  `Code`, `Message`, `Span`, `Source`, `Data`, `Notes`, `Fixes`.
  `Notes` are plain strings, as in the spec's JSON; a note attached to
  its own location would require a spec change first.
- `Diagnostic` is a value type and stays independent under derivation:
  the fluent `Set`/`AddNote`/`AddFix` methods clone-on-write the
  `Data` map and the `Notes`/`Fixes` slices before mutating, so two
  diagnostics derived from one base cannot corrupt each other
  (aliasing bug caught in review; see the verification report).
- `Span{File, Start, End}` carries the file name so that diagnostics
  and fix edits can point across files without a global FileSet (a
  go/token-style FileSet was considered and postponed; P4-5 may revisit
  it for the Gom→Go position map).
- `Source` is a string field whose zero value means "produced by gom";
  `SourceGo` marks go build / go vet diagnostics mapped back to .gom
  positions (spec section 12, item 6).
- `LineIndex` is built per file over the source bytes, which it does
  not copy; offsets are converted to line/column only at output time.
  `Pos` gives code-point columns (the spec's unit); `PosUTF16` gives
  UTF-16 columns for LSP. Columns count code points — not grapheme
  clusters — and a tab or '\r' counts as one column each.
- An offset that lands inside a multi-byte UTF-8 sequence gets a
  best-effort column (each byte of the truncated sequence counts as
  one invalid rune), which can make column non-monotonic in offset.
  Valid spans always sit on rune boundaries, so this only affects
  malformed input; if it ever matters we could floor the offset to the
  rune boundary instead.
- `Validate()` enforces the format's invariants, including a non-empty
  `Code` on every diagnostic. Which codes Go-toolchain diagnostics
  carry is left to the error-code table (D-2).
- The wire-level JSON types and the `kind`/`summary` envelopes are out
  of scope here (P1-10); `Pos`/`SpanPos` already carry JSON tags
  matching the spec's position objects.

## Consequences

- Compiler phases construct diagnostics via `diag.Error`/`Warning`/
  `Note` plus the fluent `At`/`From`/`Set`/`AddNote`/`AddFix` methods,
  and collect them in `diag.List` (`HasError`, `Counts`, `Sort` —
  positionless diagnostics sort first).
- Output work (P1-10) resolves spans through `LineIndex` and maps the
  internal model to the spec's JSON; it should not need to redefine
  position conversion.
- Adopted without spec.md / design.md text changes: every point above
  is internal API detail already implied by design.md section 2.3 and
  spec section 12's output format.
