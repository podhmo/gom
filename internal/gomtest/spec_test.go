package gomtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// mustExpected parses a JSON array of expected diagnostic objects the
// way loadExpected would return them.
func mustExpected(t *testing.T, js string) []map[string]any {
	t.Helper()
	var arr []map[string]any
	if err := json.Unmarshal([]byte(js), &arr); err != nil {
		t.Fatalf("bad test JSON: %v", err)
	}
	return arr
}

// TestDiffDiagnosticsScalarFields covers code/severity/message/source
// matching and the ignore-unknown-keys rule.
func TestDiffDiagnosticsScalarFields(t *testing.T) {
	got := []diag.Diagnostic{
		diag.Error("E0301", "non-exhaustive match on `Expr`").
			From(diag.SourceGo),
	}

	for _, tt := range []struct {
		name string
		exp  string
		want int // number of mismatch messages
	}{
		{"all fields match",
			`[{"kind":"diagnostic","severity":"error","code":"E0301","message":"non-exhaustive match on ` + "`Expr`" + `","source":"go"}]`, 0},
		{"absent fields unchecked", `[{"code":"E0301"}]`, 0},
		{"kind mismatch", `[{"kind":"summary","code":"E0301"}]`, 1},
		{"severity mismatch", `[{"severity":"warning"}]`, 1},
		{"code mismatch", `[{"code":"E0302"}]`, 1},
		{"message mismatch", `[{"message":"other"}]`, 1},
		{"source mismatch", `[{"source":"gom"}]`, 1},
		{"non-string field is a mismatch not a panic", `[{"code":301}]`, 1},
		{"unknown keys ignored", `[{"code":"E0301","future_field":{"x":1}}]`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msgs := diffDiagnostics(mustExpected(t, tt.exp), got, diag.NewDirResolver(""))
			if len(msgs) != tt.want {
				t.Errorf("got %d messages %v, want %d", len(msgs), msgs, tt.want)
			}
		})
	}
}

func TestDiffDiagnosticsCount(t *testing.T) {
	got := []diag.Diagnostic{diag.Error("E0301", "m")}
	msgs := diffDiagnostics(mustExpected(t, `[{"code":"E0301"},{"code":"E0302"}]`), got, diag.NewDirResolver(""))
	if len(msgs) == 0 || !strings.Contains(msgs[0], "expected 2, got 1") {
		t.Errorf("msgs = %v", msgs)
	}
	// The count message includes a dump of the actual diagnostics.
	if !strings.Contains(msgs[0], "E0301") {
		t.Errorf("count message should dump actual diagnostics: %q", msgs[0])
	}
}

// TestDiffDiagnosticsSortsActual verifies the harness sorts actual
// diagnostics (diag.List.Sort order) before the pairwise comparison.
func TestDiffDiagnosticsSortsActual(t *testing.T) {
	a := diag.Error("E0302", "second").At(diag.SpanIn("f.gom", 10, 12))
	b := diag.Error("E0301", "first").At(diag.SpanIn("f.gom", 2, 4))
	exp := mustExpected(t, `[{"code":"E0301"},{"code":"E0302"}]`)
	// Unsorted input still matches the output-order expectation.
	if msgs := diffDiagnostics(exp, []diag.Diagnostic{a, b}, diag.NewDirResolver("")); len(msgs) != 0 {
		t.Errorf("msgs = %v", msgs)
	}
}

