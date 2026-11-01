// Package decisionsdoc keeps the index table of docs/decisions/README.md
// in sync with the decision files in docs/decisions/. It scans the
// frontmatter of each YYYY-MM-DD-<slug>.md file (the "# title" line and
// the "- 状態:" / "- 日付:" / "- Supersedes:" bullets) and renders the
// table rows between the README's <!-- BEGIN GENERATED --> and
// <!-- END GENERATED --> markers; prose outside the markers is left
// untouched.
//
// The mechanism mirrors internal/diag's error-code doc
// (internal/diag/codes_doc.go): "go generate ./internal/tool/decisionsdoc"
// rewrites the table through the -update flag of
// TestDecisionsReadmeInSync, and the same test fails when the checked-in
// README drifts from the rendering. A malformed decision file — a
// missing title, 状態, or 日付 line, or a 日付 that disagrees with the
// filename — is a hard error, not a skipped row.
package decisionsdoc
