# Gist CLI as an executable Gom integration use case

- 状態: 採用
- 日付: 2026-11-03

## Proposal

Keep argument parsing, new/update selection, Gist URL validation, file traversal,
duplicate-name rejection, JSON object assembly, API routes/headers/status handling,
and CLI output/error policy in Gom. Use a small example-local Go adapter for
currently unavailable byte/string I/O, argument indexing, HTTP struct fields and
interface conversion, JSON string escaping, and JSON object field decoding.
The adapter must contain no Gist routes, request schema, or create/update policy.

Use `--gist https://gist.github.com/[owner/]ID` for update and positional file
paths for content. Basenames are keys; duplicate keys are input errors. PATCH
contains only supplied files so unspecified remote files remain unchanged.
Creation is secret by default (`public: false`). `--api-url` injects the fake
server endpoint; tests never contact GitHub. Success stdout contains one URL.

This is an example integration boundary, not an extension of the Gom language.
Prototype validation must expose and record compiler gaps rather than silently
moving application behavior into Go. See the forthcoming sketch for API sources,
minimal reproductions, and executable verification.

## Prototype

`examples/gist-cli/main.gom` and the example-local host adapter compile and run.
The root-discovered integration test builds the generated executable and checks
new/update behavior against local fake servers. Compiler blockers and evidence
are recorded in [the sketch](../sketch/2026-11-03-gist-cli-prototype.md).

## Outcome

Adopt the example-local boundary after executing the new/update and failure
scenarios against fake servers (including race detection). The generic host
primitives remain explicit and tracked for replacement; they do not introduce
language semantics or hide Gist policy. The verification report records the
source/adapter scope and the newly discovered void-Go-call compiler defect.
