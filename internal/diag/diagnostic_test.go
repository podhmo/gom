package diag_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

func TestSeverityString(t *testing.T) {
	tests := []struct {
		sev  diag.Severity
		want string
	}{
		{diag.SeverityError, "error"},
		{diag.SeverityWarning, "warning"},
		{diag.SeverityNote, "note"},
		{diag.Severity(0), "Severity(0)"},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(tt.sev), got, tt.want)
		}
	}
}

func TestApplicabilityString(t *testing.T) {
	tests := []struct {
		a    diag.Applicability
		want string
	}{
		{diag.ApplicabilitySafe, "safe"},
		{diag.ApplicabilityPlaceholder, "placeholder"},
		{diag.ApplicabilityUnsafe, "unsafe"},
		{diag.Applicability(0), "Applicability(0)"},
	}
	for _, tt := range tests {
		if got := tt.a.String(); got != tt.want {
			t.Errorf("Applicability(%d).String() = %q, want %q", int(tt.a), got, tt.want)
		}
	}
}

func TestConstructors(t *testing.T) {
	tests := []struct {
		name string
		d    diag.Diagnostic
		sev  diag.Severity
	}{
		{"Error", diag.Error("E0301", "non-exhaustive match on `Expr`"), diag.SeverityError},
		{"Warning", diag.Warning("W0001", "unused variable `x`"), diag.SeverityWarning},
		{"Note", diag.Note("N0001", "match is exhaustive"), diag.SeverityNote},
		{"New", diag.New(diag.SeverityWarning, "W0002", "msg"), diag.SeverityWarning},
	}
	for _, tt := range tests {
		if tt.d.Severity != tt.sev {
			t.Errorf("%s: Severity = %v, want %v", tt.name, tt.d.Severity, tt.sev)
		}
		if tt.d.Message == "" || tt.d.Code == "" {
			t.Errorf("%s: missing message or code: %+v", tt.name, tt.d)
		}
		if err := tt.d.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v, want nil", tt.name, err)
		}
	}
}