// TestDiffDiagnosticsSpan exercises span matching against a real
// source file (testdata/spans/f.gom contains "ab\ncd\nef\n", so byte
// offset 3 is line 2 col 1 and offset 6 is line 3 col 1).
func TestDiffDiagnosticsSpan(t *testing.T) {
	res := diag.NewDirResolver(filepath.Join("testdata", "spans"))
	sp := diag.SpanIn("f.gom", 3, 6) // covers "cd\n"
	got := []diag.Diagnostic{diag.Error("E0301", "m").At(sp)}

	for _, tt := range []struct {
		name string
		exp  string
		want int
	}{
		{"full position match",
			`[{"span":{"file":"f.gom","start":{"line":2,"col":1,"offset":3},"end":{"line":3,"col":1,"offset":6}}}]`, 0},
		{"offsets only", `[{"span":{"start":{"offset":3},"end":{"offset":6}}}]`, 0},
		{"line/col only", `[{"span":{"start":{"line":2,"col":1}}}]`, 0},
		{"file only", `[{"span":{"file":"f.gom"}}]`, 0},
		{"file mismatch", `[{"span":{"file":"other.gom"}}]`, 1},
		{"line mismatch", `[{"span":{"start":{"line":9}}}]`, 1},
		{"col mismatch", `[{"span":{"start":{"col":2}}}]`, 1},
		{"offset mismatch", `[{"span":{"start":{"offset":4}}}]`, 1},
		{"null span wants positionless", `[{"span":null}]`, 1},
		{"non-object span", `[{"span":"f.gom:3"}]`, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msgs := diffDiagnostics(mustExpected(t, tt.exp), got, res)
			if len(msgs) != tt.want {
				t.Errorf("got %d messages %v, want %d", len(msgs), msgs, tt.want)
			}
		})
	}
}

func TestDiffDiagnosticsSpanPositionless(t *testing.T) {
	got := []diag.Diagnostic{diag.Error("E0301", "m")} // zero Span
	res := diag.NewDirResolver(filepath.Join("testdata", "spans"))

	if msgs := diffDiagnostics(mustExpected(t, `[{"span":null}]`), got, res); len(msgs) != 0 {
		t.Errorf("null span vs positionless: msgs = %v", msgs)
	}
	if msgs := diffDiagnostics(mustExpected(t, `[{"span":{"file":"f.gom"}}]`), got, res); len(msgs) != 1 {
		t.Errorf("object span vs positionless: msgs = %v", msgs)
	}
}

// TestDiffDiagnosticsUnresolvableLineCol checks the failure mode where
// the expected file asks for line/column but the span's file cannot be
// read. Offsets remain comparable.
func TestDiffDiagnosticsUnresolvableLineCol(t *testing.T) {
	res := diag.NewDirResolver(filepath.Join("testdata", "spans"))
	sp := diag.SpanIn("missing.gom", 3, 5)
	got := []diag.Diagnostic{diag.Error("E0301", "m").At(sp)}

	msgs := diffDiagnostics(mustExpected(t, `[{"span":{"start":{"offset":3}}}]`), got, res)
	if len(msgs) != 0 {
		t.Errorf("offset-only compare must not need the file: %v", msgs)
	}
	msgs = diffDiagnostics(mustExpected(t, `[{"span":{"start":{"line":2}}}]`), got, res)
	if len(msgs) == 0 {
		t.Error("line compare against an unreadable file should report a mismatch")
	}
}

func TestDiffDiagnosticsData(t *testing.T) {
	got := []diag.Diagnostic{
		diag.Error("E0301", "m").
			Set("scrutinee_type", "Expr").
			Set("missing", []string{"Neg (Num _)", "Let _ _ _"}).
			Set("arity", 2),
	}

	for _, tt := range []struct {
		name string
		exp  string
		want int
	}{
		{"subset match",
			`[{"data":{"scrutinee_type":"Expr"}}]`, 0},
		{"exact match of all keys",
			`[{"data":{"scrutinee_type":"Expr","missing":["Neg (Num _)","Let _ _ _"],"arity":2}}]`, 0},
		{"missing key", `[{"data":{"nope":1}}]`, 1},
		{"wrong value", `[{"data":{"arity":3}}]`, 1},
		{"non-object data", `[{"data":[1]}]`, 1},
		{"absent data unchecked", `[{"code":"E0301"}]`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msgs := diffDiagnostics(mustExpected(t, tt.exp), got, diag.NewDirResolver(""))
			if len(msgs) != tt.want {
				t.Errorf("got %d messages %v, want %d", len(msgs), msgs, tt.want)
			}
		})
	}
}

