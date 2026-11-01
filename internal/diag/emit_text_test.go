package diag_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// textOf renders rep in the default (plain) text format, failing on error.
func textOf(t *testing.T, rep diag.Report, res diag.Resolver) string {
	t.Helper()
	var buf bytes.Buffer
	if err := rep.WriteText(&buf, res, diag.TextOptions{}); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	return buf.String()
}

// TestWriteTextSingleLine pins the layout of a diagnostic on one
// source line: header, arrow, gutter, source line, underline, trailer.
func TestWriteTextSingleLine(t *testing.T) {
	src := []byte("let x = 1 + \"a\"\n")
	start := strings.Index(string(src), "\"a\"")
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0401", "type mismatch").At(diag.SpanIn("f.gom", start, start+3)),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `error[E0401]: type mismatch
  --> f.gom:1:13
   |
1  | let x = 1 + "a"
   |             ^^^

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextMultiLine covers a span crossing several lines: each
// covered line is shown with the part inside the span underlined, and
// the gutter widens with the line numbers.
func TestWriteTextMultiLine(t *testing.T) {
	src := []byte("fn f =\n  match e {\n    Num -> 1\n  }\n")
	start := strings.Index(string(src), "match")
	end := strings.Index(string(src), "}") + 1
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0301", "non-exhaustive match on `Expr`").
			At(diag.SpanIn("f.gom", start, end)).
			AddNote("arms with guards do not count toward exhaustiveness"),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `error[E0301]: non-exhaustive match on ` + "`Expr`" + `
  --> f.gom:2:3
   |
2  |   match e {
   |   ^^^^^^^^^
3  |     Num -> 1
   | ^^^^^^^^^^^^
4  |   }
   | ^^^
   |
   = note: arms with guards do not count toward exhaustiveness

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextWideGutter checks the layout when line numbers reach
// two digits: the structural marks shift one column with the gutter.
func TestWriteTextWideGutter(t *testing.T) {
	lines := make([]string, 11)
	for i := range lines {
		lines[i] = "x"
	}
	lines[10] = "bad x"
	src := []byte(strings.Join(lines, "\n") + "\n")
	start := strings.Index(string(src), "bad")
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Warning("W0001", "suspicious").At(diag.SpanIn("f.gom", start, start+3)),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `warning[W0001]: suspicious
   --> f.gom:11:1
    |
11  | bad x
    | ^^^

warning: 1 warning emitted
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextPointSpan: a zero-width span underlines one caret at
// the position.
func TestWriteTextPointSpan(t *testing.T) {
	src := []byte("abcdef\n")
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "here").At(diag.Point("f.gom", 2)),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `error[E0001]: here
  --> f.gom:1:3
   |
1  | abcdef
   |   ^

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextSpanEndAtLineStart: a span ending at a line break
// underlines through the end of its last content line without
// spilling onto the next line (the end offset is exclusive).
func TestWriteTextSpanEndAtLineStart(t *testing.T) {
	src := []byte("ab\ncd\nef\n")
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "x").At(diag.SpanIn("f.gom", 3, 6)), // "cd\n"
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `error[E0001]: x
  --> f.gom:2:1
   |
2  | cd
   | ^^^

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextNonASCII: underlines follow code-point columns; the
// caret for あ sits under the rune, not its byte position.
func TestWriteTextNonASCII(t *testing.T) {
	src := []byte("let s = \"あいう\"\n")
	start := bytes.Index(src, []byte("あ"))
	end := bytes.Index(src, []byte("\"\n"))
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "x").At(diag.SpanIn("f.gom", start, end)),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `error[E0001]: x
  --> f.gom:1:10
   |
1  | let s = "あいう"
   |          ^^^

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextTab: tabs display as four spaces and the underline
// still lands under the marked rune.
func TestWriteTextTab(t *testing.T) {
	src := []byte("f\tx\n")
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "x").At(diag.SpanIn("f.gom", 2, 3)),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `error[E0001]: x
  --> f.gom:1:3
   |
1  | f    x
   |      ^

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextPositionless: a diagnostic without a location renders
// header, notes and fixes but no arrow or snippet.
func TestWriteTextPositionless(t *testing.T) {
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0203", "cannot resolve import \"no/such/pkg\"").
			AddNote("the package was not found").
			AddFix(diag.Fix{Title: "check the import path", Applicability: diag.ApplicabilitySafe}),
	}}
	res := diag.NewMapResolver(nil)

	want := `error[E0203]: cannot resolve import "no/such/pkg"
   = note: the package was not found
   = help: check the import path

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextNotesAndFixes: multi-line notes align under the first
// line, and non-safe fixes show their applicability.
func TestWriteTextNotesAndFixes(t *testing.T) {
	src := []byte("x\n")
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "x").
			At(diag.SpanIn("f.gom", 0, 1)).
			AddNote("first line\nsecond line").
			AddFix(diag.Fix{Title: "poke it", Applicability: diag.ApplicabilityUnsafe}).
			AddFix(diag.Fix{Title: "fill the hole", Applicability: diag.ApplicabilityPlaceholder}),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})

	want := `error[E0001]: x
  --> f.gom:1:1
   |
1  | x
   | ^
   |
   = note: first line
           second line
   = help: poke it (unsafe)
   = help: fill the hole (placeholder)

