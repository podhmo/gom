# Verification report: diagnostic emitters (P1-10)

- 日付: 2026-11-02
- 対象: [decision: diag-output-formats](../decisions/2026-11-02-diag-output-formats.md)

The emitters in `internal/diag` (emit_json.go, emit_text.go,
report.go, resolve.go) and their tests are the prototype for the draft
decision. This report records what was verified.

## Coverage of spec section 12

- **JSON Lines**: `TestMarshalDiagnosticSpecExample` builds the E0301
  diagnostic of the spec example — span `{line:2,col:3,offset:41}` →
  `{line:9,col:4,offset:228}`, `data`, `notes`, `fixes` with a
  resolved edit — and compares the emitted object byte for byte with
  the spec's JSON (modulo `data` key order, which `encoding/json`
  sorts). A synthetic `eval.gom` reproduces the spec's offsets,
  including the two-byte `é` behind column 4 of line 9.
- **`kind` / envelope**: `kind`/`code`/`severity`/`message` field order
  matches the spec example; `test`/`unformatted`/`applied`/
  `explanation` are exported `Kind` constants for later commands.
- **Summary**: `{"kind":"summary","ok":…,"errors":…,"warnings":…,
  "files":[…],"outputs":[…]}` byte-compared to the spec example;
  `files`/`outputs` marshal as `[]` when empty, never `null`;
  `files` deduplicated+sorted, `outputs` deduplicated keeping order;
  `ok` ↔ `errors == 0` (warnings don't fail).
- **Exit codes**: `Report.ExitCode` → 0/1; `ExitInternal` (= 2) is a
  constant for the driver, never derived from diagnostics.
- **Positions**: code-point columns verified with `あいう`
  (3 runes / 9 bytes → cols 10–12, not byte-derived); multi-file spans
  and fix edits resolve through `NewMapResolver`; `NewDirResolver`
  reads lazily and memoizes failures.
- **Omission**: `span`/`source`/`data`/`notes`/`fixes` absent when
  empty; `source:"go"` emitted for `SourceGo`.
- **Robustness**: invalid diagnostics (empty code/message, multi-line
  message), unresolvable files, non-JSON `data` (chan), nil resolver
  and failing writers all return errors rather than emitting a broken
  stream. Emission is deterministic (sorts a copy) and HTML-escaping
  is off so `->`/`&&` stay readable.

## Text format checks

- Byte-exact goldens pin the layout: `error[E0401]: …` header,
  `  --> file:l:c`, gutter `|`, numbered source lines, `^` underlines,
  `   = note:`/`   = help:` blocks, blank-line separation, rustc-style
  trailer (`aborting due to previous error`, `; N warnings emitted`,
  `warning: N warnings emitted`, `success`).
- Multi-line spans underline every covered line; a span ending on a
  line break underlines through it without spilling to the next line
  (`"cd\n"` → `^^^` on `cd`); a zero-width span emits one caret; a
  multi-byte end still covers the whole rune (`あいう` → 3 carets);
  `\t` displays as 4 cells.
- Gutter widens with two-digit line numbers (`11  |`, `    |` …).
- `Color: true` wraps headers/carets in ANSI; default stays plain.
- Two-digit gutter, positionless diagnostics, multi-line notes and
  `(placeholder)`/`(unsafe)` fix suffixes covered by goldens.

## Bug found while testing

`Pos(End-1)` over-counted the column when `End-1` landed inside a
multi-byte rune (the best-effort column counts the truncated sequence
byte-per-byte), yielding one caret too many for `あいう`. Fixed by
underlining through the *rune* containing `End-1` — backtrack to its
start byte — plus extending the underline for columns past the line's
content (a span ending on `\n`).

## gomtest unification

`fileResolver`, `renderDiagnostic` and the `wire*` types are deleted;
`-update` now encodes each diagnostic with `diag.MarshalDiagnostic`
(the real encoder) and indents via `json.Indent`, and resolution goes
through `diag.NewDirResolver`. One wire shape, one encoder.

## Result

`gofmt -l .` empty, `go vet ./...` and `go test ./...` pass on
Go 1.27.1. The decision's open points — the text layout beyond the
spec example's reach, notes as the human-facing detail channel — are
recorded in the decision and
[problems/text-format-detail-block](../problems/2026-11-02-text-format-detail-block.md).
