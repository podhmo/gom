# Sketch: source-location mapping — verification log

Verification report for
[decisions/2026-11-03-source-location-correspondence.md](../decisions/2026-11-03-source-location-correspondence.md)
(P4-5): the anchor table codegen exports, the `//line` precision the
Go toolchain relies on, and the position-restore APIs in
internal/build.

## Anchor ↔ directive agreement (the design-2.2 invariant)

`internal/build/srcmap_test.go::TestGoldenSourceMap` compiles all 23
testdata/golden cases through `build.Compile` and checks, per file:

- `len(Output.SourceMap.Anchors)` equals the `//line` directive count
  in `Output.Source` (same for `TestSourceMap`/`TestSource`).
- Every anchor resolves under `go/parser` + `token.FileSet` — Go's
  own `//line` reader — to `Span.File` and the `.gom` line of
  `Span.Start`. An anchor's pinned line is the construct's first
  generated line, so Go's reading and the exported span cannot
  diverge.
- Anchors are strictly increasing and `SpanAt(anchor.Line)` returns
  the anchor's own span.

`internal/codegen/srcmap_test.go` repeats the check on the synthetic
fixture (both halves) and pins the two lookup directions:
`SpanAt` answers with the enclosing construct's span between anchors,
`LineAt` picks the smallest containing span (the innermost
construct), unanchored header/import lines and uncovered offsets
report false.

## Panic stack restore — real `go run` E2E

`internal/build/e2e_gopos_test.go::TestPanicStackResolvesToGom`:

```gom
package main

fn boom (x: Int) -> Int = todo ()

fn main () -> Unit = {
  let _ = boom 1
  ()
}
```

compiled → written into a temp module (replace → worktree) →
`go build` → executed. The captured trace:

```
panic: gom: todo

goroutine 1 [running]:
main.boom(0x1)
	main.gom:3 +0x30
main.main()
	main.gom:6 +0x40
```

— the runtime prints `main.gom` positions unprompted; that is
`//line` doing its half. `GoPosIndex.ResolveStack` then maps
`main.boom` to `main.gom` line 3's head offset and `main.main` to
line 6's head offset; rt frames in the same trace stay unmapped
(`Span.Valid() == false`).

## Go tool diagnostics

`internal/build/gopos_test.go`:

- `ParseGoErrorLine`: `file:line:col: msg` and the col-less
  `file:line: msg`; `# pkg` headers and prose rejected. The file
  group is non-greedy — greedy splitting mistook the column for the
  line (`x.gom:12:5:` → file `x.gom:12`), caught by the test before
  any consumer shipped.
- `GoPosIndex.Resolve`: exact, `./`-stripped, and dir-absolutized
  names reach the same source; line clamps into the file (Go can
  report past-EOF lines), column clamps into the line; unknown files
  report false. An early bug — `filepath.Join(dir, p)` appends even
  when `p` is absolute — was caught by the absolute-name case.
- `GoToolDiagnostics`: one E0001 `source:"go"` per error line, gom
  files get resolved spans, handwritten `.go` and prose stay
  positionless with `raw` in data, empty runs yield one positionless
  fallback.
- `cmd/gom` e2e (`TestTestGoBuildFailure`) still passes after the
  migration to the shared API.

## Numbers

- 23/23 golden cases: anchor count = directive count, every anchored
  line resolves correctly under go/token.
- 3-frame panic trace: 2 .gom frames resolve to point spans, 1 rt
  frame unmapped.

## Known limitations (for P4-6 / open questions)

- **`//line` drift aliases lines.** `//line` carries only (file,
  start line): one .gom line expanding to k generated lines lets Go
  number into the following generated lines, so a generated line may
  resolve to a *different real .gom line* than its construct's.
  `GoPosIndex.Resolve` reproduces Go's reported position rather than
  correcting it through the anchor table — the same approximation the
  pre-P4-5 implementation made, not a regression. A SourceMap-aware
  reverse lookup (bound the reported position by the enclosing
  anchor's span) is the real fix — P4-6 design input.
- `SpanAt` returns the whole enclosing span for lines between
  anchors — deliberately coarser than Go's drifted continuation. If
  an editor feature needs sub-line precision (e.g. hover on one
  operand of a multi-operand expression), anchors would need
  column/range granularity — a table widening, not a redesign.
- `LineAt` is a linear scan; fine for diagnostics and editor queries,
  not for per-keystroke hot paths without an index.
- Positions resolve by printed file name; a generated file renamed
  after emission would still resolve, since the `//line` operand —
  not the filename — is what go reports.
