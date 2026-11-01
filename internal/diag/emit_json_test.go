package diag_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/podhmo/gom/internal/diag"
)

// specExampleSource is a synthetic eval.gom whose byte offsets match
// the positions of the spec section 12 JSON example: offset 41 is
// line 2 column 3 ("match"), offset 228 is line 9 column 4 (after the
// two-byte 'é' at column 3), and offset 226 is line 9 column 3.
func specExampleSource() []byte {
	var b strings.Builder
	b.WriteString("package eval" + strings.Repeat(" ", 26) + "\n") // 39 bytes
	b.WriteString("  match e {\n")                                 // line 2 at 39
	b.WriteString("    Num n -> n\n")
	b.WriteString("    Add l r -> eval l + eval r\n")
	b.WriteString("    Neg (Neg x) -> eval x\n")
	b.WriteString("    Neg x if is_zero x -> 0\n")
	b.WriteString("    Neg x -> 0\n")             // 15 bytes: line 9 lands at 224
	b.WriteString(strings.Repeat("p", 57) + "\n") // 58 bytes
	b.WriteString("  é}")                         // col 3 is the two-byte 'é'
	return []byte(b.String())
}

// TestMarshalDiagnosticSpecExample renders the diagnostic of the spec
// section 12 example and compares the JSON object byte for byte
// (map keys inside "data" marshal in sorted order).
func TestMarshalDiagnosticSpecExample(t *testing.T) {
	src := specExampleSource()
	res := diag.NewMapResolver(map[string][]byte{"eval.gom": src})

	// Fixture sanity: the offsets must land where the spec says.
	li := diag.NewLineIndex(src)
	for off, want := range map[int]diag.Pos{
		41:  {Line: 2, Col: 3, Offset: 41},
		226: {Line: 9, Col: 3, Offset: 226},
		228: {Line: 9, Col: 4, Offset: 228},
	} {
		if got := li.Pos(off); got != want {
			t.Fatalf("fixture: Pos(%d) = %+v, want %+v", off, got, want)
		}
	}

	d := diag.Error("E0301", "non-exhaustive match on `Expr`").
		At(diag.SpanIn("eval.gom", 41, 228)).
		Set("scrutinee_type", "Expr").
		Set("missing_patterns", []string{"Neg (Num _)", "Neg (Add _ _)", "Neg (Let _ _ _)", "Let _ _ _"}).
		AddNote("arms with guards do not count toward exhaustiveness").
		AddFix(diag.Fix{
			Title:         "add the missing arms",
			Applicability: diag.ApplicabilityPlaceholder,
			Edits: []diag.Edit{{
				Span: diag.Point("eval.gom", 226),
				Text: "  Neg (Num _) -> todo ()\n    Neg (Add _ _) -> todo ()\n    Neg (Let _ _ _) -> todo ()\n    Let _ _ _ -> todo ()\n  ",
			}},
		})

	raw, err := diag.MarshalDiagnostic(d, res)
	if err != nil {
		t.Fatalf("MarshalDiagnostic: %v", err)
	}
	want := `{"kind":"diagnostic","code":"E0301","severity":"error","message":"non-exhaustive match on ` + "`Expr`" + `",` +
		`"span":{"file":"eval.gom","start":{"line":2,"col":3,"offset":41},"end":{"line":9,"col":4,"offset":228}},` +
		`"data":{"missing_patterns":["Neg (Num _)","Neg (Add _ _)","Neg (Let _ _ _)","Let _ _ _"],"scrutinee_type":"Expr"},` +
		`"notes":["arms with guards do not count toward exhaustiveness"],` +
		`"fixes":[{"title":"add the missing arms","applicability":"placeholder","edits":[{` +
		`"file":"eval.gom","start":{"line":9,"col":3,"offset":226},"end":{"line":9,"col":3,"offset":226},` +
		`"text":"  Neg (Num _) -> todo ()\n    Neg (Add _ _) -> todo ()\n    Neg (Let _ _ _) -> todo ()\n    Let _ _ _ -> todo ()\n  "` +
		`}]}]}`
	if string(raw) != want {
		t.Errorf("marshal =\n%s\nwant\n%s", raw, want)
	}
	if bytes.Contains(raw, []byte("\n")) {
		t.Error("marshal produced a newline; objects must stay on one line")
	}
}