// TestDiffDiagnosticsDataNormalization checks that Go values in
// Diagnostic.Data compare equal to JSON-decoded expected values.
func TestDiffDiagnosticsDataNormalization(t *testing.T) {
	got := []diag.Diagnostic{
		diag.Error("E0301", "m").
			Set("ints", []int{1, 2}).
			Set("m", map[string]int{"a": 1}),
	}
	exp := mustExpected(t, `[{"data":{"ints":[1,2],"m":{"a":1}}}]`)
	if msgs := diffDiagnostics(exp, got, diag.NewDirResolver("")); len(msgs) != 0 {
		t.Errorf("msgs = %v", msgs)
	}
}

func TestDiffDiagnosticsNotes(t *testing.T) {
	got := []diag.Diagnostic{
		diag.Error("E0301", "m").
			AddNote("first note").
			AddNote("second note").
			AddNote("third note"),
	}

	for _, tt := range []struct {
		name string
		exp  string
		want int
	}{
		{"subsequence", `[{"notes":["first note","third note"]}]`, 0},
		{"all", `[{"notes":["first note","second note","third note"]}]`, 0},
		{"missing", `[{"notes":["nope"]}]`, 1},
		{"out of order", `[{"notes":["third note","first note"]}]`, 1},
		{"non-array", `[{"notes":"x"}]`, 1},
		{"non-string element", `[{"notes":[1]}]`, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msgs := diffDiagnostics(mustExpected(t, tt.exp), got, diag.NewDirResolver(""))
			if len(msgs) != tt.want {
				t.Errorf("got %d messages %v, want %d", len(msgs), msgs, tt.want)
			}
		})
	}
}

func TestDiffDiagnosticsFixes(t *testing.T) {
	fix := diag.Fix{
		Title:         "add the missing arms",
		Applicability: diag.ApplicabilityPlaceholder,
		Edits: []diag.Edit{
			{Span: diag.SpanIn("f.gom", 6, 6), Text: "    Let _ _ _ -> todo ()\n"},
		},
	}
	safe := diag.Fix{Title: "rename it", Applicability: diag.ApplicabilitySafe}
	got := []diag.Diagnostic{
		diag.Error("E0301", "m").AddFix(safe).AddFix(fix),
	}
	res := diag.NewDirResolver(filepath.Join("testdata", "spans"))

	for _, tt := range []struct {
		name string
		exp  string
		want int
	}{
		{"title only", `[{"fixes":[{"title":"add the missing arms"}]}]`, 0},
		{"title+applicability", `[{"fixes":[{"title":"add the missing arms","applicability":"placeholder"}]}]`, 0},
		{"subsequence skips earlier fix", `[{"fixes":[{"title":"add the missing arms"}]}]`, 0},
		{"both in order", `[{"fixes":[{"title":"rename it"},{"title":"add the missing arms"}]}]`, 0},
		{"out of order fails", `[{"fixes":[{"title":"add the missing arms"},{"title":"rename it"}]}]`, 1},
		{"unknown title", `[{"fixes":[{"title":"bogus"}]}]`, 1},
		{"wrong applicability", `[{"fixes":[{"title":"add the missing arms","applicability":"safe"}]}]`, 1},
		{"full edit match by offset",
			`[{"fixes":[{"title":"add the missing arms","edits":[{"file":"f.gom","start":{"offset":6},"end":{"offset":6},"text":"    Let _ _ _ -> todo ()\n"}]}]}]`, 0},
		{"edit line/col resolution",
			`[{"fixes":[{"title":"add the missing arms","edits":[{"start":{"line":3,"col":1},"end":{"line":3,"col":1}}]}]}]`, 0},
		{"edit text mismatch",
			`[{"fixes":[{"title":"add the missing arms","edits":[{"start":{"offset":5},"end":{"offset":5},"text":"x"}]}]}]`, 1},
		{"edit count mismatch",
			`[{"fixes":[{"title":"add the missing arms","edits":[]}]}]`, 1},
		{"non-array fixes", `[{"fixes":{}}]`, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msgs := diffDiagnostics(mustExpected(t, tt.exp), got, res)
			if len(msgs) != tt.want {
				t.Errorf("got %d messages %v, want %d", len(msgs), msgs, tt.want)
			}
		})
	}
}

