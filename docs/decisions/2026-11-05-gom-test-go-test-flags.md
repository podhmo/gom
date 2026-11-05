# `gom test` passes the arguments after `--` through to `go test`

- 状態: 採用（prototype verified — [sketch](../sketch/2026-11-05-gom-test-go-test-flags.md)）
- 日付: 2026-11-05

## Context

`gom test` shells out to plain `go test -json` once per input
directory, so flags like `-race`, `-run`, `-count` or `-timeout`
cannot reach the generated test code — P5-3 verified `-race` by
hand-running `go test -race` on `gom test`'s output
(docs/sketch/2026-11-05-p5-3-generated-code-perf.md). TODO.md
lists the gap as an open task.

## Decision

`gom test` splits its argument list at the first `--`: arguments
before it are gom's own flags and input files/directories;
everything after passes verbatim to the `go test` invocation —
`go test -json <passthrough args>` in each input directory. The
same arguments go to every directory's run; there is no
per-package flag selection.

- `-json` is gom's own channel and is passed first. A passthrough
  flag that defeats it (`-json=false`) breaks the result stream
  the same way a malformed flag does — a `source: "go"` E0001
  diagnostic, not a gom usage error.
- Unknown or malformed passthrough flags surface through the
  existing go-tool failure paths (E0001 "go test failed before
  running tests" or an undecodable stream), not through gom's
  flag parser — gom does not re-validate go test's flag surface.
- Test selection is via go test's own flags (`gom test -- -run
  Bump`); non-flag passthrough arguments are package patterns in
  `go test`'s sense and equally verbatim.
- Flag parsing inside gom is unchanged: `--` is split before the
  FlagSet sees the arguments, so gom's positional inputs never
  include go test's flags.

## Alternatives

- *A dedicated string flag* (`gom test -gotestflags "-race
  -count=1"`): quoting and escaping burden, and the string has
  to be re-split — `--` is the established passthrough
  convention (`cargo test --`, `npm test --`, `go run` args).
- *First-class gom flags* (`gom test -race`): duplicates a
  subset of go test's flag surface forever and collides with
  gom's own flag space (`-format` today, more later).

## Consequences

- spec.md section 12 documents the `--` passthrough and the
  `-json` caveat.
- `build.RunGoTest` gains a variadic args parameter.
