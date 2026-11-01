# Verification report: decisions README sync generator (B-7)

- 目的: keep the index table of `docs/decisions/README.md` in sync with
  the decision files automatically, following the
  `internal/diag/codes_doc.go` precedent (render → `go:generate` →
  sync test).
- 確認日: 2026-11-02. Toolchain: go1.27.1.

## What was built

`internal/tool/decisionsdoc` — a repo-tooling package (not a compiler
phase, hence `internal/tool/`):

| File | Contents |
| --- | --- |
| `doc.go` | package comment describing the mechanism |
| `decisionsdoc.go` | `ScanDir` / `ParseDecision` (frontmatter parsing), `RenderTable`, `Render` (marked-region splice), `RenderDir`; the `//go:generate` line sits here, same position as `internal/diag/codes.go` |
| `decisionsdoc_test.go` | `TestDecisionsReadmeInSync` plus unit tests for the parser, table renderer, and splice |

## Deviation from the precedent: marked region, not whole-file

`codes_doc.go` regenerates `docs/error-codes.md` wholesale because the
prose there is generated reference material. `docs/decisions/README.md`
is different: its prose (ファイル名 / 状態 rules) is the workflow
document people edit by hand, so baking it into Go string literals
would make every rule edit a compiler-tree change. Instead the table
lives between `<!-- BEGIN GENERATED -->` / `<!-- END GENERATED -->`
markers and `Render` rewrites only that region, preserving the
surrounding prose byte for byte. The mechanism is otherwise identical:
`go generate ./internal/tool/decisionsdoc` runs the sync test with
`-update`; without it the test diffs the checked-in README against the
rendering.

## Column set and markers

`| ファイル | 決定 | 状態 |` — the old two columns plus 状態, the
field that drifted (`（P0 で決定）` labels had gone stale). Values come
verbatim from frontmatter: title from the `# ` line, 状態 from the
`- 状態:` bullet. A decision file linked from another file's
`- Supersedes:` line gets `（一部を X で置き換え）` appended to its 決定
cell ("一部を" only when every reference says `のうち`), which
reproduces the two existing manual markers and adds a third that had
been missed (juxtaposition-and-currying ← function-representation).
Rows sort by date then slug — the same order filenames sort into.

## Strictness (fail loudly, don't skip)

- Missing `# ` title, `- 状態:`, or `- 日付:` → error naming the file.
- `- 日付:` disagreeing with the filename date → error.
- A `Supersedes` link naming a nonexistent decision file → error.
- Frontmatter is scanned only up to the first `## ` heading, so a
  `- 状態:` bullet in the body cannot leak in.
- `|` in titles is escaped (`\|?>` renders correctly).

## Verification

- `go generate ./internal/tool/decisionsdoc` regenerated the README:
  all 32 files are indexed; the 7 missing `2026-11-02-*` rows and the
  missing `2026-11-01-formal-grammar-interpretations` row appeared;
  stale `（P0 で決定）` text was replaced by real titles and statuses.
- `TestDecisionsReadmeInSync` fails after a manual row edit and passes
  again after `go generate` — the intended maintenance loop.
- `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` and
  `make check` all pass.

## Author workflow now

Add `YYYY-MM-DD-<slug>.md` with the usual frontmatter and run
`go generate ./internal/tool/decisionsdoc` (or let CI's `go test`
fail and remind you). Nothing else — the table row, its 状態, and any
supersession markers are derived. On squash-merge conflicts in the
table region, keep both sides' decision files and regenerate.