func TestLoadExpected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.expected.json")

	for _, tt := range []struct {
		name    string
		content string
		want    int // -1: want error
	}{
		{"empty list", `{"diagnostics": []}`, 0},
		{"one diag", `{"diagnostics": [{"code":"E0301"}]}`, 1},
		{"missing key", `{"other": []}`, -1},
		{"not an array", `{"diagnostics": {}}`, -1},
		{"element not object", `{"diagnostics": [1]}`, -1},
		{"invalid json", `{`, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			exp, err := loadExpected(path)
			if tt.want < 0 {
				if err == nil {
					t.Fatalf("want error, got %v", exp)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(exp) != tt.want {
				t.Errorf("len = %d, want %d", len(exp), tt.want)
			}
		})
	}

	if _, err := loadExpected(filepath.Join(dir, "absent.expected.json")); err == nil {
		t.Error("missing file should be an error mentioning -update")
	} else if !strings.Contains(err.Error(), "-update") {
		t.Errorf("error should hint at -update: %v", err)
	}
}

// TestSpecUpdate exercises -update: the expected file is rendered from
// actual diagnostics in the wire shape and passes comparison
// afterwards.
func TestSpecUpdate(t *testing.T) {
	dir := t.TempDir()
	gom := filepath.Join(dir, "c.gom")
	src := "package c\n\nfn f = match x {\n  A -> 1\n}\n"
	if err := os.WriteFile(gom, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	expectedPath := filepath.Join(dir, "c.expected.json")

	d := diag.Error("E0301", "non-exhaustive match on `Expr`").
		At(diag.SpanIn("c.gom", strings.Index(src, "match"), len(src)-1)).
		Set("scrutinee_type", "Expr").
		AddNote("arms with guards do not count").
		AddFix(diag.Fix{
			Title:         "add the missing arms",
			Applicability: diag.ApplicabilityPlaceholder,
			Edits:         []diag.Edit{{Span: diag.SpanIn("c.gom", len(src)-1, len(src)-1), Text: "  B -> todo ()\n"}},
		})

	res := diag.NewDirResolver(dir)
	writeExpected(t, expectedPath, []diag.Diagnostic{d}, res)

	// The written file loads and compares clean.
	exp, err := loadExpected(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if msgs := diffDiagnostics(exp, []diag.Diagnostic{d}, res); len(msgs) != 0 {
		t.Fatalf("round-trip mismatch: %v", msgs)
	}

	// And it really is the spec section 12 wire shape.
	data, _ := os.ReadFile(expectedPath)
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	obj := wire["diagnostics"].([]any)[0].(map[string]any)
	for _, k := range []string{"kind", "code", "severity", "message", "span", "data", "notes", "fixes"} {
		if _, ok := obj[k]; !ok {
			t.Errorf("rendered diagnostic lacks %q: %s", k, data)
		}
	}
	start := obj["span"].(map[string]any)["start"].(map[string]any)
	for _, k := range []string{"line", "col", "offset"} {
		if _, ok := start[k]; !ok {
			t.Errorf("span.start lacks %q", k)
		}
	}
	if start["line"].(float64) != 3 || start["col"].(float64) != 8 {
		t.Errorf("span.start = %v, want line 3 col 8", start)
	}
}