// TestFluentBuilder builds the diagnostic of the spec's E0301 example
// (docs/spec.md section 12) field by field.
func TestFluentBuilder(t *testing.T) {
	fix := diag.Fix{
		Title:         "add the missing arms",
		Applicability: diag.ApplicabilityPlaceholder,
		Edits: []diag.Edit{
			{Span: diag.Point("eval.gom", 226), Text: "  Neg (Num _) -> todo ()\n"},
		},
	}

	d := diag.Error("E0301", "non-exhaustive match on `Expr`").
		At(diag.SpanIn("eval.gom", 41, 228)).
		Set("scrutinee_type", "Expr").
		Set("missing_patterns", []string{"Neg (Num _)", "Let _ _ _"}).
		AddNote("arms with guards do not count toward exhaustiveness").
		AddFix(fix).
		From(diag.SourceGo)

	if d.Severity != diag.SeverityError || d.Code != "E0301" {
		t.Errorf("severity/code = %v/%q", d.Severity, d.Code)
	}
	if d.Span != diag.SpanIn("eval.gom", 41, 228) {
		t.Errorf("Span = %+v", d.Span)
	}
	if d.Source != diag.SourceGo {
		t.Errorf("Source = %q, want %q", d.Source, diag.SourceGo)
	}
	if got := d.Data["scrutinee_type"]; got != "Expr" {
		t.Errorf("Data[scrutinee_type] = %v", got)
	}
	if got := len(d.Notes); got != 1 {
		t.Errorf("len(Notes) = %d, want 1", got)
	}
	if got := len(d.Fixes); got != 1 || d.Fixes[0].Title != fix.Title {
		t.Errorf("Fixes = %+v", d.Fixes)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestValidate(t *testing.T) {
	validSpan := diag.SpanIn("a.gom", 0, 5)
	validFix := diag.Fix{
		Title:         "insert a comma",
		Applicability: diag.ApplicabilitySafe,
		Edits:         []diag.Edit{{Span: diag.Point("a.gom", 3), Text: ","}},
	}

	tests := []struct {
		name    string
		d       diag.Diagnostic
		wantErr string // substring of the expected error; "" means valid
	}{
		{
			name: "minimal error diagnostic",
			d:    diag.Error("E0001", "boom"),
		},
		{
			name: "no source location is allowed",
			d:    diag.Error("E0001", "boom").At(diag.Span{}),
		},
		{
			name: "full diagnostic",
			d: diag.Error("E0001", "boom").
				At(validSpan).AddNote("see also x").AddFix(validFix),
		},
		{
			name:    "invalid severity",
			d:       diag.New(diag.Severity(0), "E0001", "boom"),
			wantErr: "invalid severity",
		},
		{
			name:    "empty code",
			d:       diag.Error("", "boom"),
			wantErr: "empty code",
		},
		{
			name:    "empty message",
			d:       diag.Error("E0001", ""),
			wantErr: "empty message",
		},
		{
			name:    "multi-line message",
			d:       diag.Error("E0001", "line one\nline two"),
			wantErr: "single line",
		},
		{
			name:    "inverted span",
			d:       diag.Error("E0001", "boom").At(diag.SpanIn("a.gom", 5, 0)),
			wantErr: "invalid span",
		},
		{
			name:    "span without file",
			d:       diag.Error("E0001", "boom").At(diag.Span{Start: 1, End: 2}),
			wantErr: "invalid span",
		},
		{
			name: "fix without title",
			d: diag.Error("E0001", "boom").AddFix(diag.Fix{
				Applicability: diag.ApplicabilitySafe,
				Edits:         []diag.Edit{{Span: diag.Point("a.gom", 0), Text: "x"}},
			}),
			wantErr: "empty title",
		},
		{
			name: "fix with invalid applicability",
			d: diag.Error("E0001", "boom").AddFix(diag.Fix{
				Title:         "fix it",
				Applicability: diag.Applicability(0),
				Edits:         []diag.Edit{{Span: diag.Point("a.gom", 0), Text: "x"}},
			}),
			wantErr: "invalid applicability",
		},
		{
			name: "fix edit without file",
			d: diag.Error("E0001", "boom").AddFix(diag.Fix{
				Title:         "fix it",
				Applicability: diag.ApplicabilitySafe,
				Edits:         []diag.Edit{{Span: diag.Span{Start: 0, End: 0}, Text: "x"}},
			}),
			wantErr: "invalid span",
		},
	}

	for _, tt := range tests {
		err := tt.d.Validate()
		if tt.wantErr == "" {
			if err != nil {
				t.Errorf("%s: Validate() = %v, want nil", tt.name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: Validate() = nil, want error containing %q", tt.name, tt.wantErr)
		} else if !strings.Contains(err.Error(), tt.wantErr) {
			t.Errorf("%s: Validate() = %q, want substring %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestList(t *testing.T) {
	var l diag.List
	l.Add(diag.Warning("W0001", "unused `y`").At(diag.SpanIn("b.gom", 5, 6)))
	l.Add(diag.Error("E0002", "type mismatch").At(diag.SpanIn("a.gom", 10, 11)))
	l.Add(diag.Error("E0001", "unexpected token").At(diag.SpanIn("a.gom", 2, 3)))
	l.Add(diag.Note("N0001", "see docs").At(diag.SpanIn("a.gom", 2, 3)))

	if !l.HasError() {
		t.Error("HasError() = false, want true")
	}
	if got := l.MaxSeverity(); got != diag.SeverityError {
		t.Errorf("MaxSeverity() = %v, want %v", got, diag.SeverityError)
	}
	if errs, warns := l.Counts(); errs != 2 || warns != 1 {
		t.Errorf("Counts() = (%d, %d), want (2, 1)", errs, warns)
	}

	l.Sort()
	wantOrder := []diag.Code{"E0001", "N0001", "E0002", "W0001"}
	for i, want := range wantOrder {
		if l[i].Code != want {
			t.Fatalf("after Sort, l[%d].Code = %q, want %q (full order: %v)",
				i, l[i].Code, want, l)
		}
	}
}

func TestListEmpty(t *testing.T) {
	var l diag.List
	if l.HasError() {
		t.Error("HasError() = true on empty list")
	}
	if got := l.MaxSeverity(); got != 0 {
		t.Errorf("MaxSeverity() = %v, want 0", got)
	}
	if errs, warns := l.Counts(); errs != 0 || warns != 0 {
		t.Errorf("Counts() = (%d, %d), want (0, 0)", errs, warns)
	}
	l.Sort() // must not panic
}

// TestDerivedDiagnosticsAreIndependent is a regression test for the
// fluent builder: Diagnostic is a value type whose Data/Notes/Fixes
// fields are reference types, so deriving multiple diagnostics from a
// common base must clone before writing — otherwise appends overwrite
// each other's elements and map writes leak across copies.
func TestDerivedDiagnosticsAreIndependent(t *testing.T) {
	base := diag.Error("E0001", "boom").
		Set("shared", "base").
		AddNote("n1").
		AddNote("n2").
		AddNote("n3")

	a := base.AddNote("A").Set("branch", "a")
	b := base.AddNote("B").Set("branch", "b")

	// The base is untouched.
	if got := len(base.Notes); got != 3 {
		t.Fatalf("len(base.Notes) = %d, want 3: %v", got, base.Notes)
	}
	if _, ok := base.Data["branch"]; ok {
		t.Errorf("base.Data gained \"branch\": %v", base.Data)
	}

	// Each derived diagnostic keeps its own note, not its sibling's.
	if want := []string{"n1", "n2", "n3", "A"}; !slices.Equal(a.Notes, want) {
		t.Errorf("a.Notes = %v, want %v", a.Notes, want)
	}
	if want := []string{"n1", "n2", "n3", "B"}; !slices.Equal(b.Notes, want) {
		t.Errorf("b.Notes = %v, want %v", b.Notes, want)
	}

	// Data entries do not leak between base, a and b, and later writes
	// through one copy do not reach the others.
	if a.Data["shared"] != "base" || a.Data["branch"] != "a" {
		t.Errorf("a.Data = %v", a.Data)
	}
	if b.Data["shared"] != "base" || b.Data["branch"] != "b" {
		t.Errorf("b.Data = %v", b.Data)
	}
	a.Data["shared"] = "mutated"
	if b.Data["shared"] != "base" || base.Data["shared"] != "base" {
		t.Errorf("write to a.Data leaked: b=%v base=%v", b.Data, base.Data)
	}

	// Fixes behave like Notes.
	bf := base.AddFix(diag.Fix{Title: "fix from base", Applicability: diag.ApplicabilitySafe})
	cf := base.AddFix(diag.Fix{Title: "fix from base too", Applicability: diag.ApplicabilityUnsafe})
	if bf.Fixes[0].Title != "fix from base" || cf.Fixes[0].Title != "fix from base too" {
		t.Errorf("fixes leaked: bf=%v cf=%v", bf.Fixes, cf.Fixes)
	}
	if len(base.Fixes) != 0 {
		t.Errorf("base.Fixes = %v, want empty", base.Fixes)
	}
}

func TestListSortBySeverityWithinSamePosition(t *testing.T) {
	// At the same position, errors precede warnings and notes.
	var l diag.List
	l.Add(diag.Note("N0001", "n").At(diag.Point("a.gom", 0)))
	l.Add(diag.Error("E0001", "e").At(diag.Point("a.gom", 0)))
	l.Add(diag.Warning("W0001", "w").At(diag.Point("a.gom", 0)))
	l.Sort()
	if l[0].Severity != diag.SeverityError || l[1].Severity != diag.SeverityWarning ||
		l[2].Severity != diag.SeverityNote {
		t.Errorf("severity order = %v, %v, %v; want error, warning, note",
			l[0].Severity, l[1].Severity, l[2].Severity)
	}
}

func TestListSortPositionlessFirst(t *testing.T) {
	// Diagnostics without a location sort before located ones.
	var l diag.List
	l.Add(diag.Error("E0002", "located").At(diag.Point("a.gom", 0)))
	l.Add(diag.Error("E0001", "no location"))
	l.Sort()
	if !l[0].Span.IsZero() || l[1].Span.IsZero() {
		t.Errorf("order = %+v, %+v; want positionless first", l[0].Span, l[1].Span)
	}
}