// TestWriteJSONLines checks a full stream: one diagnostic object per
// line, then the summary line — the spec's default format.
func TestWriteJSONLines(t *testing.T) {
	src := []byte("ab\ncd\nef\n")
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})
	rep := diag.Report{
		Diagnostics: []diag.Diagnostic{
			diag.Error("E0001", "broken").At(diag.SpanIn("f.gom", 3, 5)),
		},
		Files: []string{"f.gom"},
	}
	var buf bytes.Buffer
	if err := rep.WriteJSONLines(&buf, res); err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"diagnostic","code":"E0001","severity":"error","message":"broken","span":{"file":"f.gom","start":{"line":2,"col":1,"offset":3},"end":{"line":2,"col":3,"offset":5}}}` + "\n" +
		`{"kind":"summary","ok":false,"errors":1,"warnings":0,"files":["f.gom"],"outputs":[]}` + "\n"
	if buf.String() != want {
		t.Errorf("stream =\n%s\nwant\n%s", buf.String(), want)
	}
}

// TestWriteJSONLinesEmpty emits only the summary line when the report
// holds no diagnostics.
func TestWriteJSONLinesEmpty(t *testing.T) {
	var buf bytes.Buffer
	rep := diag.Report{Files: []string{"a.gom"}, Outputs: []string{"a.go"}}
	if err := rep.WriteJSONLines(&buf, diag.NewMapResolver(nil)); err != nil {
		t.Fatal(err)
	}
	want := `{"kind":"summary","ok":true,"errors":0,"warnings":0,"files":["a.gom"],"outputs":["a.go"]}` + "\n"
	if buf.String() != want {
		t.Errorf("stream = %q, want %q", buf.String(), want)
	}
}

// TestWriteJSONLinesSorts emits diagnostics in List.Sort order
// regardless of the order they were collected in.
func TestWriteJSONLinesSorts(t *testing.T) {
	src := []byte("ab\ncd\n")
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0002", "later").At(diag.SpanIn("f.gom", 3, 4)),
		diag.Error("E0001", "earlier").At(diag.SpanIn("f.gom", 0, 1)),
	}}
	var buf bytes.Buffer
	if err := rep.WriteJSONLines(&buf, res); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines = %d, want 3: %q", len(lines), buf.String())
	}
	if !strings.Contains(lines[0], `"code":"E0001"`) {
		t.Errorf("line 1 = %s, want the earlier-position diagnostic first", lines[0])
	}
	if !strings.Contains(lines[1], `"code":"E0002"`) {
		t.Errorf("line 2 = %s", lines[1])
	}
	if !strings.HasPrefix(lines[2], `{"kind":"summary"`) {
		t.Errorf("last line = %s, want summary", lines[2])
	}
	// The input order is untouched (emission sorts a copy).
	if rep.Diagnostics[0].Code != "E0002" {
		t.Error("WriteJSONLines reordered the report's diagnostics")
	}
}

// TestMarshalDiagnosticMultiFile resolves a span and fix edits that
// point into different files.
func TestMarshalDiagnosticMultiFile(t *testing.T) {
	res := diag.NewMapResolver(map[string][]byte{
		"a.gom": []byte("aaa\n"),
		"b.gom": []byte("bb\n"),
	})
	d := diag.Error("E0001", "x").
		At(diag.SpanIn("a.gom", 1, 2)).
		AddFix(diag.Fix{
			Title:         "fix b",
			Applicability: diag.ApplicabilitySafe,
			Edits:         []diag.Edit{{Span: diag.SpanIn("b.gom", 0, 1), Text: "z"}},
		})
	raw, err := diag.MarshalDiagnostic(d, res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"file":"a.gom","start":{"line":1,"col":2,"offset":1}`) {
		t.Errorf("primary span not resolved in a.gom: %s", s)
	}
	if !strings.Contains(s, `"file":"b.gom","start":{"line":1,"col":1,"offset":0},"end":{"line":1,"col":2,"offset":1},"text":"z"`) {
		t.Errorf("fix edit not resolved in b.gom: %s", s)
	}
}

// TestMarshalDiagnosticCodePointColumns pins the column unit: columns
// count Unicode code points, not bytes (docs/spec.md section 12).
func TestMarshalDiagnosticCodePointColumns(t *testing.T) {
	src := []byte("let s = \"あいう\"\n") // あいう: 3 runes, 9 bytes
	res := diag.NewMapResolver(map[string][]byte{"f.gom": src})
	start := bytes.Index(src, []byte("あ"))  // byte 9, col 10
	end := bytes.Index(src, []byte("\"\n")) // byte 18, col 13
	d := diag.Error("E0001", "x").At(diag.SpanIn("f.gom", start, end))
	raw, err := diag.MarshalDiagnostic(d, res)
	if err != nil {
		t.Fatal(err)
	}
	// あ is the 10th code point; あいう spans cols 10-12, so the
	// closing '"' is col 13 — not the byte-derived 14.
	want := `"start":{"line":1,"col":10,"offset":9},"end":{"line":1,"col":13,"offset":18}`
	if !strings.Contains(string(raw), want) {
		t.Errorf("span = %s, want columns in code points %s", raw, want)
	}
}

