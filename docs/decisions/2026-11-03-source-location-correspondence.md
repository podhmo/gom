# Source-location correspondence: anchor table shape and Go-position restore

- 状態: 採用
- 日付: 2026-11-03
- Supersedes: [generated-go-conventions](2026-11-02-generated-go-conventions.md) のうち `//gomsrc` sentinel の形式（`<line>:<file>` → `<idx>:<line>:<file>`。splice 手順と列 1 での書き換えは変わらない）

## Context

docs/design.md section 2.2 specifies a "Gom span → Core IR origin →
generated Go range" correspondence table used for three jobs: driving
the `//line` directives in generated code, restoring `go build` /
`go vet` / `go test` diagnostics to .gom positions, and translating
panic stack frames. Until P4-5 the table existed only implicitly —
`//line` directives were emitted but the mapping itself was never
materialized, so `gom test`'s build-error folding kept a private
regex-and-resolve pair, panic frames had no restore path at all, and
the future LSP (P4-6) had nothing to query.

## Decision

### The exported table is one anchor per `//line` directive

`diag.SourceMap` is a list of `Anchor{Line, Span}` — the generated
line a directive pins paired with the .gom byte-offset span of the
node emitted there. codegen records `gen.marks` (the span of every
`//gomsrc` sentinel) and `spliceLines` appends one anchor for each
directive it rewrites, so the exported map is *derived from the same
operation that produces the directives* — the two can never disagree
about which lines a file claims. `codegen.Output` exposes it as
`SourceMap` / `TestSourceMap`; `build.Result.Output` is the internal
API boundary (the P4-6 LSP entry point).

The directive operand stays `file:line` (line of the span's start) —
go/token has no span concept — while the anchor keeps the full span.
`SpanAt` deliberately does NOT imitate Go's continuation semantics:
where `//line` numbering drifts past a construct's real extent into
sibling lines, `SpanAt` reports the enclosing construct's span. That
is the better answer for diagnostics and editors; the drifted line is
an artifact of line-granularity transmission.

### Go-position restore lives in internal/build, not rt

`build.GoPosIndex` is the name-resolution half: a printed position
name matches a compiled input by exact srcs key, `./`-stripped form,
or dir-absolutized path; `Resolve` turns 1-based line/col into a
clamped point span. `build.GoToolDiagnostics` folds the error stream
into E0001 `source:"go"` diagnostics (first numeric fields split —
the file group is non-greedy so `file:line:col:` orders correctly;
column is optional). `diag.ParseStack` splits panic/goroutine frames
and `GoPosIndex.ResolveStack` resolves them — `rt` keeps depending on
nothing but Go itself, and panic-restore shares the same index as
build errors. Hand-written Go and toolchain files resolve to no span,
keeping their diagnostics positionless.

## Consequences

- `//gomsrc` sentinels changed shape (`//gomsrc:idx:N:file`, the
  index into marks — see Supersedes). The directive bytes are
  unchanged — golden files were not rewritten. Malformed sentinels
  pass through verbatim and never index `marks` out of range.
- `Input.Path` may not contain control characters or invalid UTF-8:
  it is written verbatim into sentinels and `//line` operands, so a
  `\n` would split a sentinel line and inject arbitrary directives.
  `build.Compile` refuses with E0002 before parsing — the driver
  boundary covers CLI and library callers alike.
- `cmd/gom/test.go` lost its private regex/resolve pair; the mapping
  logic is now shared and unit-tested.
- Column positions in anchors are whole spans: a `.gom` offset maps
  back through `LineAt` to the innermost anchored construct, which is
  enough for LSP outline/hover queries. Finer per-expression mapping
  (columns within a generated line) is future work if an editor
  feature needs it.
- Multi-.gom-file runs index by printed name; two inputs reachable
  under the same relative spelling in different directories resolve
  by the run directory, same as the toolchain prints them.

### `//line` drift aliases generated lines to wrong .gom lines

`//line` transmits only (file, start line): when one .gom line
expands into k generated lines, Go continues numbering into the
following generated lines — so a generated line can resolve to a
*different real .gom line* than the construct's (drift). The anchor
table records the true span, but `GoPosIndex.Resolve` deliberately
does not use it: it resolves the printed position the way Go
reported it — same approximation the previous implementation made,
so this is not a regression. A SourceMap-aware reverse lookup (take
the directive's mapped position, then bound the span by the
enclosing anchor) is the real fix and is a design input for P4-6.

## Verification

[sketch: 2026-11-03-source-location-mapping](../sketch/2026-11-03-source-location-mapping.md)
— go/token sweep of all 23 golden cases, `go run` panic E2E, and
unit tests for both lookup directions and the error parser.
