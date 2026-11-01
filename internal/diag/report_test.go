package diag_test

import (
	"encoding/json"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

func TestReportExitCode(t *testing.T) {
	for _, tt := range []struct {
		name  string
		diags []diag.Diagnostic
		want  int
	}{
		{"empty", nil, diag.ExitSuccess},
		{"warnings only", []diag.Diagnostic{diag.Warning("W0001", "w")}, diag.ExitSuccess},
		{"notes only", []diag.Diagnostic{diag.Note("N0001", "n")}, diag.ExitSuccess},
		{"warning and note", []diag.Diagnostic{
			diag.Warning("W0001", "w"), diag.Note("N0001", "n")}, diag.ExitSuccess},
		{"one error", []diag.Diagnostic{diag.Error("E0001", "e")}, diag.ExitError},
		{"error among others", []diag.Diagnostic{
			diag.Warning("W0001", "w"), diag.Error("E0001", "e")}, diag.ExitError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rep := diag.Report{Diagnostics: tt.diags}
			if got := rep.ExitCode(); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestExitCodeConstants(t *testing.T) {
	// The spec fixes the exit-code values (docs/spec.md section 12).
	if diag.ExitSuccess != 0 || diag.ExitError != 1 || diag.ExitInternal != 2 {
		t.Errorf("exit codes = %d/%d/%d, want 0/1/2",
			diag.ExitSuccess, diag.ExitError, diag.ExitInternal)
	}
}

func TestReportSummary(t *testing.T) {
	rep := diag.Report{
		Diagnostics: []diag.Diagnostic{
			diag.Error("E0002", "e2"),
			diag.Error("E0001", "e1"),
			diag.Warning("W0001", "w"),
			diag.Note("N0001", "n"), // notes are not counted
		},
		Files:   []string{"b.gom", "a.gom", "b.gom"},
		Outputs: []string{"b.go", "a.go", "b.go"},
	}
	s := rep.Summary()
	if s.OK || s.Errors != 2 || s.Warnings != 1 {
		t.Errorf("Summary = %+v, want ok=false errors=2 warnings=1", s)
	}
	if got := s.Files; len(got) != 2 || got[0] != "a.gom" || got[1] != "b.gom" {
		t.Errorf("Files = %v, want [a.gom b.gom] (sorted, deduplicated)", got)
	}
	// Outputs keep the caller's order, deduplicated.
	if got := s.Outputs; len(got) != 2 || got[0] != "b.go" || got[1] != "a.go" {
		t.Errorf("Outputs = %v, want [b.go a.go] (order kept, deduplicated)", got)
	}
}

func TestReportSummaryClean(t *testing.T) {
	s := diag.Report{Files: []string{"a.gom"}}.Summary()
	if !s.OK || s.Errors != 0 || s.Warnings != 0 {
		t.Errorf("Summary = %+v, want ok=true 0/0", s)
	}
	if len(s.Outputs) != 0 {
		t.Errorf("Outputs = %v, want empty", s.Outputs)
	}
}

// TestSummaryMarshalJSON pins the wire shape of the summary object to
// the spec's example (docs/spec.md section 12).
func TestSummaryMarshalJSON(t *testing.T) {
	s := diag.Summary{OK: false, Errors: 1, Warnings: 0,
		Files: []string{"eval.gom"}, Outputs: []string{}}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"summary","ok":false,"errors":1,"warnings":0,"files":["eval.gom"],"outputs":[]}`
	if string(raw) != want {
		t.Errorf("marshal =\n%s\nwant\n%s", raw, want)
	}
}

// TestSummaryMarshalJSONEmpty verifies files/outputs marshal as empty
// arrays rather than null or absent.
func TestSummaryMarshalJSONEmpty(t *testing.T) {
	raw, err := json.Marshal(diag.Summary{OK: true})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"summary","ok":true,"errors":0,"warnings":0,"files":[],"outputs":[]}`
	if string(raw) != want {
		t.Errorf("marshal =\n%s\nwant\n%s", raw, want)
	}
}

func TestKindConstants(t *testing.T) {
	// The spec fixes the kind vocabulary (docs/spec.md section 12).
	want := map[diag.Kind]string{
		diag.KindDiagnostic:  "diagnostic",
		diag.KindSummary:     "summary",
		diag.KindTest:        "test",
		diag.KindUnformatted: "unformatted",
		diag.KindApplied:     "applied",
		diag.KindExplanation: "explanation",
	}
	for k, v := range want {
		if string(k) != v {
			t.Errorf("Kind = %q, want %q", string(k), v)
		}
	}
}
