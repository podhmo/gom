# Verification report: internal/diag model (P1-1)

- 日付: 2026-11-02
- 対象: [decision: diag-internal-model](../decisions/2026-11-02-diag-internal-model.md)

The `internal/diag` package and its unit tests (internal/diag/*_test.go)
are the prototype for the draft decision. This report records what was
verified before marking it 採用.

## Coverage of spec section 12

- Position model: `Span` anchors diagnostics to 0-based UTF-8 byte
  offsets (the source of truth); `LineIndex` computes 1-based
  line/column at output time with columns in Unicode code points.
  `PosUTF16` additionally produces UTF-16 columns for LSP-style
  coordinates. Verified by table tests over ASCII, multi-byte UTF-8
  (`あいうx`), and a surrogate-pair case (`𐐀x`) where code-point and
  UTF-16 columns diverge.
- Message fields: `Severity`/`Code`/`Message`/`Data`/`Notes`/`Fixes`
  mirror the spec's diagnostic object; `Severity.String()` and
  `Applicability.String()` return the spec's tokens
  (error/warning/note, safe/placeholder/unsafe). `Source` carries the
  `"go"` marker for Go-toolchain diagnostics (item 6).
- Wire shape: `Pos`/`SpanPos` marshal to exactly the spec's position
  objects (`{"line":…,"col":…,"offset":…}`), checked by a golden JSON
  comparison.
- `List.Counts` produces the errors/warnings counters the `summary`
  line reports; `Sort` gives a deterministic order (position,
  descending severity, code, message; positionless diagnostics first).

## Edge cases

- Empty source, source without a trailing newline, and a trailing
  newline producing a final empty line.
- Offsets clamped into `[0, len(src)]`; offsets inside a multi-byte
  character return a best-effort column (documented as non-monotonic;
  could floor to the rune boundary later).
- CRLF: `\r` counts as an ordinary column character.
- `Validate()` rejects invalid severity, empty code/message,
  multi-line messages, inverted or file-less spans, and malformed fixes.

## Bug found in review and fixed

The fluent builder originally appended to shared `Notes`/`Fixes`
backing arrays and wrote into a shared `Data` map, so two diagnostics
derived from one base corrupted each other (the appended note of one
overwrote the other's; map writes leaked). Fixed by clone-on-write
(`maps.Clone`, `slices.Clone`) and covered by
`TestDerivedDiagnosticsAreIndependent`, which asserts independence of
base and both derived copies in both directions.

## Result

`gofmt -l .` empty, `go vet ./...` and `go test ./...` pass on
Go 1.27.1. The model needs no spec.md or design.md text changes — it
is internal API detail implied by design.md section 2.3 — so the
decision moves to 採用.
