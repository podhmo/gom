# testdata

Test fixtures for the gom compiler (docs/design.md section 3), run by
the `internal/gomtest` harness:

- `spec/` — one `.gom` file per spec rule, together with the expected
  diagnostics as JSON
- `golden/` — golden files of the Go code produced by codegen
- `dump/` — phase-IR dump goldens
- `fmt/` — formatter inputs and their expected outputs
- `sumtype/` — canary module for the go-check-sumtype CI step:
  `nonexhaustive/` MUST keep failing the check (scripts/check-sumtype.sh)
- `multifile/` — scratch Go module for the multi-`.gom` package +
  `.gomi` import end-to-end test
- `gocall/` — scratch Go module for the Go-calls-Gom end-to-end test
  (the `geom` fixture exercises the generated API from Go tests; the
  `boundary`/`box` fixture pair probes cross-boundary aliasing —
  see docs/sketch/2026-11-04-boundary-aliasing-hypotheses.md)
- `realworld/` — scratch module of small-but-real `package main`
  programs exercising the Go-interop boundary (os/io/strconv/flag/
  encoding/json+csv/net/http/time), run end-to-end by
  `cmd/gom`'s realworld test through check → build → go build → run
- `goiface/` — Go-side fixture package for `internal/goimport` and
  boundary tests
- `voidhost/` — host-side fixture for `internal/build` void tests

## Case layout

Suites are scanned recursively for `*.gom` files; subdirectories group
cases and become nested subtests. Files and directories starting with
`_` or `.` are helpers, not cases. For a case `<name>.gom`:

- spec suite: `<name>.expected.json` is `{"diagnostics": [...]}` where
  each element uses the diagnostic object shape of spec.md section 12.
  Only the fields present in an element are checked; `data` matches by
  subset and `notes`/`fixes` by ordered subsequence. See
  `internal/gomtest/doc.go` for the full semantics.
- golden suite: `<name>.golden.go` holds the generated Go source,
  compared literally after newline normalization.
- either suite: an optional `<name>.case.json` manifest marks the case
  skipped — `{"skip": "<reason>"}` reports the reason via `t.Skip`.
  Skipped cases keep their expectations in the tree, so a case can be
  committed before the feature it tests exists.

`go test -update` rewrites the expected files from the current output
(skipped cases are untouched); review the diff before committing.

The `.gitkeep` file under `fmt/` is a placeholder so git tracks the
directory until real fixtures land.
