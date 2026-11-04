# `gom test` renders panic stacks with `.gom` positions and keeps the raw Go trace

- 状態: 採用（実装は TODO）
- 日付: 2026-11-05

## Context

open-questions asked whether `gom test` should redraw panic stacks with
`.gom` positions in user-facing output. Today the mapping exists only as
the internal `GoPosIndex.ResolveStack` API (design.md 2.2); a failed
test's `output` and the package-level E0001 `output` carry the raw
`go test` text.

## Decision

- A panic or goroutine dump in `gom test` output is rendered as a
  `.gom`-position trace first: each frame goes through `ResolveStack`,
  resolvable frames get the `.gom` position, and frames that do not
  resolve (toolchain, `rt`, hand-written Go) keep their raw `file:line`
  text. The raw Go stack trace is then printed verbatim after it —
  both halves appear by default.
- `gom test -q` / `gom test --quiet` suppresses the raw Go trace. The
  rendered `.gom`-position trace still appears.
- Rendering is fail-soft: a conversion that fails partway never removes
  the Go trace. Unresolvable frames stay verbatim inside the rendered
  section and the raw dump is unaffected.

Rationale for keeping both: a probabilistic bug that cannot be
reproduced has to be diagnosed from whatever the one failing run
captured — PC offsets, goroutine headers, frames the renderer could not
classify. Dropping the raw trace would lose exactly that information.

## Alternatives

- *Rendered trace only*: smaller output, but a flaky failure loses the
  raw frames precisely when they are hardest to reproduce.
- *Raw trace only* (status quo): faithful to `go test`, but the user
  maps `//line`-adjusted positions back to `.gom` by hand.

## Consequences

- spec.md section 12 documents the two-part panic output and `-q` /
  `--quiet` on `gom test`.
- Work lands in TODO.md: the panic-output paths of `runGoTests`
  (`TestResult.Output` and the package-level failure `output`) gain the
  rendered section via `GoPosIndex.ResolveStack`, and `gom test` gains
  the `-q`/`--quiet` flag.
