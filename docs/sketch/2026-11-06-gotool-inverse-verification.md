# Prototype: the go-position inverse on real generated output

Verification record for
[docs/decisions/2026-11-06-gotool-position-inverse.md](../decisions/2026-11-06-gotool-position-inverse.md):
the anchor-table candidate enumeration implemented in
`internal/diag/srcmap.go` and `internal/build/gopos.go`, exercised
against real codegen output and a real `go build`.

## The drift shape, measured

For this input (`internal/build/goposmap_test.go`, `driftSrcGo`):

```gom
package m

import go "fmt"

fn pair (x: Int) -> (Int, Int) = {
    let tmp1 = x + 1
    (x, tmp1)
}

fn main () -> Unit = {
    let (a, b) = pair 5
    let z = a + b
    ignore fmt.Println z
    ()
}
```

the generated tail is (Go's reported line in brackets):

```
//line main.gom:13
	v, err := fmt.Println(tmp3)      [13]  anchored → `fmt.Println z`
	_ = result.FromGo(int64(v), err) [14]  UNANCHORED — drifts
//line main.gom:14
	return struct{}{}                [14]  anchored → `()`
}                                    [15]  unanchored — drifts
```

`//line` continuation arithmetic makes the materialization line
report **14**, colliding with the `()` literal that genuinely owns
14. The naive mapping (`file:line` taken verbatim) blames the
line-14 construct for errors sitting in line 13's tail.

## Candidate enumeration on the real table

After the comment/blank filter the claims are:

| reported | code claimants | verdict |
| --- | --- | --- |
| 8 | `}` of pair (drift) | drift → `(x, tmp1)` span |
| 11 | `pair 5`, `(a, b)` ×2 | ambiguous (same-line siblings) |
| 13 | `tmp2`, `tmp3`, `fmt.Println` | ambiguous (siblings, one span dedupes) |
| 14 | `_ = result.FromGo` (drift), `return struct{}{}` | ambiguous (the collision) |
| 15 | `}` of main (drift) | drift → `()` span |

Every drifted `//line` directive and blank line that the raw
`Candidates` enumeration produces is filtered out by the
generated-bytes check — in this table the filter is what turns
"five raw claimants" into "two real ones".

## Real `go build` evidence

`TestGoBuildDiagnosticsDriftE2E` writes the generated file into a
scratch module (`replace github.com/podhmo/gom => <repo>`), tampers
it, and runs the toolchain:

- `return rt.Tuple2[...]{...}` → `_ = tmp1`:
  `go build` prints `./main.gom:8: missing return` — the drifted
  `}` line. The fold lands on `main.gom:88-97` (the `(x, tmp1)`
  expression on line 7) with `data["drifted"]`, not on the `}` the
  naive mapping would point at.
- `_ = result.FromGo(int64(v), err)` → `_ = nope.FromGo(...)`:
  `go build` prints `./main.gom:14: undefined: nope`. The fold
  lands on the union `main.gom:177-197` — covering the line-13
  `ignore` construct (the truth) and the line-14 `()` literal —
  with `data["ambiguous"]` + `data["candidates"]`, rather than the
  naive line-14 point that names the wrong construct.

So: correct landing where unique, honest ambiguity where the
table genuinely cannot pick — no invented precision.

## Cost

Per `go build` invocation over the scratch module above
(`TestGoBuildDiagnosticsDriftE2E`, `t.Logf`): **~55.7ms and
~55.9ms** on this machine — call it ~55–60ms for a cold single-
package `go build`. The live fold (`gom test`) pays **zero** extra
invocations: `go test`'s build step already runs `go build`, and
`AddSourceMap` only registers bytes the compile already produced.
The `ResolveMatch` lookup itself is a binary search per anchor
plus one line-index `Pos` call per candidate — noise next to the
tool run. A hypothetical `gom lsp` go-build pass would pay the
~55ms plus a compile-with-codegen per change batch — that decision
is deferred (below).

## Where go-tool execution belongs — the LSP wall

Recorded honestly rather than shipped:

- `gom lsp` compiles with `CheckOnly: true` today — **no codegen,
  no SourceMap, no generated dir**, so there is nothing for
  `go build` to check and nothing to register maps for.
- Wiring the go tool into LSP therefore requires a
  compile-with-codegen path (or a scratch dir per package) plus a
  ~55ms-per-package tool run on every change batch. The substrate
  this wave built (`AddSourceMap` + `ResolveMatch` +
  `GoToolDiagnostics`) is exactly what that path consumes, but
  the invocation site is a separate decision — it changes LSP
  latency characteristics and needs the package-unit design wave
  landed first.
- `go vet` stays unwired for the same reason: no live caller.
  One wire-format wart is already measured: `go vet` prefixes
  wrapped compile errors with `vet: ` — `vet: ./main.gom:8:
  missing return`. `ParseGoErrorLine`'s greedy file field then
  captures `vet: ./main.gom` as the filename, so the line parses
  (`msg="missing return"`) but resolves no input and lands
  positionless — the `vet:` prefix disappears into `data["file"]`,
  the raw line survives in `data["raw"]`. The message text is
  preserved; only the position is lost. If a vet caller ships,
  strip a leading `vet: ` before the fold rather than teaching the
  parser the prefix (verified against real `go vet` output).

## Fallback honesty, pinned by tests

- `ResolveMatch` returns `Mapped=false` + the naive point when no
  candidate claims the report (reported line printed only by
  blank/directive generated lines) — the index never answers worse
  than before.
- Ambiguous matches never pick a side: `Span` is the union of all
  distinct candidate spans, `Candidates` carries the contenders in
  generated-line order.
- `RenderStack` rewrites resolved frames to their construct's line
  and normalizes ambiguous frames (printed position kept, PC
  trailer dropped) so the two-part panic shape still marks them as
  mapped — `TestTestPanicStackTwoPart` pins the shape.

## Residual

- The `ambiguous` answer is honest but coarse for editors: a
  squint at the union is right for diagnostics, less right for
  go-to-definition. The per-line re-pin emission (recorded in the
  decision) would kill the class; codegen emission is another
  lane's surface this wave.
- `ParseGoErrorLine` columns are generated-file columns
  reinterpreted as `.gom` columns — kept for display but not
  trusted for the span computation.