error: aborting due to previous error
`
	if got := textOf(t, rep, res); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestWriteTextTrailers pins the closing line for each report shape.
func TestWriteTextTrailers(t *testing.T) {
	for _, tt := range []struct {
		name  string
		diags []diag.Diagnostic
		want  string
	}{
		{"one error", []diag.Diagnostic{diag.Error("E0001", "x")},
			"error: aborting due to previous error\n"},
		{"two errors", []diag.Diagnostic{diag.Error("E0001", "x"), diag.Error("E0002", "y")},
			"error: aborting due to 2 previous errors\n"},
		{"error and warning", []diag.Diagnostic{diag.Error("E0001", "x"), diag.Warning("W0001", "w")},
			"error: aborting due to previous error; 1 warning emitted\n"},
		{"error and warnings", []diag.Diagnostic{diag.Error("E0001", "x"), diag.Warning("W0001", "w"), diag.Warning("W0002", "v")},
			"error: aborting due to previous error; 2 warnings emitted\n"},
		{"warnings only", []diag.Diagnostic{diag.Warning("W0001", "w"), diag.Warning("W0002", "v")},
			"warning: 2 warnings emitted\n"},
		{"one warning", []diag.Diagnostic{diag.Warning("W0001", "w")},
			"warning: 1 warning emitted\n"},
		{"clean", nil, "success\n"},
		{"note does not fail", []diag.Diagnostic{diag.Note("N0001", "n")}, "success\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rep := diag.Report{Diagnostics: tt.diags}
			got := textOf(t, rep, diag.NewMapResolver(nil))
			if !strings.HasSuffix(got, tt.want) {
				t.Errorf("trailer = %q, want suffix %q", got, tt.want)
			}
			// Exactly one trailing line: the text before it is the
			// diagnostics (or nothing when clean).
			head := strings.TrimSuffix(got, tt.want)
			if tt.name == "clean" {
				if head != "" {
					t.Errorf("clean report should emit only the trailer: %q", got)
				}
			} else if !strings.HasSuffix(head, "\n\n") {
				t.Errorf("a blank line should precede the trailer: %q", got)
			}
		})
	}
}

// TestWriteTextOrderingAndSeparation: diagnostics emit in List.Sort
// order separated by blank lines; the report order is untouched.
func TestWriteTextOrderingAndSeparation(t *testing.T) {
	src := []byte("ab\ncd\n")
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0002", "later").At(diag.SpanIn("f.gom", 3, 4)),
		diag.Error("E0001", "earlier").At(diag.SpanIn("f.gom", 0, 1)),
	}}
	got := textOf(t, rep, res)
	first := strings.Index(got, "error[E0001]")
	second := strings.Index(got, "error[E0002]")
	if first < 0 || second < 0 || first > second {
		t.Errorf("diagnostics out of order:\n%s", got)
	}
	between := got[first:second]
	if !strings.Contains(between, "\n\n") {
		t.Errorf("diagnostics should be separated by a blank line:\n%s", got)
	}
	if rep.Diagnostics[0].Code != "E0002" {
		t.Error("WriteText reordered the report's diagnostics")
	}
}

// TestWriteTextColor checks that Color emits ANSI styling on the
// header, marks and carets, and nothing when disabled (covered by the
// other tests' byte-exact output).
func TestWriteTextColor(t *testing.T) {
	src := []byte("x\n")
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "x").At(diag.SpanIn("f.gom", 0, 1)),
	}}
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})
	var buf bytes.Buffer
	if err := rep.WriteText(&buf, res, diag.TextOptions{Color: true}); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.Contains(got, "\x1b[1m\x1b[31merror[E0001]\x1b[0m") {
		t.Errorf("header should be bold red: %q", got)
	}
	if !strings.Contains(got, "\x1b[31m^\x1b[0m") && !strings.Contains(got, "1m\x1b[31m^\x1b[0m") {
		t.Errorf("caret should be red: %q", got)
	}
	if strings.Contains(got, "error[E0001]:") {
		t.Errorf("severity word should be wrapped in ANSI: %q", got)
	}
}

// TestWriteTextEmpty emits just the success trailer.
func TestWriteTextEmpty(t *testing.T) {
	got := textOf(t, diag.Report{}, diag.NewMapResolver(nil))
	if got != "success\n" {
		t.Errorf("got %q, want \"success\\n\"", got)
	}
}

func TestWriteTextUnresolvableSpan(t *testing.T) {
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "x").At(diag.SpanIn("missing.gom", 0, 1)),
	}}
	var buf bytes.Buffer
	err := rep.WriteText(&buf, diag.NewMapResolver(nil), diag.TextOptions{})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "missing.gom") {
		t.Errorf("error should name the file: %v", err)
	}
}

func TestWriteTextInvalidDiagnostic(t *testing.T) {
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "multi\nline"),
	}}
	var buf bytes.Buffer
	if err := rep.WriteText(&buf, diag.NewMapResolver(nil), diag.TextOptions{}); err == nil {
		t.Error("invalid diagnostic should fail")
	}
}

func TestWriteTextWriteError(t *testing.T) {
	res := diag.NewMapResolver(nil)
	rep := diag.Report{Diagnostics: []diag.Diagnostic{diag.Error("E0001", "x")}}
	if err := rep.WriteText(&failingWriter{}, res, diag.TextOptions{}); err == nil {
		t.Error("writer failure should be returned")
	}
}
