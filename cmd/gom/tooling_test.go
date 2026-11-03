package main

// Unit tests for the gom fix selection policy in tooling.go:
// planFixes picks the first applicable safe fix per diagnostic,
// skipping a whole fix when any of its edits overlaps an
// already-accepted edit or names a file outside the inputs. The
// real compiler cannot produce conflicting fixes today (E0402's
// `ignore (...)` wrap never overlaps), so these exercise the policy
// with synthetic diagnostics.

import (
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// safeFix builds a safe fix whose single edit replaces [start, end)
// in file by text.
func safeFix(title, file string, start, end int, text string) diag.Fix {
	return diag.Fix{
		Title:         title,
		Applicability: diag.ApplicabilitySafe,
		Edits: []diag.Edit{
			{Span: diag.SpanIn(file, start, end), Text: text},
		},
	}
}

// fixDiag returns an error diagnostic located at file:start-start,
// carrying the given fixes.
func fixDiag(code diag.Code, file string, start int, fixes ...diag.Fix) diag.Diagnostic {
	d := diag.Error(code, "boom").At(diag.Point(file, start))
	for _, f := range fixes {
		d = d.AddFix(f)
	}
	return d
}

// TestPlanFixesPicksFirstSafe: per diagnostic at most one fix
// applies — the first safe one; placeholder and unsafe candidates
// are skipped over.
func TestPlanFixesPicksFirstSafe(t *testing.T) {
	files := map[string]bool{"a.gom": true}
	unsafe := diag.Fix{
		Title:         "maybe wrong",
		Applicability: diag.ApplicabilityUnsafe,
		Edits:         []diag.Edit{{Span: diag.Point("a.gom", 0), Text: "u"}},
	}
	placeholder := diag.Fix{
		Title:         "fill the hole",
		Applicability: diag.ApplicabilityPlaceholder,
		Edits:         []diag.Edit{{Span: diag.Point("a.gom", 0), Text: "todo"}},
	}

	diags := diag.List{
		fixDiag("E0001", "a.gom", 0,
			unsafe, placeholder,
			safeFix("right one", "a.gom", 0, 0, "x"),
			safeFix("never reached", "a.gom", 1, 1, "y")),
		fixDiag("E0002", "a.gom", 5, placeholder, unsafe), // no safe fix
		fixDiag("E0003", "a.gom", 9),                      // no fixes at all
	}

	got := planFixes(diags, files)
	if len(got) != 1 {
		t.Fatalf("applied %d fixes, want 1: %v", len(got), got)
	}
	a := got[0]
	if a.Code != "E0001" || a.Title != "right one" {
		t.Errorf("applied = %+v, want E0001's first safe fix", a)
	}
}

// TestPlanFixesSkipsOverlapping: a fix whose edits collide with an
// already-accepted edit is skipped whole — even when only one of
// several edits conflicts.
func TestPlanFixesSkipsOverlapping(t *testing.T) {
	files := map[string]bool{"a.gom": true}
	diags := diag.List{
		fixDiag("E0001", "a.gom", 0,
			safeFix("first", "a.gom", 4, 8, "w")),
		fixDiag("E0002", "a.gom", 2,
			safeFix("colliding", "a.gom", 6, 10, "x")), // [6,10) overlaps "first"'s [4,8)
		fixDiag("E0003", "a.gom", 12,
			diag.Fix{
				Title:         "multi-edit",
				Applicability: diag.ApplicabilitySafe,
				Edits: []diag.Edit{
					{Span: diag.Point("a.gom", 20), Text: "i"},
					{Span: diag.SpanIn("a.gom", 7, 9), Text: "conflicts with first"},
				},
			}),
		fixDiag("E0004", "a.gom", 30,
			safeFix("adjacent ok", "a.gom", 8, 12, "edge")), // starts where "first" ends — no overlap
	}

	got := planFixes(diags, files)
	var titles []string
	for _, a := range got {
		titles = append(titles, a.Title)
	}
	want := []string{"first", "adjacent ok"}
	if len(titles) != len(want) {
		t.Fatalf("applied titles = %v, want %v", titles, want)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("applied titles = %v, want %v", titles, want)
		}
	}
}

// TestPlanFixesSkipsForeignFile: a fix is skipped whole when any of
// its edits names a file that is not among the inputs.
func TestPlanFixesSkipsForeignFile(t *testing.T) {
	files := map[string]bool{"a.gom": true}
	diags := diag.List{
		fixDiag("E0001", "a.gom", 0,
			diag.Fix{
				Title:         "touches b.gom",
				Applicability: diag.ApplicabilitySafe,
				Edits: []diag.Edit{
					{Span: diag.Point("a.gom", 1), Text: "x"},
					{Span: diag.Point("b.gom", 1), Text: "y"},
				},
			},
			safeFix("local fallback", "a.gom", 1, 1, "z")), // insert at 1
	}

	got := planFixes(diags, files)
	if len(got) != 1 || got[0].Title != "local fallback" {
		t.Fatalf("applied = %v, want only the fallback fix", got)
	}
}

// TestPlanFixesRejectsBadFixes: an empty-edits or invalid-span fix
// cannot apply; the next safe candidate is tried instead.
func TestPlanFixesRejectsBadFixes(t *testing.T) {
	files := map[string]bool{"a.gom": true}
	diags := diag.List{
		fixDiag("E0001", "a.gom", 0,
			diag.Fix{Title: "empty", Applicability: diag.ApplicabilitySafe},
			diag.Fix{
				Title:         "bad span",
				Applicability: diag.ApplicabilitySafe,
				Edits:         []diag.Edit{{Span: diag.Span{File: "a.gom", Start: 9, End: 2}, Text: "x"}},
			},
			safeFix("good", "a.gom", 0, 3, "x")),
	}

	got := planFixes(diags, files)
	if len(got) != 1 || got[0].Title != "good" {
		t.Fatalf("applied = %v, want only \"good\"", got)
	}
}