// TestMarshalDiagnosticOmittedFields verifies optional fields are
// absent rather than null or empty when the diagnostic lacks them.
func TestMarshalDiagnosticOmittedFields(t *testing.T) {
	res := diag.NewMapResolver(nil)
	raw, err := diag.MarshalDiagnostic(diag.Error("E0001", "x"), res)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, k := range []string{`"span"`, `"source"`, `"data"`, `"notes"`, `"fixes"`} {
		if strings.Contains(s, k) {
			t.Errorf("bare diagnostic should omit %s: %s", k, s)
		}
	}
	want := `{"kind":"diagnostic","code":"E0001","severity":"error","message":"x"}`
	if s != want {
		t.Errorf("marshal = %s, want %s", s, want)
	}
}

func TestMarshalDiagnosticSource(t *testing.T) {
	res := diag.NewMapResolver(nil)
	d := diag.Error("E0001", "go build failed").From(diag.SourceGo)
	raw, err := diag.MarshalDiagnostic(d, res)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"source":"go"`) {
		t.Errorf("source marker missing: %s", raw)
	}
}

// TestMarshalDiagnosticSeverityWords emits the spec's severity tokens.
func TestMarshalDiagnosticSeverityWords(t *testing.T) {
	res := diag.NewMapResolver(nil)
	for _, tt := range []struct {
		d   diag.Diagnostic
		sev string
	}{
		{diag.Error("E0001", "x"), `"severity":"error"`},
		{diag.Warning("W0001", "x"), `"severity":"warning"`},
		{diag.Note("N0001", "x"), `"severity":"note"`},
	} {
		raw, err := diag.MarshalDiagnostic(tt.d, res)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), tt.sev) {
			t.Errorf("marshal = %s, want %s", raw, tt.sev)
		}
	}
}

// TestMarshalDiagnosticNoHTMLEscape keeps Gom operators ("->", "|>",
// "&&", "<-") readable on the wire instead of < escapes.
func TestMarshalDiagnosticNoHTMLEscape(t *testing.T) {
	res := diag.NewMapResolver(nil)
	d := diag.Error("E0001", "expected `->`, got `<-` in `a && b`")
	raw, err := diag.MarshalDiagnostic(d, res)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `\u`) {
		t.Errorf("operators must not be HTML-escaped: %s", raw)
	}
	if !strings.Contains(string(raw), "expected `->`, got `<-` in `a && b`") {
		t.Errorf("message mangled: %s", raw)
	}
}

// TestMarshalDiagnosticInvalidDiagnostics reports diagnostics that
// break the format's invariants instead of emitting them.
func TestMarshalDiagnosticInvalidDiagnostics(t *testing.T) {
	res := diag.NewMapResolver(nil)
	for _, tt := range []struct {
		name string
		d    diag.Diagnostic
	}{
		{"empty message", diag.Error("E0001", "")},
		{"empty code", diag.Error("", "x")},
		{"multi-line message", diag.Error("E0001", "a\nb")},
	} {
		if _, err := diag.MarshalDiagnostic(tt.d, res); err == nil {
			t.Errorf("%s: want error", tt.name)
		}
	}
}

func TestMarshalDiagnosticUnresolvableSpan(t *testing.T) {
	res := diag.NewMapResolver(map[string][]byte{"a.gom": []byte("x")})
	d := diag.Error("E0001", "x").At(diag.SpanIn("missing.gom", 0, 1))
	_, err := diag.MarshalDiagnostic(d, res)
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "missing.gom") {
		t.Errorf("error should name the file: %v", err)
	}
}

func TestMarshalDiagnosticUnrepresentableData(t *testing.T) {
	res := diag.NewMapResolver(nil)
	d := diag.Error("E0001", "x").Set("ch", make(chan int))
	if _, err := diag.MarshalDiagnostic(d, res); err == nil {
		t.Error("non-JSON data should fail")
	}
}

type failingWriter struct{}

func (w *failingWriter) Write(b []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestWriteJSONLinesWriteError(t *testing.T) {
	res := diag.NewMapResolver(nil)
	rep := diag.Report{Diagnostics: []diag.Diagnostic{diag.Error("E0001", "x")}}
	if err := rep.WriteJSONLines(&failingWriter{}, res); err == nil {
		t.Error("write failure should propagate")
	}
}

// TestWriteJSONLinesNilResolverWithLocation: a located span plus a nil
// resolver reports an error (positionless diagnostics still emit).
func TestWriteJSONLinesNilResolverWithLocation(t *testing.T) {
	rep := diag.Report{Diagnostics: []diag.Diagnostic{
		diag.Error("E0001", "x").At(diag.SpanIn("f.gom", 0, 1)),
	}}
	var buf bytes.Buffer
	if err := rep.WriteJSONLines(&buf, nil); err == nil {
		t.Error("located span with nil resolver should fail")
	}
}
