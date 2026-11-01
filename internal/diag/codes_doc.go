package diag

import (
	"fmt"
	"strings"
)

// RenderCodeDoc renders the error-code table as the Markdown document
// published at docs/error-codes.md. The output is deterministic; the
// doc-sync test (TestErrorCodesDocInSync) keeps the checked-in file
// identical to this rendering, and "go generate ./internal/diag"
// rewrites it.
func RenderCodeDoc() string {
	var b strings.Builder

	b.WriteString(`# Error codes

<!-- Code generated from internal/diag/codes.go by "go generate ./internal/diag". DO NOT EDIT. -->

This file is generated from the error-code table in
[internal/diag/codes.go](../internal/diag/codes.go). Regenerate it with:

    go generate ./internal/diag

Diagnostic codes are stable identifiers (spec section 12): a code never
changes meaning and is never reused, even after the diagnostic it named
is removed — such codes stay in the table marked **retired**.

`)

	b.WriteString(`## Numbering scheme

A code is ` + "`LCCNN`" + ` — five characters:

- ` + "`L`" + ` — the severity: ` + "`E`" + ` error, ` + "`W`" + ` warning, ` + "`N`" + ` note.
- ` + "`CC`" + ` — a two-digit category naming the area of the language the
  diagnostic belongs to (see the table below). Categories are assigned
  once and never renumbered; they do not mirror the pipeline order.
- ` + "`NN`" + ` — a serial inside the category, allocated lowest-first.

To allocate a new code, take the lowest free serial in the right
category. If a category is exhausted, add a new category rather than
renumbering.

`)

	b.WriteString("## Categories\n\n")
	b.WriteString("| Prefix | Category | Scope |\n")
	b.WriteString("| --- | --- | --- |\n")
	for _, c := range categories {
		fmt.Fprintf(&b, "| `%sxx` | %s | %s |\n", c.Number, c.Title, c.Blurb)
	}

	for _, c := range categories {
		var entries []CodeInfo
		for _, info := range codeTable {
			if string(info.Code)[1:3] == c.Number {
				entries = append(entries, info)
			}
		}
		if len(entries) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n## %sxx — %s\n", c.Number, c.Title)
		for _, info := range entries {
			b.WriteString("\n### ")
			b.WriteString(string(info.Code))
			b.WriteString(" — ")
			b.WriteString(info.Title)
			b.WriteString("\n\n")
			var attrs []string
			attrs = append(attrs, "**Severity:** "+info.Severity.String())
			attrs = append(attrs, "**Phase:** "+string(info.Phase))
			if info.Spec != "" {
				attrs = append(attrs, "**Spec:** §"+info.Spec)
			}
			if info.Retired {
				attrs = append(attrs, "**Status:** retired")
			}
			b.WriteString(strings.Join(attrs, " · "))
			b.WriteString("\n\n")
			b.WriteString(strings.TrimSpace(info.Explanation))
			b.WriteString("\n")
			if info.Example != "" {
				b.WriteString("\n```gom\n")
				b.WriteString(strings.TrimSpace(info.Example))
				b.WriteString("\n```\n")
			}
		}
	}
	return b.String()
}
